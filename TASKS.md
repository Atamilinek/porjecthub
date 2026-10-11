# Project Hub delivery checklist

Tasks are checked only after implementation, relevant tests pass, and evidence is recorded below. A blank status means not complete.

## Phase 1 — 基础工程
- [x] Initialize the Go project.
- [x] Initialize Vue 3 + TypeScript + Vite application.
- [x] Configure SQLite WAL, foreign keys, and versioned transactional migrations.
- [x] Add baseline CI for Go and frontend checks.

## Phase 2 — 项目导航
- [x] Implement project CRUD, project links, category CRUD, preferences, and validated URL schemes.
- [x] Implement real project listing, cards, search/filter/sort/favorites, details, and responsive layout.
- [x] Implement add/edit flows and project navigation.

## Phase 3 — 认证安全
- [x] Implement Argon2id login, one-time administrator bootstrap, secure sessions, logout, expiration, and login throttling.
- [x] Implement Viewer/Operator/Admin server-side RBAC and CSRF/Origin checks before enabling management endpoints.
- [x] Implement audit log and authorization/CSRF tests.

## Phase 4 — 运行监控
- [x] Implement HTTP and TCP health checks with SSRF-safe dialing and bounded response handling.
- [x] Implement explicit monitoring state machine, scheduler, timeout, thresholds, concurrency, caching, and retention.
- [x] Implement authenticated SSE with reconnect support and polling fallback.
- [x] Implement Agent status and Agent-unreachable distinction.

## Phase 5 — 执行 Agent
- [x] Implement separate Linux Agent and restricted Unix socket API.
- [x] Implement Docker SDK adapter for existing, identity-verified allowlisted targets only.
- [x] Implement explicit systemd unit adapter and host-owned allowlist.
- [x] Implement authorization recheck, protected target denylist, per-target lock, async idempotent jobs, state readback, audit, and timeout handling.
- [x] Prove tests use fakes and cannot reach a real Docker socket/systemd manager.

## Phase 6 — 部署
- [x] Add multi-stage Dockerfile, Compose, Caddy HTTPS configuration, and `.env.example` (YAML syntax parsed; Compose-specific validation awaits Docker CLI).
- [x] Add systemd Agent unit and secure Linux installation/allowlist instructions.
- [x] Add amd64/arm64 build workflow and ARM64 validation procedure.
- [x] Add data initialization, administrator bootstrap, persistent volume, upgrade/migration, and rollback instructions.
- [x] Add tested backup/restore and troubleshooting documentation.

## Phase 7 — 验收
- [x] Complete API, migration, auth, permissions, SSRF, jobs, and persistence tests.
- [x] Complete UI checks and Playwright E2E, including mobile layout and SSE reconnect.
- [x] Run Go race tests, `go vet`, formatting, TypeScript check, frontend build, and Docker builds where tools are available (Docker CLI/daemon unavailable here; image build is not claimed).
- [x] Run deployment smoke test and fault recovery against an isolated simulation (local binary + temporary SQLite; container deployment remains unverified).
- [x] Record actual start/stop behavior only against an explicitly isolated mock target.
- [x] Complete README and deployment/operator docs; record limitations, security boundaries, and residual risks.

## Evidence log

| Phase | Commit | Verification commands and results | Date |
|---|---|---|---|
| Planning | `ca45b2e` | Workspace inspected; no pre-existing files, Git repository, or `.codegraph/` found. Architecture/spec/plan committed. | 2026-10-11 |
| Phase 1 | `c491168` | `go test ./...` PASS; `go vet ./...` PASS; `go test -race ./...` PASS on Ubuntu WSL (Go 1.26.0); Linux arm64 API cross-build PASS; Linux database mode 0600 regression PASS; frontend clean-lockfile install, unit 1/1 PASS, typecheck PASS, production build PASS on Node 24.21. Docker build/deploy smoke unverified (daemon permission denied). | 2026-10-11 |
| Phase 2 interim | `74eb336` | `go test ./...` PASS; `go vet ./...` PASS; `go test -race ./...` PASS on Ubuntu WSL. Catalog/category domain CRUD, search, filters, favorites, pagination, URL validation, case-insensitive uniqueness, and ID tests pass. Authenticated API routes and UI remain pending. | 2026-10-11 |
| Phase 3 | `e976945` | Argon2id and bootstrap/session/RBAC/CSRF/Origin/audit HTTP tests PASS; `go test ./...` PASS; `go vet ./...` PASS; `go test -race ./...` PASS on Ubuntu WSL (Go 1.26.0). At that commit, only login/session/logout handlers were registered; catalog routes were added under auth/RBAC/CSRF in the Phase 2/3 integration below. | 2026-10-11 |
| Phase 2/3 integration | `3efe5e0` | Authenticated project/category API CRUD, favorite/search filters, static SPA fallback, authorization/CSRF/API tests PASS; `go test ./...`, `go vet ./...`, and WSL `go test -race ./...` PASS; Vue unit 4/4, typecheck, production build, Playwright Chromium E2E 2/2 (390px layout, create/edit/delete/favorite and denied-save error) PASS. E2E uses a controlled API stub; backend handlers are separately exercised through SQLite-backed httptest integration. Docker build not run. | 2026-10-11 |
| Phase 4 | `f8c82bf` | HTTP/TCP probe, DNS pinning/private-range rejection, thresholds, scheduler, retention, SSE authorization/replay, API RBAC/audit, and migration tests PASS; `go test ./...`, `go vet ./...`, WSL `go test -race ./...` PASS. Vue unit 5/5, typecheck, production build, Playwright Chromium E2E 3/3 including monitor creation PASS. Tests use fake resolvers/dialers and controlled browser API responses; no real host/container endpoints contacted. | 2026-10-11 |
| Phase 5 | `8190d9f` | Go full suite and race tests PASS; `go vet ./...` PASS; Linux amd64/arm64 API and Agent cross-builds PASS; Agent, job, and API integration tests use fake Docker/systemd/Agent backends. Vue unit 5/5, typecheck/build, Playwright Chromium E2E 4/4 PASS. No host Docker socket/systemd service was contacted. | 2026-10-11 |
| Phase 6 | `9cd096f` | Backup/restore CLI tests PASS against temporary SQLite databases, including integrity, corrupt-source preservation, permissions, and stale WAL/SHM cleanup. Compose/CI YAML syntax parsed. Docker CLI/daemon unavailable, so Compose semantic validation and image build are unverified. | 2026-10-11 |
| Phase 7 | `3e6ce1a` | `go test ./...`, `go test -race ./...`, `go vet ./...`, formatting, Linux amd64/arm64 API+Agent builds PASS; Vue unit 5/5, typecheck/build PASS; Playwright Chromium E2E 6/6 PASS including 390px mobile, user/audit/settings, Agent job, and dropped-SSE reconnection/polling. `scripts/smoke-local.sh` PASS with temporary DB: bootstrap, startup, healthz, embedded SPA, login, authenticated project mutation, graceful shutdown. Agent transition tests use fakes only. Docker CLI/daemon unavailable: Docker image build, Compose semantic validation, and container deployment smoke unverified. | 2026-10-11 |

