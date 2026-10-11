//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type peerKey struct{}
type peer struct{ uid, gid uint32 }
type SocketServer struct {
	service                  *Service
	allowedUIDs, allowedGIDs map[uint32]bool
	server                   *http.Server
}
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func NewSocketServer(service *Service, allowedUIDs, allowedGIDs []uint32) *SocketServer {
	uids, gids := make(map[uint32]bool), make(map[uint32]bool)
	for _, id := range allowedUIDs {
		uids[id] = true
	}
	for _, id := range allowedGIDs {
		gids[id] = true
	}
	s := &SocketServer{service: service, allowedUIDs: uids, allowedGIDs: gids}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/actions", s.action)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, MaxHeaderBytes: 8 << 10, ConnContext: s.connectionContext}
	return s
}

func (s *SocketServer) Serve(listener net.Listener) error  { return s.server.Serve(listener) }
func (s *SocketServer) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }
func (s *SocketServer) connectionContext(ctx context.Context, conn net.Conn) context.Context {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return ctx
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return ctx
	}
	var credentials *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || controlErr != nil || credentials == nil {
		return ctx
	}
	return context.WithValue(ctx, peerKey{}, peer{uid: credentials.Uid, gid: credentials.Gid})
}
func (s *SocketServer) action(w http.ResponseWriter, r *http.Request) {
	caller, ok := r.Context().Value(peerKey{}).(peer)
	if !ok || !s.allowedUIDs[caller.uid] && !s.allowedGIDs[caller.gid] {
		writeAgentError(w, http.StatusForbidden, "peer_denied", "peer credentials are not authorized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request ActionRequest
	if err := decoder.Decode(&request); err != nil {
		writeAgentError(w, http.StatusBadRequest, "invalid_request", "request body is invalid")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeAgentError(w, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return
	}
	result, err := s.service.Execute(r.Context(), request)
	if err != nil {
		status, code, message := classifyAgentError(err)
		writeAgentError(w, status, code, message)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}
func writeAgentError(w http.ResponseWriter, status int, code, message string) {
	var body apiError
	body.Error.Code, body.Error.Message = code, message
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func classifyAgentError(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrAgentIdentity):
		return http.StatusForbidden, "agent_identity_mismatch", "agent identity mismatch"
	case errors.Is(err, ErrProtectedTarget):
		return http.StatusForbidden, "protected_target", "target is protected"
	case errors.Is(err, ErrUnsupportedAction):
		return http.StatusForbidden, "action_denied", "action is not authorized"
	case errors.Is(err, ErrTargetNotFound):
		return http.StatusNotFound, "target_not_found", "target is not authorized"
	case errors.Is(err, ErrIdentityMismatch):
		return http.StatusConflict, "identity_mismatch", "target identity changed"
	case errors.Is(err, ErrRequestConflict):
		return http.StatusConflict, "request_conflict", "request ID was reused"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "action_timeout", "action timed out"
	default:
		return http.StatusBadGateway, "backend_failed", "target operation failed"
	}
}

func ListenSocket(path string, groupID int) (net.Listener, error) {
	return listenSocket(path, groupID, true)
}
func listenSocket(path string, groupID int, requireRootDirectory bool) (net.Listener, error) {
	if !filepath.IsAbs(path) || len(path) > 100 || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("agent socket path must be a short absolute path")
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return nil, fmt.Errorf("socket directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return nil, errors.New("agent socket directory must not be group/world writable")
	}
	if requireRootDirectory {
		stat, ok := info.Sys().(*unix.Stat_t)
		if !ok || stat.Uid != 0 {
			return nil, errors.New("agent socket directory must be owned by root")
		}
	}
	if prior, err := os.Lstat(path); err == nil {
		if prior.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace a non-socket at agent path")
		}
		conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil, errors.New("agent socket is already active")
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale agent socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	cleanup := func(cause error) (net.Listener, error) { _ = listener.Close(); _ = os.Remove(path); return nil, cause }
	if err := os.Chmod(path, 0660); err != nil {
		return cleanup(err)
	}
	if groupID >= 0 {
		if err := os.Chown(path, -1, groupID); err != nil {
			return cleanup(err)
		}
	}
	return listener, nil
}
