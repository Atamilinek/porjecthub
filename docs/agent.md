# Linux Agent installation and target authorization

The Agent is a separate Linux binary that exposes only a local Unix socket. It has no TCP listener, shell endpoint, Docker create/exec operation, Compose project control, API-controlled policy editing, or automatic target discovery. The API stores its own binding of a host and target ID; the Agent's root-owned policy remains the authority for exact target identity and permitted operations.

## Install

Build or obtain the version-matched `projecthub-agent` binary for Linux amd64/arm64, verify its release checksum, then install as root:

```sh
install -o root -g root -m 0755 projecthub-agent /usr/local/bin/projecthub-agent
install -d -o root -g root -m 0755 /etc/projecthub
install -o root -g root -m 0600 deploy/agent-policy.example.json /etc/projecthub/agent-policy.json
```

Edit the policy before starting. Use a unique `agent_id`. Each target ID is a stable API mapping key. Docker identities must be a full 64-character container ID; systemd identities must be exact `.service` unit names. Only include targets whose ownership and operational impact you have reviewed. Include `status` plus the smallest necessary action set. Protected core units and Project Hub services are rejected. Docker containers named `projecthub-*` or labeled `com.projecthub.protected=true` / `projecthub.protected=true` are protected.

Set API access to the numeric UID/GID of the API process. Compose API runs as UID and GID 10001 by default. For an API container to connect, Docker's bind-mounted socket path must be accessible at the same path in the container; the Agent creates a mode-0660 socket owned by the configured group. Keep `/run/projecthub-agent` root-owned and non-writable by untrusted users.

The supplied `deploy/projecthub-agent.service` is a hardened starting point. Review its `ExecStart` before install: remove `--docker-endpoint` if there are no Docker targets; remove `--enable-systemd` if there are no systemd targets. Both integration types remain disabled unless their explicit flag/configuration is supplied. The Docker endpoint is a powerful host interface even though the Agent only calls inspect/start/stop/restart for exact allowlisted IDs.

```sh
install -o root -g root -m 0644 deploy/projecthub-agent.service /etc/systemd/system/projecthub-agent.service
systemctl daemon-reload
systemctl enable --now projecthub-agent.service
systemctl status projecthub-agent.service
```

Configure `.env` `AGENT_SOCKET_HOST=/run/projecthub-agent/agent.sock`, then start Compose with the overlay in [deployment.md](deployment.md). In Project Hub, an Admin registers the Agent ID and maps the exact policy target ID to a service/project. An Operator can submit only the actions allowed by RBAC and Agent policy. The Agent rechecks authorization on every request, serializes per target, uses timeouts and idempotency IDs, and verifies state by readback. Jobs interrupted by API restart fail safely and are not replayed automatically.

## Safe changes and recovery

To change policy, stop the Agent, edit a new root-owned mode-0600 file, validate the JSON, inspect every identity/action, atomically install it, and restart the Agent. Policy is read once at process startup; there is no runtime/API reload. Do not add host service targets during test runs. The test suite uses fake Docker/systemd adapters and never calls host daemons. For status collection, use the authenticated Project Hub UI/API and `systemctl` diagnostics for the Agent service itself.
