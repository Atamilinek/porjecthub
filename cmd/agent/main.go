//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/projecthub/projecthub/internal/agent"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	policyPath := flag.String("policy", "/etc/projecthub/agent-policy.json", "root-owned target allowlist")
	socketPath := flag.String("socket", "/run/projecthub-agent/agent.sock", "restricted Unix socket path")
	allowedUID := flag.Uint("api-uid", ^uint(0), "API process UID allowed to use the socket")
	allowedGID := flag.Uint("api-gid", ^uint(0), "API group ID allowed to use the socket")
	socketGID := flag.Int("socket-gid", -1, "group that owns the socket")
	dockerEndpoint := flag.String("docker-endpoint", "", "explicit Docker Engine endpoint; unset disables Docker control")
	enableSystemd := flag.Bool("enable-systemd", false, "explicitly enable systemd unit control")
	timeout := flag.Duration("action-timeout", 30*time.Second, "maximum duration for one action")
	flag.Parse()
	unconfigured := ^uint(0)
	if *allowedUID == unconfigured && *allowedGID == unconfigured {
		logger.Error("configure --api-uid or --api-gid")
		os.Exit(2)
	}
	if *allowedUID != unconfigured && uint64(*allowedUID) > uint64(^uint32(0)) || *allowedGID != unconfigured && uint64(*allowedGID) > uint64(^uint32(0)) {
		logger.Error("API UID/GID must fit a Linux credential ID")
		os.Exit(2)
	}
	if *allowedUID != unconfigured && *allowedGID == unconfigured && *socketGID == -1 && *allowedUID != uint(os.Geteuid()) {
		logger.Error("a different API UID requires --api-gid or --socket-gid so filesystem permissions permit socket access")
		os.Exit(2)
	}
	policy, err := agent.LoadRootPolicy(*policyPath)
	if err != nil {
		logger.Error("load policy", "error", err)
		os.Exit(1)
	}
	var docker *agent.DockerBackend
	var systemd *agent.SystemdBackend
	needsDocker, needsSystemd := false, false
	for _, target := range policy.TargetIDs() {
		item, _ := policy.Target(target)
		if item.Kind == agent.KindDocker {
			needsDocker = true
		}
		if item.Kind == agent.KindSystemd {
			needsSystemd = true
		}
	}
	if needsDocker {
		if *dockerEndpoint == "" {
			logger.Error("policy contains Docker targets but --docker-endpoint is unset")
			os.Exit(2)
		}
		client, err := agent.NewDockerClient(*dockerEndpoint)
		if err != nil {
			logger.Error("create Docker client", "error", err)
			os.Exit(1)
		}
		defer client.Close()
		docker = agent.NewDockerBackend(client)
	}
	if needsSystemd {
		if !*enableSystemd {
			logger.Error("policy contains systemd targets but --enable-systemd is unset")
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		systemd, err = agent.NewSystemdBackend(ctx)
		cancel()
		if err != nil {
			logger.Error("connect systemd D-Bus", "error", err)
			os.Exit(1)
		}
		defer systemd.Close()
	}
	service := agent.NewService(policy, agent.BackendMux{Docker: docker, Systemd: systemd}, *timeout)
	if *socketGID == -1 && *allowedGID != unconfigured {
		*socketGID = int(*allowedGID)
	}
	listener, err := agent.ListenSocket(*socketPath, *socketGID)
	if err != nil {
		logger.Error("listen on agent socket", "error", err)
		os.Exit(1)
	}
	allowedUIDs, allowedGIDs := []uint32{}, []uint32{}
	if *allowedUID != unconfigured {
		allowedUIDs = append(allowedUIDs, uint32(*allowedUID))
	}
	if *allowedGID != unconfigured {
		allowedGIDs = append(allowedGIDs, uint32(*allowedGID))
	}
	server := agent.NewSocketServer(service, allowedUIDs, allowedGIDs)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	logger.Info("Project Hub Agent listening", "agent_id", policy.AgentID(), "socket", *socketPath, "docker_enabled", docker != nil, "systemd_enabled", systemd != nil)
	select {
	case err := <-serveErr:
		if !errors.Is(err, net.ErrClosed) {
			logger.Error("Agent server stopped", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			logger.Error("Agent shutdown", "error", err)
			os.Exit(1)
		}
	}
}
