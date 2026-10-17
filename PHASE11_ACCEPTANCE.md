# Phase 11 — Formal Launch and Daily Use Acceptance

> **Public version note**: host-specific identifiers in this document have been replaced with placeholders (`<LAN_IP>`、`<HOSTNAME>`、`<NVME_MODEL>`、`<LOCAL_CA_SHA256>`). Acceptance dates, measurements, commands, and pass/fail results are unchanged. Substitute your own environment values when reproducing locally.

Date: 2026-10-11 (Asia/Shanghai)

Target: Orange Pi Zero 3W, Debian 13 ARM64, `<HOSTNAME>`, LAN address `<LAN_IP>`
Storage: `<NVME_MODEL>`, `/dev/nvme0n1p1` mounted at `/srv/data`

This record distinguishes live checks from prior automated tests and work that requires the owner. A checkbox means the stated acceptance criterion has direct evidence; it does not imply unrelated items passed.

## Acceptance results

| Criterion | Result | Evidence / remaining work |
|---|---|---|
| Production and validation Compose inventory | **Verified** | Production Compose project `projecthub` has healthy `projecthub-api-1` and running `projecthub-caddy-1`. Separate `phvalidation20261011` remains: validation API exited 127, validation Caddy running on loopback ports 39081/39443. Project, container, network, and volume inventory collected read-only. |
| LAN HTTPS and certificate identity | **Verified from host and Windows LAN client; device trust pending** | Production Caddy TCP 443 is bound to `<LAN_IP>`; leaf certificate SAN is `IP:<LAN_IP>`. Root CA SHA-256 fingerprint: `<LOCAL_CA_SHA256>`. Pi curl validated `/healthz` with the CA (HTTP 200, verify 0); Windows LAN curl validated `/healthz` and `/` with the CA (HTTP 200, verify 0). Windows curl required `--ssl-no-revoke` because this private CA has no revocation endpoint; this did not bypass chain validation. No computer or phone trust store was changed. |
| API, Docker Socket, and Agent exposure | **Verified** | Compose publishes no API host port; `docker port projecthub-api-1` has no mapping for port 8080. LAN TCP 8080 check failed to connect. Unauthenticated management API reads return HTTP 401. API container has no Docker socket mount; it receives only the Agent socket as a read-only bind mount. Docker daemon is not exposed over TCP. Agent listens on a Unix socket, not a host TCP port. Production Caddy's only host port mapping is `<LAN_IP>:443/tcp`; the `443/udp` shown in container listings has no production host mapping (`docker port` and host socket listing confirm this). |
| Authentication, navigation, categories, live status, Agent policy | **Partly verified; owner login pending** | The deployed API denies unauthenticated project/category/host/admin routes with HTTP 401. Existing server-side role/CSRF/Agent policy tests and Operator UI control tests passed in prior project phases. Current database contains one project, no categories, one admin and one operator, four service targets, four checks; all four checks were healthy and reported actual systemd state. Root-owned Agent policy is mode 0600 and permits only `status` on the four exact protected unit targets; it contains no start/stop/restart action. No service action was sent. A successful interactive login using the owner's credential and browser navigation review remain pending. The category feature exists, but there is no category data to demonstrate on this deployment. |
| Persistent data, backup, and restore | **Backup verified; production restore intentionally not attempted** | Live database is `/srv/data/docker/volumes/projecthub_projecthub-data/_data/projecthub.db`, mode 0600, owner 10001:10001. `/srv/data` is ext4 on `/dev/nvme0n1p1`; Docker data root is `/srv/data/docker`. A production online backup exists at `/srv/data/projecthub/backups/phase11-acceptance-20261011T153627Z.db`, mode 0600, SHA-256 `e7f72da9db9d6c2d3141d8ef485aa3494290ec7252b42f6e04d7ef04394c39ce`. Checksum and SQLite `integrity_check` passed; the backup held the expected existing project, host, service targets, checks, samples, and users. A restore drill passed earlier against an isolated validation DB in Phase 7. Replacing the live production DB was not appropriate for this acceptance. Copy an encrypted backup off-host and schedule a controlled restore drill before claiming disaster recovery complete. |
| Restart policy and data-mount ordering | **Verified** | Production API/Caddy use `restart: unless-stopped`. `docker.service` and `projecthub.service` are enabled and active. Docker requires and follows `srv-data.mount`, with `RequiresMountsFor=/srv/data`; Project Hub requires Docker and the mount, with `RequiresMountsFor=/srv/data/projecthub`. Agent and Project Hub units are active. `/srv/data` is currently mounted. Existing fail-closed behavior must remain if the NVMe mount is absent. |
| NVMe diagnostic and present health | **Script verified and deployed; long-term reliability pending** | Fixture tests pass 4/4. Installed read-only command: `/usr/local/bin/projecthub-nvme-health-check`, SHA-256 `ad02893368bb3731d465cf2ccaf42c42685c2dfd308c6d29d4fdd8a11c436480`. Live snapshot at `2026-10-11T15:45:33Z`: kernel `6.6.98-sun60iw2`; `<NVME_MODEL>` SMART passed; 58°C; critical warning 0; media errors 0; health-log error entries 1; Root Port `0000:00:00.0` Link Up, 8.0 GT/s x1; endpoint `0000:01:00.0` 8.0 GT/s x1. The endpoint's `DLActive-` is not used as Root Port state. Prior intermittent link-training failures remain a long-term risk; one healthy snapshot does not establish reliability. No rescan, link/firmware/DTB/partition change, or reboot was performed. |
| Validation resource cleanup | **Pending owner review; nothing removed** | Validation Caddy is still running on loopback; its API exited 127. Three validation named volumes (`phvalidation20261011_caddy-config`, `phvalidation20261011_caddy-data`, `phvalidation20261011_projecthub-data`) and three validation networks remain. Proposed container/network cleanup after owner reviews that stack: `docker compose -p phvalidation20261011 -f /srv/data/projecthub-deploy-validation-20261011/compose.yaml -f /srv/data/projecthub-deploy-validation-20261011/compose.agent.yaml down`. Do not add `-v`; do not prune or delete volumes/databases. No cleanup has been run. |
| Router reservation and actual phone/browser trust | **Pending owner action** | Router DHCP reservation is not configured or verified. CA has not been installed on the computer or phone, and browser trust has not been tested after installation. Follow the device-specific steps in `docs/lan-deployment.md`; record the final IP and actual browser result. |
| Bootstrap credential cleanup | **Pending owner login** | `/srv/data/projecthub/secrets/bootstrap-admin.password` remains mode 0600, owned by UID/GID 10001. It was not read or used. Confirm the administrator can log in and rotate credentials before securely removing the bootstrap file according to deployment docs. |

The health-log field `num_err_log_entries=1` and media-error count `0` were reported by the NVMe health log. A separate `smartctl -l error` read reported “No Errors Logged”; retain both readings and trend future snapshots rather than treating the health-log count as a media error. The endpoint reports max width x4 because that is the SSD capability, while the board Root Port supports x1; the current 8.0 GT/s x1 link is expected for this platform.

## Current deployment topology

| Scope | Project / resource | Current state |
|---|---|---|
| Production containers | `projecthub-api-1`, `projecthub-caddy-1` | API healthy; Caddy running |
| Production networks | `projecthub_private`, `projecthub_edge`, `projecthub_probes` | Private network is internal; edge and probe networks are separate |
| Production named volumes | `projecthub_projecthub-data`, `projecthub_caddy-data`, `projecthub_caddy-config` | Retained under Docker data root on `/srv/data` |
| API host mapping | `8080/tcp` | Container-only; no host publish |
| Caddy host mapping | `<LAN_IP>:443/tcp` | HTTPS entry point |
| Validation containers | `phvalidation20261011-api-1`, `phvalidation20261011-caddy-1` | API exited 127; Caddy running on loopback only |
| Validation named volumes | `phvalidation20261011_caddy-config`, `phvalidation20261011_caddy-data`, `phvalidation20261011_projecthub-data` | Preserve pending review |

The host also has unrelated existing listeners, including SSH, DNS, nginx, rpcbind, and the BT panel. This acceptance did not modify them or the host firewall. Project Hub's only production published port is TCP 443 on the configured LAN address.

## Manual steps before calling Phase 11 fully accepted

- [ ] Reserve `<LAN_IP>` for the Orange Pi in the router DHCP settings; confirm the reservation after lease renewal.
- [ ] Transfer the public CA certificate over a trusted channel, verify the fingerprint above, and install it on the Windows computer and actual phone. Visit `https://<LAN_IP>/` without bypassing warnings.
- [ ] Sign in with the administrator's human-held credential, confirm project navigation and Admin-only management controls, then rotate the credential and remove the bootstrap password file securely.
- [ ] Review the validation stack and decide whether to run the container/network-only `compose down` command above. Preserve all three volumes unless separately approved after backup/review.
- [ ] Copy an encrypted backup off the board and perform a scheduled restore drill using a separate test database.
- [ ] Collect health-script snapshots and early monotonic PCIe logs across repeated cold boots to quantify the intermittent link-training failure rate. Do not change Gen1/DTB/kernel or bypass `/srv/data` dependencies without a separate approved experiment.

## Verification record

- Production Compose rendered successfully with `docker compose config`; running images report Linux ARM64 from the prior deployment acceptance.
- Current read-only service snapshot: API healthy, Caddy running, Agent active; `/srv/data` mounted as ext4 from `/dev/nvme0n1p1`.
- Production unauthenticated project endpoint: HTTP 401. Windows LAN workstation previously verified `/healthz` and `/` over HTTPS with the exported CA, HTTP 200 and certificate verify result 0.
- Production backup checksum and SQLite integrity check passed. A current production restore was not attempted.
- `go test ./...` and `go vet ./...` passed in Linux/WSL on a Linux filesystem copy of the repository. Native Windows Go tests are not treated as Linux acceptance because Unix permission/socket semantics differ.
- `python3 -m unittest discover -s scripts -p test_nvme_health_check.py -v`: 4 tests passed using fixture sysfs and fake tool output.
- Frontend unit tests first timed out while starting a fork on the Windows-mounted WSL tree. A clean copy on the WSL Linux filesystem passed Vitest 6/6, `vue-tsc -b --pretty false`, and the Vite production build. `npm ci` warned that WSL Node `22.22.1` is below the declared `22.22.2` minimum for several dependencies; the tests/typecheck/build completed successfully with this warning. No Phase 11 UI implementation changed.
- `git diff --check` and Compose validation are part of closeout; results are recorded in `TASKS.md` and the Phase 11 Git evidence entry.
