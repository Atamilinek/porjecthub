# Project Hub Architecture

## Goals and constraints

Project Hub is a self-hosted internal catalog and control plane for projects and explicitly authorized services. Navigation metadata and execution authority are separate. The first release is a single repository with a modular Go API, Vue 3 web client, and separately deployed Linux Agent. It supports Linux amd64 and arm64; the API is not exposed directly to untrusted networks.

The platform must remain useful without an Agent: projects can be catalogued and monitored without any control target. Adding a project never grants host privileges. Agent configuration is managed by the host administrator outside the Web API, and unknown targets/actions fail closed.

## Components and trust boundaries

```text
Browser --HTTPS--> Caddy --private network--> Go API + Vue assets
                                              | SQLite (WAL, persistent volume)
                                              | HTTP/TCP health probes (restricted)
                                              | SSE event stream
                                              `-- Unix socket --> projecthub-agent
                                                                  | local allowlist
                                                                  `-- Docker Engine API / explicit systemd units
```

- **Web/API:** One Go process serves the `/api/v1` API and the built SPA. Its SQLite file is persistent. It never mounts `/var/run/docker.sock` and never runs shell commands.
- **Web client:** Vue 3, TypeScript, Vite, Vue Router, Pinia and a mature component library. It uses same-origin APIs, secure session cookies, CSRF tokens, and SSE with polling fallback.
- **Agent:** A distinct Go binary, Linux systemd unit, and Unix domain socket. It accepts only typed target IDs and the fixed actions `status`, `start`, `stop`, and `restart`. A root-owned policy file is validated at startup and defines immutable target identity, supported operations, and protected resources. Policy changes require a host administrator to edit and restart the Agent; the API cannot edit/reload the file.
- **Gateway:** Caddy terminates HTTPS. The API listens only on the Compose private network. SQLite, credentials, certificates, and Agent policy files are persistent or host-managed secrets and are never committed.

## Repository and modules

One repository contains the API, web UI, Agent, migration files, deployment files, tests, and operator documentation. The Go API is a modular monolith with packages for configuration, persistence/migrations, auth/RBAC, projects/catalog, monitoring/SSRF, jobs/audit, and HTTP transport. The Agent has a separate command and packages for socket transport, policy loading, Docker/systemd adapters, and target locking. Interfaces isolate Docker/systemd from policy and job logic so tests use fakes only.

The SQLite schema uses UUID identifiers, foreign keys, indexes, parameterized SQL, WAL, bounded connection concurrency, and numbered migrations. Migration application is transactional. The database directory is created with owner-only permissions and the database file is restricted to mode 0600 on POSIX systems. Health samples have configurable retention and scheduled cleanup. `projecthub-api backup` uses `VACUUM INTO` for an online consistent snapshot; `restore` stages a copy, applies migrations, runs `PRAGMA integrity_check`, and atomically replaces the database. Restore requires the operator to stop the API first.

## Authentication and authorization

All management routes require an authenticated session and server-side RBAC. Viewer can read authorized catalog/state data; Operator can submit actions only for targets the server has bound to a host allowlist ID; Admin can manage projects, links, categories, hosts, users, and settings. The first-run command creates the initial admin interactively or from a one-time secret supplied through a protected file/stdin; it has no default password and refuses to reset an existing account implicitly.

Passwords use Argon2id with the OWASP baseline parameters (19 MiB, 2 iterations, 1 lane); passwords must be 12–1024 bytes. Sessions use random 256-bit opaque tokens; only SHA-256 token hashes are persisted. A session expires after 12 hours, rotates on successful login when an old cookie is present, and is revoked on logout. Cookies are HttpOnly, Secure, SameSite=Strict, and path-scoped. Mutations require a per-session CSRF token and same-origin validation. Login throttles each source/account pair after 5 failures and each source after 20 failures per 15 minutes. The bootstrap command accepts the initial secret only from a regular protected file (mode 0600 on POSIX) and refuses a second administrator bootstrap. Authentication and authorization middleware are installed before management handlers are registered. Login and logout audit records include actor where known, request ID, action, timestamp, and outcome without passwords or session secrets.

## Monitoring and SSRF controls

HTTP(S) and TCP checks have explicit configured targets, bounded deadlines, bounded concurrency, failure/recovery thresholds, persisted states, and retained samples. Agent checks query the mapped target through the Agent client; Agent loss never implies a service is stopped. Sample retention is 30 days, with cleanup at least daily; a check defaults to 30-second intervals, 5-second timeout, and 3-failure/2-recovery transitions. Monitoring configuration and samples are available through authenticated API endpoints and the monitoring page. The page consumes authenticated SSE state/sample events and falls back to 15-second polling while the stream reconnects.

HTTP checks reject credentials, unsupported schemes, loopback/link-local/private/metadata and reserved destinations. Private IPv4 and ULA IPv6 destinations are permitted only when contained by an explicit narrow CIDR in `PROJECTHUB_PROBE_ALLOW_CIDRS`; configuration rejects public or broad ranges. The checker resolves at connection time, validates every DNS answer, pins one permitted address for the connection while preserving the URL host for TLS verification, disables redirects, caps response bodies at 64 KiB, and never uses environment proxies. TCP probes use the same destination policy. Monitoring configuration is Admin-only and distinct from project navigation URLs.

## Service control and jobs

The Web API stores host/target bindings but cannot create Agent policy entries. The Agent reads its root-owned policy at startup, verifies target identity and operation on every request, denies protected core services, checks current state, serializes each target, and returns explicit not-found/unavailable errors. Docker control uses the Moby Docker Engine client and only an existing full container ID present in policy; it never discovers, adopts, creates, execs, or controls Compose projects. systemd control is limited to exact allowlisted service unit names through the systemd D-Bus interface, never a generic command endpoint. Project Hub and core host units are hard-protected; Docker containers named `projecthub-*` or carrying `com.projecthub.protected=true` / `projecthub.protected=true` labels are also denied.

Actions are asynchronous, time-bounded, idempotent by request ID, and recorded in `action_jobs` and `audit_logs`. Duplicate idempotency keys return the existing job only for the same actor/target/action; mismatched reuse conflicts. The API confirms state by reading it back after an action. API startup marks unfinished jobs as interrupted failures and never automatically replays them. Agent socket access is limited by owner/group/mode and Linux peer credentials. API-to-Agent is optional and disabled unless an explicit local socket path is configured. There is no shell, arbitrary file operation, or raw Docker socket forwarding endpoint.

## API, errors, and events

REST endpoints use `/api/v1`, JSON, request IDs, request size limits, timeouts, validation, and a stable `{error:{code,message,request_id}}` envelope. OpenAPI documents all public routes. External project links are limited to `https`, `http`, and `ssh` (repository-specific allowlist), with safe handling when opening in a new tab. SSE emits versioned state/job events, supports `Last-Event-ID`, and has bounded queues; clients fall back to polling.

## Deployment and operations

Compose builds a multi-stage image, runs Caddy and the Web/API, and uses an explicit persistent data volume and private API network; only Caddy publishes 80/443. Caddy uses a configurable DNS name for automatic HTTPS. Linux amd64/arm64 builds are supported. Agent installs separately as a systemd service with a restricted Unix socket and root-owned policy file. Documentation covers bootstrap, upgrades/migrations, backup/restore, health checks, and troubleshooting. Docker image build and live deployment require a Docker daemon and must be reported unverified when unavailable.

## Delivery gates and known environment constraints

The seven phases in `TASKS.md` define completion. A task is checked only with its test command and result recorded. Authentication/RBAC/CSRF are required before any management API is enabled. Tests must never connect to a real host Docker socket or real systemd services; adapters use a fake engine/transport. Validation uses Go 1.26.x and Node 24.21 with npm; Docker is present only inside WSL without daemon permission, so image builds and deployment smoke tests must be reported as unverified unless access changes.

