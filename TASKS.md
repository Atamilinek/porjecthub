# Project Hub delivery checklist

Tasks are checked only after implementation, relevant tests pass, and evidence is recorded below. A blank status means not complete.

## Phase 1 — 基础工程
- [ ] Create the repository layout and Go API/Agent modules.
- [ ] Initialize Vue 3 + TypeScript + Vite application.
- [ ] Configure SQLite WAL, foreign keys, and versioned transactional migrations.
- [ ] Add baseline CI for Go and frontend checks.

## Phase 2 — 项目导航
- [ ] Implement project CRUD, project links, category CRUD, preferences, and validated URL schemes.
- [ ] Implement real project listing, cards, search/filter/sort/favorites, details, and responsive layout.
- [ ] Implement add/edit flows and project navigation.

## Phase 3 — 认证安全
- [ ] Implement Argon2id login, one-time administrator bootstrap, secure sessions, logout, expiration, and login throttling.
- [ ] Implement Viewer/Operator/Admin server-side RBAC and CSRF/Origin checks before enabling management endpoints.
- [ ] Implement audit log and authorization/CSRF tests.

## Phase 4 — 运行监控
- [ ] Implement HTTP and TCP health checks with SSRF-safe dialing and bounded response handling.
- [ ] Implement explicit monitoring state machine, scheduler, timeout, thresholds, concurrency, caching, and retention.
- [ ] Implement authenticated SSE with reconnect support and polling fallback.
- [ ] Implement Agent status and Agent-unreachable distinction.

## Phase 5 — 执行 Agent
- [ ] Implement separate Linux Agent and restricted Unix socket API.
- [ ] Implement Docker SDK adapter for existing, identity-verified allowlisted targets only.
- [ ] Implement explicit systemd unit adapter and host-owned allowlist.
- [ ] Implement authorization recheck, protected target denylist, per-target lock, async idempotent jobs, state readback, audit, and timeout handling.
- [ ] Prove tests use fakes and cannot reach a real Docker socket/systemd manager.

## Phase 6 — 部署
- [ ] Add multi-stage Dockerfile, Compose, Caddy HTTPS configuration, and `.env.example`.
- [ ] Add systemd Agent unit and secure Linux installation/allowlist instructions.
- [ ] Add amd64/arm64 build workflow and ARM64 validation procedure.
- [ ] Add data initialization, administrator bootstrap, persistent volume, upgrade/migration, and rollback instructions.
- [ ] Add tested backup/restore and troubleshooting documentation.

## Phase 7 — 验收
- [ ] Complete API, migration, auth, permissions, SSRF, jobs, and persistence tests.
- [ ] Complete UI checks and Playwright E2E, including mobile layout and SSE reconnect.
- [ ] Run Go race tests, `go vet`, formatting, TypeScript check, frontend build, and Docker builds where tools are available.
- [ ] Run deployment smoke test and fault recovery against an isolated simulation.
- [ ] Record actual start/stop behavior only against an explicitly isolated mock target.
- [ ] Complete README and deployment/operator docs; record limitations, security boundaries, and residual risks.

## Evidence log

| Phase | Commit | Verification commands and results | Date |
|---|---|---|---|
| Planning | pending | Workspace inspected; no pre-existing files, Git repository, or `.codegraph/` found. Go 1.26.5 and Node 24 available. npm and Docker unavailable. | 2026-10-11 |

