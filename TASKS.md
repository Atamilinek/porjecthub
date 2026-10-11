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
- [x] Add multi-stage Dockerfile, Compose, Caddy HTTPS configuration, and `.env.example` (`docker compose config --quiet` PASS on Orange Pi Zero 3 with `DOMAIN=localhost`).
- [x] Add systemd Agent unit and secure Linux installation/allowlist instructions.
- [x] Add amd64/arm64 build workflow and ARM64 validation procedure.
- [x] Add data initialization, administrator bootstrap, persistent volume, upgrade/migration, and rollback instructions.
- [x] Add tested backup/restore and troubleshooting documentation.

## Phase 7 — 验收
- [x] Complete API, migration, auth, permissions, SSRF, jobs, and persistence tests.
- [x] Complete UI checks and Playwright E2E, including mobile layout and SSE reconnect.
- [x] Run Go race tests, `go vet`, formatting, TypeScript check, and frontend build.
- [x] Build and transfer offline-deployable ARM64 Docker images for Project Hub API and Caddy; verify loaded image architecture and runtime on Orange Pi Zero 3.
- [ ] Build Docker images for the remaining supported architectures (linux/amd64 container build remains unverified in this acceptance cycle).
- [x] Run deployment smoke test and fault recovery in an isolated Compose project (`scripts/smoke-local.sh` PASS; native ARM64 API smoke PASS; real ARM64 Compose health/UI, backup/restore, and manual fault recovery PASS on Orange Pi Zero 3).
- [x] Record actual Agent start/stop behavior only against an explicitly isolated disposable container; verify denied access to an unallowlisted target.
- [x] Complete README and deployment/operator docs; record limitations, security boundaries, and residual risks.

## Phase 9 — 内网正式上线与自动恢复
- [x] Make service control Admin-only in both API and UI; Go authorization test proves Operator receives 403 and Admin reaches target lookup; Vue unit test verifies Operator sees no service controls.
- [x] Deploy the ARM64 Compose profile on the Orange Pi before reboot. API and Caddy were healthy; TCP 443 was bound only to `192.168.1.32`, API had no published host port, and the existing firewall/host service configurations were not changed.
- [x] Install the restricted Agent as an enabled systemd service. Live Agent status returned `running` for `dnsmasq.service`, `mihomo.service`, `ssh.service`, and `docker.service`; all four persisted health checks reached `healthy`. A protected SSH transition was denied with HTTP 403 and SSH remained active. Agent restart preserved the socket directory and API visibility after recreating only the Project Hub API container.
- [x] Verify LAN-client HTTPS with the exported private CA: Windows `curl.exe` returned HTTP 200 for `/healthz` and `/`, TLS verify result 0. Certificate install instructions are documented for Windows, iOS/iPadOS, and Android.
- [ ] Verify browser/mobile trusted access after installing the CA and reserve the Orange Pi DHCP address in the router; browser/mobile trust and router configuration were not tested or changed.
- [x] Verify actual project and service state before reboot: the configured Orange Pi project and four host targets were readable; four scheduled Agent checks were healthy. Created a production online DB backup before reboot (SHA-256 recorded below); Phase 7 also exercised backup/restore on isolated data.
- [x] Verify API process recovery by terminating only the Project Hub API process after checking its exact executable path; Docker restarted that container and health returned 200. No business container or existing host service was targeted.
- [x] Verify host boot recovery and post-reboot data persistence. At the first post-reboot check, the pre-existing `/srv/data` NVMe filesystem was not enumerated, so Docker and Project Hub correctly waited on their existing mount dependency; no substitute path was created. Once `/dev/nvme0n1p1` (fstab UUID `8a35d80d-23bf-4f61-a468-af9ca52efd32`) appeared and mounted at `/srv/data`, Docker and Project Hub started without a manual start command. Project Hub became active at 22:16 CST. Authenticated post-reboot reads found the original project, host, four service targets, live `running` state for all four units, and 28 healthy samples per monitor. The DB volume and pre-reboot backup remained present with the recorded checksum. Startup depends on the existing data disk being available; if it is absent, startup remains fail-closed until the mount is restored.
- [x] Commit the Phase 9 implementation and actual results as `8b075ee`; keep the remaining host-recovery and device-trust acceptance items unchecked until their external prerequisites are verified.

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
| Phase 7 ARM64 initial preflight (attempt 1; later superseded) | `609d3d1` | On `orangepi-zero3w` (Orange Pi Zero 3, Debian 13, aarch64), `DOMAIN=localhost docker compose config --quiet` PASS. Native ARM64 API binary smoke PASS: bootstrap, startup, healthz, embedded SPA, login, authenticated project mutation, graceful shutdown; used random loopback port and temporary SQLite only; no Docker containers or host services touched. The first on-device image build attempt failed before build steps because the Pi timed out reaching `registry-1.docker.io` while resolving `alpine:3.22`; required base images were absent from cache. No image was created in that attempt. The later offline archive transfer and Compose deployment result is recorded below. | 2026-10-11 |
| Phase 7 ARM64 offline image, Compose, Agent, and recovery validation | `33db891` (Agent ownership fix; image includes this source; Dockerfile build-platform change is `ef8dca1`) | Buildah 1.42.1 on WSL built API image `linux/arm64` after changing Node/Go build stages to native `BUILDPLATFORM`; `npm ci` (185 packages, 0 vulnerabilities), Vue typecheck+Vite build, Go ARM64 build, and final Alpine ARM64 stage PASS. Caddy 2.10 Alpine ARM64 image archived separately. Docker archives and Agent ELF transferred by SCP; SHA-256 matched at source and Pi (final API archive `b4af66f488dab275ecc36a4df221a708884870e97c6757da35aa6cc19d1879f2`; Caddy archive `cd08ad86bfc8de0d22ed7a1bcb8c5d46f97a3e3022db40a18b5909b4883ba958`; fixed Agent ELF `33a17b3287214bcee73ae1e62ca8b8edc9fe92dd6f114d6303d10f8af3349364`). `docker load` PASS; images inspect as `linux/arm64`; final API image runs as UID:GID 10001:10001. `docker compose config --quiet` and real isolated Compose deployment PASS under project `phvalidation20261011`; API healthcheck healthy, HTTPS `/healthz` returned `{"status":"ok"}`, embedded SPA returned HTTP 200. Caddy host ports are loopback only (39081/39443); API has no host port. Unauthenticated management request returned HTTP 401; authenticated admin project/host/target mutation succeeded. The root-owned Agent policy and socket ownership checks exposed incorrect `os.FileInfo.Sys()` type assertions; regression tests reproduced the failures, both assertions now use `syscall.Stat_t`, and Go suite/vet/race plus root-only regression tests PASS. Agent ran with systemd disabled and an exact-ID allowlist for one disposable `sleep` container (`--network none`, zero mounts, zero published ports); authorized stop/start jobs succeeded and status readback matched stopped/running; unallowlisted target status returned HTTP 404. Backup created mode 0600 (SHA-256 `8cbdce4a0ebac3fe140095f1899e36117dcf1ea540b67060a9a5493e9321020c`); after adding a sentinel, stopping API, restoring with the CLI, and starting Compose, the pre-backup project remained and sentinel disappeared; login/list succeeded and restored DB mode was 0600. API process fault injection exited 137; manual Compose `up` recovered service and `/healthz` returned 200. Final API image was then deployed through Compose and Agent stop/start, auth-denial, and allowlist-denial checks passed again. Existing BT-Panel (10402), dnsmasq (53), SSH (22), rpcbind (111), and mihomo loopback (7890) listeners remained present; pre-existing Docker project/container inventory was empty. Agent systemd unit remains inactive; no existing host service or business container was controlled. `linux/amd64` container image build and public-domain/internet-facing production rollout remain unverified. | 2026-10-11 |
| Phase 9 LAN deployment and reboot recovery | `8b075ee` | Admin-only actions verified (Go Operator 403/Admin lookup 404, Vue Operator controls hidden); Go/vet/race and Vue unit/typecheck/build pass. ARM64 production Compose was healthy before reboot; API process auto-recovery passed (restart count incremented; health 200). LAN Windows curl with the exported CA: `/healthz` 200, app 200, TLS verify 0. Agent reported four explicit host units running and all four monitor checks healthy; protected SSH transition returned 403. Production DB backup before reboot: `/srv/data/projecthub/backups/phase9-pre-reboot-20261011T140500Z.db`, SHA-256 `1ea629a6cbccfeba8a02fd862ad19271ba87a464db66c399326bc58812a4ed84`; checksum still matched after reboot. The first post-reboot check found the NVMe absent, and Docker/Project Hub correctly remained stopped on the existing `/srv/data` mount dependency. After `/dev/nvme0n1p1` appeared as UUID `8a35d80d-23bf-4f61-a468-af9ca52efd32` and mounted at `/srv/data`, Docker and Project Hub started without a manual start command; Project Hub became active at 22:16 CST. Authenticated post-reboot API reads confirmed the original project and host, four host-unit states `running`, and four persisted health checks with 28 healthy samples each. The Caddy CA fingerprint was unchanged (`C2:5D:C4:40:64:A5:1A:7E:6C:26:F3:AE:E2:40:3A:BE:31:6B:06:6C:F2:9D:E1:1B:C7:70:C6:C4:8C:9B:1B:1D`). Existing SSH/dnsmasq/mihomo/rpcbind/frpc and Agent are active. Mobile/browser trust and router DHCP reservation remain unverified/not changed. | 2026-10-11 |

