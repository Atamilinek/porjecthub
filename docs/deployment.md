# Deployment and initialization

## Requirements

- Linux host with Docker Engine and the Docker Compose plugin.
- A DNS record pointing `DOMAIN` at the host; inbound TCP 80 and 443. UDP 443 enables HTTP/3.
- A protected off-host backup destination and a password manager for administrator credentials.
- For host control only: Linux systemd and the separately installed Agent described in [agent.md](agent.md).

The API defaults to loopback for direct development use. Compose overrides it to `0.0.0.0:8080`, but attaches it only to an `internal: true` network shared with Caddy. The API publishes no host port. Caddy has a separate egress network to obtain/renew certificates. The API image does not include Docker CLI or mount the Docker Engine socket.

## Start the application

```sh
cp .env.example .env
# Edit DOMAIN to the DNS name that resolves to this host. Keep PROBE_ALLOW_CIDRS empty initially.
install -d -o 10001 -g 10001 -m 0700 backups
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 api caddy
```

Compose creates the `projecthub-data` named volume for SQLite and named volumes for Caddy state. Keep `.env` out of source control. Caddy provisions TLS after DNS and firewall reachability are correct. `GET /healthz` is available through the site and returns HTTP 200 only when the database responds.

To permit monitoring a private service, add only the smallest required subnet/address to `PROBE_ALLOW_CIDRS`, then recreate the API (`docker compose up -d api`). Do not add loopback, broad private ranges, or networks containing cloud metadata endpoints. HTTP checks reject unsafe DNS answers and redirects.

## First administrator

Make a one-line password at least 12 characters and no more than 1024 bytes. Keep the secret in a mode-0600 file readable by UID 10001 in the container (for example, create a temporary file, set owner/mode to the deployment's mapped UID, and remove it immediately after bootstrap). Then:

```sh
docker compose run --rm --no-deps \
  -v /secure/path/projecthub-admin-password:/run/secrets/admin-password:ro \
  api bootstrap-admin --username admin --password-file /run/secrets/admin-password
```

The command creates/migrates the database and refuses to bootstrap a second administrator. It does not print or accept a default password. Remove the host secret file securely when done. Create additional users through the authenticated admin interface when available; do not reuse the bootstrap secret.

## Upgrade and rollback

1. Read release notes and make a verified backup before changing the image.
2. Pull/build the desired release, then run `docker compose up -d --build`. The API applies numbered transactional migrations before serving requests.
3. Review `docker compose logs --since=10m api`; confirm `docker compose ps` is healthy and sign in.
4. Keep the old image digest and backup until the new release is accepted. To roll back, stop the API, restore the pre-upgrade backup, set the prior image/build revision, then start it. Database migrations are forward-only; restoring the pre-upgrade snapshot is the supported rollback path.

The repository currently builds from source with `docker compose build`; a published multi-architecture image/release pipeline is not yet configured.

## Network and secret handling

- Do not expose port 8080. Put the service behind Caddy or another HTTPS reverse proxy with correct secure-cookie and same-origin behavior.
- Keep the database volume and Caddy `/data` and `/config` volumes persistent. Restrict host access to the Docker daemon, `.env`, backups, and Agent policy.
- The optional Agent overlay mounts only its Unix socket, never `/var/run/docker.sock`:

```sh
docker compose -f compose.yaml -f compose.agent.yaml up -d --build
```

Use the exact socket path configured for the host Agent; configure its permitted GID to match the API container's socket-access group. See [agent.md](agent.md) before enabling host control.
