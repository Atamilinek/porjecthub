package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var ErrAgentUnavailable = errors.New("agent unavailable")

type Client interface {
	Execute(context.Context, ActionRequest) (Result, error)
}
type UnixClient struct {
	path   string
	client *http.Client
}

func NewUnixClient(path string, timeout time.Duration) (*UnixClient, error) {
	if !filepath.IsAbs(path) || len(path) > 100 {
		return nil, fmt.Errorf("%w: socket path must be absolute", ErrAgentUnavailable)
	}
	if timeout <= 0 {
		timeout = 35 * time.Second
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", path)
	}, DisableKeepAlives: true}
	return &UnixClient{path: path, client: &http.Client{Transport: transport, Timeout: timeout}}, nil
}
func (c *UnixClient) CloseIdleConnections() {
	if c != nil && c.client != nil {
		c.client.CloseIdleConnections()
	}
}
func (c *UnixClient) Execute(ctx context.Context, request ActionRequest) (Result, error) {
	if c == nil || c.client == nil || c.path == "" {
		return Result{}, ErrAgentUnavailable
	}
	data, err := json.Marshal(request)
	if err != nil {
		return Result{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://projecthub-agent/v1/actions", bytes.NewReader(data))
	if err != nil {
		return Result{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 8<<10)).Decode(&body)
		return Result{}, &RemoteError{Status: response.StatusCode, Code: body.Error.Code}
	}
	var result Result
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<10)).Decode(&result); err != nil {
		return Result{}, fmt.Errorf("decode agent result: %w", err)
	}
	return result, nil
}

type RemoteError struct {
	Status int
	Code   string
}

func (e *RemoteError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("agent request failed (%d)", e.Status)
	}
	return "agent request failed: " + e.Code
}

func UnixSocketAvailable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0
}
