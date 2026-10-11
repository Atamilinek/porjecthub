# Orange Pi LAN deployment

This profile runs the preloaded ARM64 API image behind Caddy with a private local CA. It publishes only TCP 443 on the explicitly configured LAN IPv4 address. The API is not published to the host. The SQLite database, Caddy CA, and Caddy configuration use named Docker volumes; Compose containers use `unless-stopped`, and systemd brings the project up after Docker and the network are available.

## Before installation

1. Confirm the Orange Pi's current Wi-Fi LAN IPv4 address with `ip -4 addr show wlan0` and reserve that address in the router's DHCP settings. The service binds only to that address, so it will fail closed if the address changes unexpectedly.
2. Check that TCP 443 is unused on that address. This deployment does not change UFW or any existing host-service configuration.
3. Transfer the API image and Caddy image archives from the matching ARM64 release, verify their SHA-256 digests, and load them with `docker load`. Set the exact loaded image tags in `/srv/data/projecthub/.env`. The service uses `pull_policy: never` and does not need registry access at boot.
4. Install the `projecthub-agent` binary built from this repository to `/usr/local/bin/projecthub-agent`. The API image uses GID 10001 for its socket access; if GID 10001 is unused on the host, create the dedicated group `projecthub-api` with that GID and use it in the Agent unit. Create `/etc/projecthub/agent-policy.json` from `deploy/lan/agent-policy.json.example`, review every target, and set root ownership and mode 0600. The example grants **status only** for dnsmasq, mihomo, SSH, and Docker. It does not control Docker containers or start/stop/restart host units.

## Install and initialize

Create `/srv/data/projecthub`, copy `compose.yaml` and `Caddyfile` from `deploy/lan/`, create `backups/` mode 0700 owned by UID/GID 10001, and create a protected `.env` file with the image tags and current LAN IPv4 address:

```dotenv
API_IMAGE=projecthub-api:phase9-arm64
CADDY_IMAGE=caddy:2.10-alpine
LAN_BIND_IP=192.168.1.32
LAN_HOST=192.168.1.32
AGENT_RUNTIME_DIR_HOST=/run/projecthub-agent
PROBE_ALLOW_CIDRS=
```

Set the IP to the reserved address you actually selected. Keep this file mode 0600. Install the unit files and enable them:

```sh
install -o root -g root -m 0644 deploy/lan/projecthub-agent.service /etc/systemd/system/projecthub-agent.service
install -o root -g root -m 0644 deploy/lan/projecthub.service /etc/systemd/system/projecthub.service
install -o root -g root -m 0755 deploy/lan/wait-for-lan-ip.sh /usr/local/libexec/projecthub-wait-lan-ip
systemctl daemon-reload
systemctl enable --now projecthub-agent.service
systemctl enable --now projecthub.service
```

Generate an initial admin password of at least 12 characters into a temporary protected host file, then run the one-time bootstrap command using a read-only bind mount to `/run/secrets/admin-password`. Remove the temporary secret after bootstrap and store the credential securely. Bootstrap refuses to create a second initial admin.

For the deployed Orange Pi, the generated initial password is kept at `/srv/data/projecthub/secrets/bootstrap-admin.password`, owned by UID/GID 10001 with mode 0600 under a root-only directory. Retrieve it over SSH with `sudo cat` for the first login, change it in a password manager, then delete the file after confirming the new credential works.

Check `systemctl status projecthub-agent projecthub`, `docker compose -p projecthub -f /srv/data/projecthub/compose.yaml ps`, and `curl --cacert /srv/data/projecthub/certs/projecthub-local-ca.crt https://<LAN-IP>/healthz`.

## Trust the HTTPS certificate on devices

Caddy creates a private root CA on first start and stores its private key in the persistent `projecthub_caddy-data` volume. Export only the public root certificate to `/srv/data/projecthub/certs/projecthub-local-ca.crt`; do not copy the private key. Transfer that certificate to each LAN computer or phone over the trusted LAN/SSH channel, compare its SHA-256 fingerprint over a separate trusted channel, and install it as a trusted root certificate on that device. The certificate file is not secret, but trust in it is security-sensitive. Browsers will then validate `https://<LAN-IP>` without bypassing certificate warnings. Removing/recreating the Caddy data volume creates a new CA and requires re-trusting it on every device.

For this Orange Pi, verify the root certificate SHA-256 fingerprint is `C2:5D:C4:40:64:A5:1A:7E:6C:26:F3:AE:E2:40:3A:BE:31:6B:06:6C:F2:9D:E1:1B:C7:70:C6:C4:8C:9B:1B:1D`. On Windows, open the `.crt` file, choose **Current User**, then **Trusted Root Certification Authorities**. On iOS/iPadOS, install the downloaded profile and enable full trust for the CA in Certificate Trust Settings. On Android, install it as a CA certificate from the device security settings; exact menu names vary by vendor/version. Do not use certificate-warning bypasses.

The Orange Pi address is DHCP-managed. A router reservation keeps the URL and certificate hostname stable; if the address changes, update both `LAN_BIND_IP` and `LAN_HOST` in `.env`, re-create Caddy, and re-export/trust the CA only if its root certificate changed. Do not expose the internal CA or Project Hub ports through router port forwarding.

## Data, restart, backup, and restore

The SQLite database resides in Docker volume `projecthub_projecthub-data`; Caddy CA state resides in `projecthub_caddy-data`. These volumes survive container replacement and host reboot. Avoid `docker compose down -v`, `docker volume prune`, and Docker-wide prune commands.

Create a consistent online backup with:

```sh
stamp=$(date -u +%Y%m%dT%H%M%SZ)
docker compose -p projecthub -f /srv/data/projecthub/compose.yaml exec -T api \
  /usr/local/bin/projecthub-api backup --file "/backups/projecthub-$stamp.db"
sha256sum "/srv/data/projecthub/backups/projecthub-$stamp.db" > \
  "/srv/data/projecthub/backups/projecthub-$stamp.db.sha256"
```

Copy encrypted backups off the Orange Pi and perform periodic restore drills. For restore, verify the checksum, stop the service, restore the backup with `projecthub-api restore` using a one-off API container with the same persistent volume, then start the service. Keep a copy of the current DB before restore; do not run a restore while API writes are active. See [operations.md](operations.md) for atomic restore behavior and troubleshooting.

The systemd unit starts Compose on boot. It requires Docker but only wants the Agent, so an Agent maintenance restart does not stop the web/API stack. Compose mounts the Agent runtime directory read-only, allowing the API to see a replacement socket after Agent restart. Docker's `unless-stopped` policy restarts crashed API/Caddy containers. After changing the image or configuration, use `systemctl restart projecthub.service`; do not restart Docker globally on a host with unrelated containers.

## Management access

All management routes require authenticated sessions, CSRF protection, and same-origin checks. Only Admin may start, stop, or restart services; Viewer and Operator accounts can read status. The Agent separately enforces its root-owned exact-target policy. The initial Orange Pi policy is status-only, so no existing host service can be controlled. To authorize a service later, a host administrator must review and explicitly add its exact identity and actions to `/etc/projecthub/agent-policy.json`, restart only `projecthub-agent.service`, and then create an Admin-managed target mapping. Do not authorize SSH, networking, firewall, Docker, BT Panel, dnsmasq, or mihomo for transitions.

## Troubleshooting

- **Unit waits/fails during boot:** it waits up to 180 seconds for the configured LAN address before Compose starts, then systemd retries after failure. Check `journalctl -u projecthub -b` and Docker/network status. Confirm the reserved address exists.
- **Docker or Project Hub stays down after boot:** check `findmnt /srv/data`, `lsblk -f`, `systemctl status srv-data.mount docker projecthub`, and the Docker unit drop-ins. If Docker's data root is on a separate filesystem, restore that exact device/mount first; do not create an empty `/srv/data/docker` as a fallback, since that can hide the original data and prevent a safe start.
- **Port bind failure:** use `ss -ltnp 'sport = :443'`; change the Phase 9 binding only after reviewing the conflicting service. Do not stop an unrelated listener.
- **Agent unavailable:** check `systemctl status projecthub-agent`, `journalctl -u projecthub-agent`, socket owner/mode, and the root-owned policy file.
- **Certificate warning:** install the exported public root certificate and verify its fingerprint; never bypass the browser warning.
- **State is unknown:** check Agent connectivity and `systemctl status <unit>` directly. An Agent outage is not reported as a stopped service.
- **Database issue:** preserve the Docker volume and logs, run the documented integrity/restore procedure, and never remove the volume to make startup succeed.
