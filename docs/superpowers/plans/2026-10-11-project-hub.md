# Project Hub Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the authenticated, deployable Project Hub catalog, monitoring system, and host-isolated restricted Agent described in the approved design.

**Architecture:** One repository contains a modular Go API and a separate Go Agent binary, a Vue SPA, migrations, deployment assets, and documentation. Auth/RBAC/CSRF are mandatory middleware before management routes are enabled; the Agent is governed by host-owned policy and tested only through fakes.

**Tech Stack:** Go, SQLite/WAL, Vue 3, TypeScript, Vite, Vue Router, Pinia, Naive UI, SSE, Docker Compose, Caddy, systemd.

**Spec:** `docs/superpowers/specs/2026-10-11-project-hub-design.md`

## Global Constraints

- Keep all components in one repository; use a modular monolith plus one separate Agent executable.
- Use `/api/v1`; do not expose unauthenticated management APIs.
- Keep navigation URLs separate from monitoring URLs and service-control policy.
- Agent policy is host-owned and cannot be changed through the Web API.
- Never mount Docker Socket into Web/API; never execute arbitrary shell commands.
- Deny unknown targets and actions; never auto-create or adopt services.
- Never allow automated tests to control host Docker/systemd services.
- Check off work only after relevant test results are recorded in `TASKS.md`.
- Support linux/amd64 and linux/arm64; report unavailable checks as unverified.

## Review Focus

- A Viewer or anonymous caller attempting a mutation must be denied by middleware and tested before handlers are enabled.
- A URL that resolves to a forbidden or changed DNS address must never be dialed; tests pin resolver and dialer behavior.
- An Agent request with a known ID but changed container identity or missing policy must be denied before invoking the backend.
- Duplicate action requests and concurrent requests for one target must not perform conflicting transitions.
- Agent loss must produce Agent-unreachable/unknown state rather than stopped.

---

### Task 1: Repository foundation and SQLite migrations

**Files:** Create Go module layout under `cmd/api`, `cmd/agent`, `internal/{config,store,migrate}`, `migrations/`; create `go.mod`, `.gitignore`, Makefile, CI workflow, baseline tests; scaffold `web/` Vue/Vite/TypeScript app.

**Interfaces:**
- Produces `store.Open(path string) (*sql.DB, error)` configured with WAL, foreign keys, and bounded connections.
- Produces `migrate.Apply(ctx context.Context, db *sql.DB, fs.FS) error` with numbered, transactional migrations.
- Produces `config.LoadFrom(lookup func(string) string) (Config, error)` with a loopback default listener.
- Produces an API health handler; management routes remain unregistered until authenticated routing exists.

- [x] Add migration tests for fresh DB, repeat application, and preservation across reopen.
- [x] Confirm new-database test fails before migration implementation.
- [x] Implement schema with required tables, indexes, foreign keys, and UUID text IDs.
- [x] Run migration and store tests plus `go test ./...`.
- [x] Run frontend unit test, typecheck, and production build using the pinned lockfile.
- [x] Commit Phase 1 foundation and record command/results in `TASKS.md`.

**Additional security regression:** `TestOpenCreatesDatabaseWithOwnerOnlyPermissions` failed first on Linux with mode 0644, then passed after `store.Open` pre-created and chmodded the database to 0600.

### Task 2: Project catalog domain and validation

**Files:** `internal/projects`, `internal/categories`, migration additions, domain/repository tests. Do not register or expose management HTTP routes in this task.

**Interfaces:** Project/category repositories and service methods with validated inputs and typed results. HTTP handlers and OpenAPI are added only in Task 3's authenticated route registry.

- [x] Test CRUD, pagination, search, category filter, favorites/preferences, and allowed link schemes at the service/repository boundary.
- [x] Test invalid IDs, duplicate links, missing categories, and case-insensitive name conflicts. Malformed JSON is tested at the HTTP boundary in Task 3.
- [x] Implement parameterized repository queries and domain validation.
- [x] Run repository tests; keep management handlers unavailable until Task 3 installs authentication/RBAC/CSRF middleware.
- [x] Commit catalog domain work and record partial Phase 2 evidence; authenticated HTTP routes are added in Task 3.

### Task 3: Authentication, RBAC, CSRF, sessions, audit

**Files:** `internal/auth`, `internal/rbac`, `internal/audit`, middleware, `cmd/api` route registration, auth migrations/tests.

**Interfaces:** `Principal{UserID, Role}`, middleware for authentication, role checks, CSRF, and same-origin validation; bootstrap CLI accepts secret from protected stdin/file only.

- [x] Test Argon2id verification, one-time bootstrap refusal, session rotation/expiry/revocation, cookie attributes, and login throttling.
- [x] Test anonymous and Viewer mutation denial, role matrix, missing/invalid CSRF, and invalid Origin.
- [x] Test malformed JSON and invalid API request bodies at the authenticated HTTP boundary.
- [x] Add audit on mutations/actions with request ID and outcome, excluding secrets.
- [x] Gate all management route registration behind auth and role middleware; no insecure development bypass.
- [x] Run auth/RBAC/API tests and `go test -race ./...`; commit Phase 3 and record evidence.

### Task 4: Web application and real API integration

**Files:** `web/` Vite/Vue app, pages/components/stores/router/API client, Go static embed/serving configuration, browser tests.

**Interfaces:** Typed API client consumes `/api/v1`; routes for login, overview, project detail/editor, categories, hosts, audit, and settings. Role-aware UI is usability only; backend remains authoritative.

- [ ] Implement project overview/detail/editor/categories with actual API data, loading/error/empty states, filters, sorting, and favorites.
- [ ] Implement login and administrative navigation only after authenticated server integration.
- [ ] Test component/API behavior and responsive/mobile views; add Playwright E2E for login, CRUD, permission-denied result, and error state.
- [ ] Run frontend typecheck/build/E2E when npm is available; otherwise record exact unverified commands.
- [ ] Commit Phase 2/3 web integration and evidence.

### Task 5: Health monitoring, SSRF defenses, SSE

**Files:** `internal/monitor`, checker adapters, scheduler/state machine, migrations, event broker, SSE handlers, security tests.

**Interfaces:** Probe interface with bounded context; `State` enum; explicit configuration and admin-only mutations; SSE broker with event IDs and bounded subscriber queues.

- [ ] Test HTTP/TCP timeouts, threshold transitions, recovery, retention cleanup, concurrency limits, and Agent-unreachable distinction.
- [ ] Test SSRF rejection for loopback/private/link-local/metadata, DNS rebinding, redirects, credentials, unsupported schemes, and response-size bounds using injected resolver/dialer.
- [ ] Implement scheduling with default 30-second interval and 5-second HTTP timeout; persist checks/samples and publish state changes.
- [ ] Test SSE authorization, Last-Event-ID/reconnect, overflow behavior, and polling fallback contract.
- [ ] Run monitoring/security tests and race tests; commit Phase 4 with evidence.

### Task 6: Restricted Agent and asynchronous jobs

**Files:** `cmd/agent`, `internal/agent`, Linux socket/systemd adapters, Docker SDK adapter, policy config examples, API job handlers, fake-engine tests.

**Interfaces:** Typed `ActionRequest{RequestID, TargetID, Action}`; `Backend` status/transition interface; host allowlist maps target IDs to immutable identity, allowed actions, and protected flag. Unix socket protocol returns structured errors/results.

- [ ] Test policy deny-by-default, protected targets, identity mismatch, missing target, unsupported actions, and real backend not being invoked on denial.
- [ ] Test duplicate idempotency key, per-target lock, timeout, status readback, Agent offline, and Docker failure with fakes only.
- [ ] Implement Docker SDK operations against existing container identities only; no create/exec/compose-up methods.
- [ ] Implement allowlisted systemd unit operations using a narrow systemd API; no shell/systemctl arbitrary command path.
- [ ] Test Unix socket mode/peer identity on Linux; make API-Agent client fail closed when socket unavailable.
- [ ] Run Agent/API security suite and cross-compile Linux amd64/arm64; commit Phase 5 with evidence.

### Task 7: Deployment, operations, and acceptance

**Files:** Dockerfile, `compose.yaml`, Caddyfile, `.env.example`, systemd unit, install scripts, `README.md`, `docs/deployment.md`, backup/restore/troubleshooting docs, CI release workflow.

**Interfaces:** Container listens privately; Caddy public HTTPS entry; Agent installation uses dedicated account and root-managed allowlist. Admin bootstrap and backup commands are documented and repeatable.

- [ ] Validate Compose configuration, environment handling, persistent volume, TLS hostname, and API network isolation.
- [ ] Test backup/restore against a temporary SQLite database and verify migration/upgrade procedure.
- [ ] Build linux/amd64 and linux/arm64 binaries/images; run isolated deployment smoke test when Docker is available.
- [ ] Run final `gofmt`, `go test -race ./...`, `go vet ./...`, TypeScript checks, frontend build, API/E2E tests, and report unavailable checks without claiming success.
- [ ] Complete security boundary/residual risk docs, update all seven Phase checkboxes/evidence, and commit Phase 6/7 results.

