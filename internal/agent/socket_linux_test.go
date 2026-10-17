//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// privateTempDir returns a per-test temporary directory with mode 0700.
//
// t.TempDir() creates its numbered subdirectory with os.Mkdir(dir, 0o777), so
// its effective mode follows the process umask: 0755 under the 0o022 umask used
// in CI, but 0775 under the default user umask 0o002 on Debian and Ubuntu. The
// directory checks in listenSocket and LoadRootPolicy reject group-writable
// directories, so tests that exercise them must pin the mode instead of
// inheriting it from the environment.
func privateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod temporary directory: %v", err)
	}
	return dir
}

func TestUnixSocketRequiresPeerAndRunsOnlyAuthorizedRequest(t *testing.T) {
	target := Target{ID: "app", Kind: KindDocker, Identity: strings.Repeat("b", 64), Actions: []Action{ActionStatus}}
	backend := &fakeBackend{state: StateRunning}
	service := NewService(NewPolicy("agent-1", []Target{target}), backend, time.Second)
	uid := uint32(os.Geteuid())
	path := filepath.Join(privateTempDir(t), "agent.sock")
	listener, err := listenSocket(path, -1, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0660 {
		t.Fatalf("socket mode=%v err=%v", info.Mode().Perm(), err)
	}
	server := NewSocketServer(service, []uint32{uid}, nil)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	agentClient, err := NewUnixClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer agentClient.CloseIdleConnections()
	agentResult, err := agentClient.Execute(context.Background(), validRequest("status-1", "app", ActionStatus))
	if err != nil || agentResult.State != StateRunning || backend.statusCalls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", agentResult, err, backend.statusCalls)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	requestBody := `{"request_id":"status-1","agent_id":"agent-1","target_id":"app","action":"status"}`
	response, err := client.Post("http://agent/v1/actions", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || result.State != StateRunning || backend.statusCalls != 1 {
		t.Fatalf("response=%d result=%+v calls=%d", response.StatusCode, result, backend.statusCalls)
	}
	requestBody = `{"request_id":"bad-action","agent_id":"agent-1","target_id":"app","action":"restart"}`
	response, err = client.Post("http://agent/v1/actions", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden || backend.doCalls != 0 {
		t.Fatalf("unauthorized action status=%d transitions=%d", response.StatusCode, backend.doCalls)
	}
	_ = response.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveErr; err != http.ErrServerClosed {
		t.Fatalf("serve error=%v", err)
	}
	transport.CloseIdleConnections()
}

func TestListenSocketRefusesRegularFileAndUnsafePolicyMetadata(t *testing.T) {
	dir := privateTempDir(t)
	path := filepath.Join(dir, "agent.sock")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := listenSocket(path, -1, false); err == nil {
		t.Fatal("regular file was replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing file changed: %q err=%v", data, err)
	}
	policy := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(policy, []byte(`{"agent_id":"agent-1","targets":[]}`), 0660); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(policy, 0660); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePolicyFile(info, false); err == nil {
		t.Fatal("group-writable policy accepted")
	}
	if err := os.Chmod(policy, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(policy); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, policy); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePolicyFile(info, false); err == nil {
		t.Fatal("policy symlink accepted")
	}
}

func TestAgentClientFailsClosedWhenSocketUnavailable(t *testing.T) {
	client, err := NewUnixClient(filepath.Join(t.TempDir(), "missing.sock"), 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Execute(context.Background(), validRequest("offline", "app", ActionStatus))
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("unavailable socket error=%v", err)
	}
}

func TestSocketHandlerRejectsMissingPeerCredentialsBeforeDecode(t *testing.T) {
	backend := &fakeBackend{state: StateRunning}
	service := NewService(NewPolicy("agent-1", []Target{{ID: "app", Kind: KindDocker, Identity: strings.Repeat("a", 64), Actions: []Action{ActionStatus}}}), backend, time.Second)
	server := NewSocketServer(service, nil, nil)
	response := httptest.NewRecorder()
	server.action(response, httptest.NewRequest(http.MethodPost, "/v1/actions", strings.NewReader("{}")))
	if response.Code != http.StatusForbidden || backend.statusCalls != 0 {
		t.Fatalf("status=%d backend calls=%d body=%s", response.Code, backend.statusCalls, response.Body.String())
	}
}

func TestLoadRootPolicyRejectsUntrustedDirectory(t *testing.T) {
	dir := privateTempDir(t)
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"agent_id":"agent-1","targets":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRootPolicy(path); err == nil {
		t.Fatal("non-root policy directory accepted")
	}
}

func TestLoadRootPolicyAcceptsRootOwnedPolicy(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires a root-owned temporary policy")
	}
	dir := privateTempDir(t)
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"agent_id":"agent-1","targets":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePolicyFile(info, true); err != nil {
		t.Fatalf("root-owned policy metadata rejected: %v", err)
	}
	policy, err := LoadRootPolicy(path)
	if err != nil {
		t.Fatalf("root-owned policy rejected: %v", err)
	}
	if policy.AgentID() != "agent-1" {
		t.Fatalf("agent ID = %q, want agent-1", policy.AgentID())
	}
}

func TestListenSocketAcceptsRootOwnedDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires a root-owned socket directory")
	}
	path := filepath.Join(privateTempDir(t), "agent.sock")
	listener, err := ListenSocket(path, -1)
	if err != nil {
		t.Fatalf("root-owned socket directory rejected: %v", err)
	}
	defer listener.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
