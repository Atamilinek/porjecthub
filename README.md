# Project Hub

Project Hub is a self-hosted project catalog, health monitor, and explicitly authorized service control panel. It is one repository and one Go modular monolith, with a separately installed Linux Agent for host actions. The web/API remains useful when no Agent is connected.

## Quick start

1. Install Docker Engine and the Compose plugin, point a DNS name at the host, and allow inbound TCP 80/443.
2. Copy `.env.example` to `.env`, set `DOMAIN` to that DNS name, and run `mkdir -p backups && chmod 700 backups`.
3. Start the web/API and HTTPS gateway: `docker compose up -d --build`.
4. Create the first administrator with a protected password file, following [deployment and initialization](docs/deployment.md).
5. Open `https://$DOMAIN`. Service actions stay unavailable until an Agent is installed and an administrator maps a target explicitly.

The API container has no Docker socket, runs without Linux capabilities, and is reachable only through Caddy's private network. Agent installation is separate and opt-in. Admins can manage accounts, review audit events, and inspect effective runtime settings. See the [API reference](docs/api.md), [operations](docs/operations.md), [Agent security](docs/agent.md), and [the delivery checklist](TASKS.md).

For a host-side isolated startup/login/CRUD smoke test with a temporary database, run `make smoke-local` from Linux with Go, Node.js 24.21.0 (see `.nvmrc`), npm 11.19.0, curl, and Python 3 installed. The script binds a loopback port and never connects to Docker or systemd.
