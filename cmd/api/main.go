package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/projecthub/projecthub/internal/agent"
	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/categories"
	"github.com/projecthub/projecthub/internal/config"
	"github.com/projecthub/projecthub/internal/events"
	"github.com/projecthub/projecthub/internal/jobs"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/monitor"
	"github.com/projecthub/projecthub/internal/projects"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/internal/webapp"
	"github.com/projecthub/projecthub/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-admin" {
		if err := bootstrapAdmin(os.Args[2:]); err != nil {
			logger.Error("administrator bootstrap failed", "error", err)
			os.Exit(1)
		}
		logger.Info("administrator account created")
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "backup" || os.Args[1] == "restore") {
		if err := runDatabaseCommand(os.Args[1], os.Args[2:]); err != nil {
			logger.Error("database command failed", "command", os.Args[1], "error", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := migrate.Apply(context.Background(), db, migrations.FS); err != nil {
		logger.Error("apply database migrations", "error", err)
		os.Exit(1)
	}

	var agentClient agent.Client
	if cfg.AgentSocketPath != "" {
		client, clientErr := agent.NewUnixClient(cfg.AgentSocketPath, 35*time.Second)
		if clientErr != nil {
			logger.Error("configure Agent client", "error", clientErr)
			os.Exit(2)
		}
		agentClient = client
	}
	handler, scheduler, dispatcher := newRuntime(db, cfg.ProbeAllowCIDRs, agentClient)
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("monitor scheduler stopped", "error", err)
		}
	}()
	go func() {
		if err := dispatcher.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("action dispatcher stopped", "error", err)
		}
	}()
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("Project Hub API listening", "address", cfg.ListenAddress)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("API server stopped", "error", err)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown API server", "error", err)
		os.Exit(1)
	}
}

func newHandler(db *sql.DB) http.Handler {
	handler, _, _ := newRuntime(db, nil, nil)
	return handler
}

func newRuntime(db *sql.DB, allowedProbeCIDRs []netip.Prefix, agentClient agent.Client) (http.Handler, *monitor.Scheduler, *jobs.Dispatcher) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	authRoutes := &authHTTP{service: auth.NewService(db), limiter: auth.NewLoginLimiter(5, 15*time.Minute), sourceLimiter: auth.NewLoginLimiter(20, 15*time.Minute), db: db}
	authRoutes.register(mux)
	(&catalogHTTP{auth: authRoutes, projects: projects.NewService(db), categories: categories.NewService(db)}).register(mux)
	broker := events.NewBroker(512, 64)
	dispatcher := jobs.NewDispatcher(db, agentClient, broker, 4, 64)
	checker := monitor.NewDefaultChecker(allowedProbeCIDRs)
	checker.Agent = monitorAgentProbe{dispatcher: dispatcher}
	monitorService := monitor.NewService(db, checker, 10)
	(&monitorHTTP{auth: authRoutes, service: monitorService, broker: broker}).register(mux)
	(&agentHTTP{auth: authRoutes, db: db, jobs: dispatcher}).register(mux)
	mux.Handle("/", webapp.Embedded())
	return requestID(mux), monitor.NewScheduler(monitorService, broker, time.Second), dispatcher
}
