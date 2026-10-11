# Project Hub Design

## Purpose

Build a deployable, self-hosted internal project portal that combines project navigation, authenticated administration, health monitoring, and narrowly authorized service control. It must support Linux amd64/arm64 and remain safe when no Agent is configured.

## Approved approach

Use one repository and a Go modular monolith for the API, static web serving, authentication, project catalog, monitoring scheduler, jobs, and SSE. Deploy `projecthub-agent` as a separate Go executable. Use Vue 3/TypeScript/Vite/Router/Pinia and a mature component library. Use SQLite/WAL with migrations. Caddy terminates HTTPS in front of Compose Web/API. There are no external cloud dependencies.

## Core boundaries

1. Project URLs are navigation metadata and are never commands.
2. Service-control authorization lives in a host-managed Agent allowlist that the Web API cannot modify.
3. The API has no Docker Socket mount. Agent accepts fixed typed actions on a permission-restricted Unix socket and verifies target identity, policy, current state, locks, and protected-target rules for every request.
4. No arbitrary shell/command/file API; no automatic adoption or creation of containers; no host-service control in tests.
5. Authentication/RBAC/CSRF middleware must protect all management endpoints before they are made available.

## Work packages in one repository

- Foundation and project navigation.
- Authentication, RBAC, CSRF, sessions, and audit.
- Monitoring, SSRF controls, state history, and SSE.
- Restricted Agent, deployment, and operations documentation.

These are delivery groupings only; they share schemas, API contracts, and one repository.

## Data/API decisions

Use UUID identifiers, foreign keys/indexes, parameterized SQL, numbered transactional migrations, and tables for users, sessions, projects, links, categories, hosts, service targets, health checks/samples, jobs, audit, and preferences. Use `/api/v1`; errors use a stable code/message/request ID envelope. OpenAPI is committed. Health samples have retention cleanup and backup/restore is documented.

## Security decisions

Passwords use Argon2id. Sessions are random opaque server-side records with HttpOnly/SameSite cookies, Secure under HTTPS, expiration, and revocation. Mutations require CSRF and Origin checks. Login attempts are rate-limited. Roles are Viewer, Operator, Admin with server-side checks. The first admin is created with a one-time CLI flow and no published default password.

HTTP checks are admin-configured separately from project links. They resolve, validate, and pin allowed addresses, reject forbidden networks/metadata targets by default, cap redirects/body/concurrency, and cannot use file or Docker protocols. Monitoring states explicitly represent unconfigured, checking, healthy, degraded, offline, unknown, and Agent-unreachable.

## Testing and delivery

Preserve the seven phases and acceptance checkboxes in root `TASKS.md`. Each checked item requires a passing relevant test and evidence entry. Unit/integration tests inject fake Docker and systemd boundaries and must never contact real host services. Run Go formatting, unit/race tests, vet, API/security tests, frontend type/build/E2E, multi-arch image builds, and deployment smoke checks when available; report unavailable checks as unverified.

## Assumptions

- The initial Agent supports Linux only; Windows Agent is a later extension.
- The first deployment manages one API instance and one SQLite database; multi-host coordination uses Agent host identities but does not require distributed API services.
- Hostnames, ports, retention, and network allowances are operator-configured. No unsafe target is allowed by default.
- Current workspace lacks npm and Docker, so those validation steps may remain unverified on this host.

