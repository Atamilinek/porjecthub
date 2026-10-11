# Phase 11 Formal Launch and Daily Use Acceptance Plan

> **For agentic workers:** Execute this plan task by task. The user has authorized the Phase 11 work and specified that device-impacting changes wait for a human.

**Goal:** Verify Project Hub's live LAN deployment and security boundaries, add a read-only NVMe health report, and record verified results and remaining manual steps.

**Architecture:** Keep the existing Go modular monolith, Caddy LAN gateway, Compose deployment, SQLite volume, and separate Agent unchanged. Use read-only host/API checks for live acceptance and an isolated Python unittest fixture for the new NVMe diagnostic script.

**Tech Stack:** Docker Compose, systemd, Caddy internal TLS, Go tests, Python 3 standard library `unittest`, `smartctl`, Linux sysfs, `lspci`.

**Spec:** User's Phase 11 request in this task.

## Global Constraints

- Do not modify device tree, PCIe rate, NVMe firmware, or disk partitioning.
- Do not install the CA on computers/phones, change router DHCP, or perform destructive device operations.
- Do not remove validation containers, networks, Docker volumes, or databases without separate authorization.
- The diagnostic script may read only; tests use fixtures and mocked command output, never the real NVMe or host services.
- A criterion stays unchecked unless its evidence was collected and recorded.

## Review Focus

- NVMe absent or sysfs fields missing: report unavailable without failing noisily or writing to the device.
- `smartctl` missing/failing or malformed JSON: preserve a useful diagnostic report with an explicit reason.
- Root Port visible while downstream link is inactive: distinguish current negotiated speed from link-active state.
- Private CA works for IP SAN but Windows revocation checking may be unavailable on an offline LAN: do not recommend disabling certificate verification.
- Verification Compose state shares `/srv/data` storage: any cleanup must preserve all volumes and data pending manual approval.

---

### Task 1: Live deployment, security, persistence, and recovery audit

**Files:** Create `PHASE11_ACCEPTANCE.md`; update `TASKS.md` and `docs/lan-deployment.md` with the verified outcome and device trust instructions.

- [x] Record Compose projects, container ownership/status, host ports, networks, volumes, database path, systemd ordering, certificate SAN/fingerprint, anonymous API denials, backup integrity, live SMART/link status, and validation-stack cleanup proposal.
- [x] Keep phone trust, router reservation, real admin login with the human-held credential, and any resource removal pending manual action.
- [x] Validate production Compose configuration read-only and probe HTTPS/API exposure from the host and LAN workstation.
- [x] Commit the records and tested documentation as `2eb32af`.

### Task 2: Read-only NVMe health diagnostic

**Files:** Create `scripts/nvme-health-check.py` and `scripts/test_nvme_health_check.py`.

- [x] Write fixture-based tests for populated SMART + PCIe results, missing NVMe/SMART tools, and inactive Root Port links; verify expected failures before implementation.
- [x] Implement JSON stdout reporting with model, temperature, SMART pass/warnings/media errors/error-log count, endpoint/root-port speed and width, `lspci` link-active status, kernel, and timestamp; never emit the SSD serial number.
- [x] Run the tests with only temporary fake sysfs files and mocked `smartctl`/`lspci` output; run the script once on the Pi as a read-only health snapshot after transfer is authorized/available.
- [x] Document a manual invocation and optional shell redirection for operator-managed history; do not install cron/timers.

### Task 3: Acceptance closeout

**Files:** Update `PHASE11_ACCEPTANCE.md`, `TASKS.md`, and the evidence log.

- [x] Run relevant Go and frontend suites, the new Python tests, and whitespace/config checks. Linux filesystem copies passed the Go and frontend suites; Python fixtures passed; Compose validation and diff whitespace checks passed.
- [x] Mark only verified criteria complete; leave phone/router, actual administrator credential login, long-term PCIe reliability, and validation resource cleanup open as applicable.
- [x] Commit the Phase 11 deliverables with actual test results as `2eb32af`.
