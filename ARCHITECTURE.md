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
- **Agent:** A distinct Go binary, Linux systemd unit, and Unix domain socket. It accepts only typed target IDs and the fixed actions `status`, `start`, `stop`, and `restart`. A root-managed allowlist defines target identity, supported operations, and protected resources. No API can edit this allowlist.
- **Gateway:** Caddy terminates HTTPS. The API listens only on the Compose private network. SQLite, credentials, certificates, and Agent policy files are persistent or host-managed secrets and are never committed.

## Repository and modules

One repository contains the API, web UI, Agent, migration files, deployment files, tests, and operator documentation. The Go API is a modular monolith with packages for configuration, persistence/migrations, auth/RBAC, projects/catalog, monitoring/SSRF, jobs/audit, and HTTP transport. The Agent has a separate command and packages for socket transport, policy loading, Docker/systemd adapters, and target locking. Interfaces isolate Docker/systemd from policy and job logic so tests use fakes only.

The SQLite schema uses UUID identifiers, foreign keys, indexes, parameterized SQL, WAL, bounded connection concurrency, and numbered migrations. Migration application is transactional. The database directory is created with owner-only permissions and the database file is restricted to mode 0600 on POSIX systems. Health samples have configurable retention and scheduled cleanup. Backup uses SQLite's online backup mechanism or a documented quiesced-copy procedure; restore is validated before service start.

## Authentication and authorization

All management routes require an authenticated session and server-side RBAC. Viewer can read authorized catalog/state data; Operator can submit actions only for targets the server has bound to a host allowlist ID; Admin can manage projects, links, categories, hosts, users, and settings. The first-run command creates the initial admin interactively or from a one-time secret supplied through a protected file/stdin; it has no default password and refuses to reset an existing account implicitly.

Passwords use Argon2id with the OWASP baseline parameters (19 MiB, 2 iterations, 1 lane); passwords must be 12–1024 bytes. Sessions use random 256-bit opaque tokens; only SHA-256 token hashes are persisted. A session expires after 12 hours, rotates on successful login when an old cookie is present, and is revoked on logout. Cookies are HttpOnly, Secure, SameSite=Strict, and path-scoped. Mutations require a per-session CSRF token and same-origin validation. Login throttles each source/account pair after 5 failures and each source after 20 failures per 15 minutes. The bootstrap command accepts the initial secret only from a regular protected file (mode 0600 on POSIX) and refuses a second administrator bootstrap. Authentication and authorization middleware are installed before management handlers are registered. Login and logout audit records include actor where known, request ID, action, timestamp, and outcome without passwords or session secrets.

## Monitoring and SSRF controls

HTTP(S), TCP, and Agent-reported container/process checks have explicit configured targets, bounded deadlines, bounded concurrency, failure/recovery thresholds, cached states, and retained samples. States distinguish unconfigured, checking, healthy, degraded, offline, unknown, and Agent-unreachable; Agent loss never implies a service is stopped.

HTTP checks reject credentials, unsupported schemes, unapproved hosts, loopback/link-local/private/metadata destinations unless a narrow admin-configured network policy explicitly permits them, and DNS answers that change into forbidden ranges. The checker resolves and validates addresses before dialing, pins the chosen validated address for the request, disables redirects by default (and revalidates any explicitly allowed redirect), caps response bytes, and cannot access local files or Docker APIs. Monitoring configuration is admin-only and distinct from project navigation URLs.

## Service control and jobs

The Web API stores host/target bindings but cannot create Agent policy entries. The Agent reloads a root-owned allowlist, verifies target identity and operation on every request, denies protected core services, checks current state, serializes each target, and returns explicit not-found/unavailable errors. Docker control uses the Docker Engine API through the Go SDK and only existing container IDs/labels or pre-authorized Compose project membership; it never creates containers. systemd control is limited to named allowlisted units through a fixed D-Bus/systemd interface, never a generic command endpoint.

Actions are asynchronous, time-bounded, idempotent by request ID, and recorded in `action_jobs` and `audit_logs`. The API confirms state by reading it back after an action. Agent socket access is limited by owner/group/mode and peer credentials where supported. There is no shell, arbitrary file operation, or raw Docker socket forwarding endpoint.

## API, errors, and events

REST endpoints use `/api/v1`, JSON, request IDs, request size limits, timeouts, validation, and a stable `{error:{code,message,request_id}}` envelope. OpenAPI documents all public routes. External project links are limited to `https`, `http`, and `ssh` (repository-specific allowlist), with safe handling when opening in a new tab. SSE emits versioned state/job events, supports `Last-Event-ID`, and has bounded queues; clients fall back to polling.

## Deployment and operations

Compose runs Caddy and the Web/API, with an explicit persistent data volume and private API network. Caddy config uses a configurable internal DNS name and requires HTTPS for management. Multi-arch images target linux/amd64 and linux/arm64. Agent installs separately as a systemd service with a dedicated account, restrictive filesystem/socket permissions, and a root-managed policy file. Documentation covers bootstrap, upgrades/migrations, backup/restore, health checks, and troubleshooting.

## Delivery gates and known environment constraints

The seven phases in `TASKS.md` define completion. A task is checked only with its test command and result recorded. Authentication/RBAC/CSRF are required before any management API is enabled. Tests must never connect to a real host Docker socket or real systemd services; adapters use a fake engine/transport. Validation uses Go 1.26.x and Node 24.21 with npm; Docker is present only inside WSL without daemon permission, so image builds and deployment smoke tests must be reported as unverified unless access changes.

