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

For this Orange Pi, the current root certificate SHA-256 fingerprint is `C2:5D:C4:40:64:A5:1A:7E:6C:26:F3:AE:E2:40:3A:BE:31:6B:06:6C:F2:9D:E1:1B:C7:70:C6:C4:8C:9B:1B:1D`. The currently served leaf certificate has the IP SAN `192.168.1.32`; use `https://192.168.1.32/`. The CA file is `/srv/data/projecthub/certs/projecthub-local-ca.crt` on the Orange Pi. Copy that public certificate over the trusted SSH/LAN channel, then verify its fingerprint before installing it:

```sh
scp <admin>@192.168.1.32:/srv/data/projecthub/certs/projecthub-local-ca.crt .
openssl x509 -in projecthub-local-ca.crt -noout -fingerprint -sha256
```

The printed SHA-256 fingerprint must match the value above. On Windows, `certutil -dump projecthub-local-ca.crt` also shows the certificate's SHA-256 hash. Do not use `certutil -hashfile` as the certificate fingerprint; that hashes the PEM file bytes instead of the X.509 certificate.

### Windows computer

1. Open the `.crt` file and inspect the issuer/fingerprint, or verify it with `certutil -dump` before trusting it.
2. Open `certmgr.msc`, choose **Current User**, then import into **Trusted Root Certification Authorities\Certificates**. This scope limits trust to the signed-in Windows account. Microsoft documents the Current User and Local Computer stores separately; use Local Computer only if all users on that PC need this private CA.
3. Open `https://192.168.1.32/` in Edge or Chrome. Firefox normally uses third-party roots from the operating-system store; if it does not, use Firefox's Certificate Authorities settings and import the same verified root.

### iPhone/iPad

1. Transfer the `.crt` to the device through a trusted channel and compare the certificate fingerprint before installing it.
2. Open the downloaded profile in Settings and install it.
3. Manually enable it under **Settings > General > About > Certificate Trust Settings > Enable Full Trust for Root Certificates**. A manually installed profile is not automatically trusted for TLS; follow Apple's current steps.
4. Open `https://192.168.1.32/`. Do not continue through a certificate warning.

### Android phone

1. Transfer the `.crt` through a trusted channel and verify the fingerprint first.
2. In Settings, find **Security & privacy** or **Security > More security settings > Encryption & credentials > Install a certificate > CA certificate**. Menu names vary by Android release and device maker; use the manufacturer's instructions if these entries differ.
3. Open `https://192.168.1.32/` in the browser. Android apps can choose not to trust user-added CAs, so browser trust must be tested on the actual phone. Never choose a certificate-warning bypass.

References: [Microsoft certificate stores](https://learn.microsoft.com/en-us/windows-server/identity/ad-cs/inspect-certificates-manage-certificate-stores), [Mozilla Firefox certificate authorities](https://support.mozilla.org/en-US/kb/setting-certificate-authorities-firefox), [Apple manual certificate trust](https://support.apple.com/en-us/102390), and [Android certificate settings](https://support.google.com/android/answer/9654714?hl=en).

The Phase 11 check confirmed the served certificate chain with the supplied CA file from the Orange Pi and a Windows LAN client. The computer's certificate store and a phone have not been changed or tested; complete those steps manually and record the browser result. Windows curl on this offline LAN required `--ssl-no-revoke` because Schannel could not obtain revocation status for the private CA; that check still validated the supplied CA and IP SAN. Do not turn off browser TLS verification or treat this curl option as a device trust installation.

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

The live Phase 11 inspection also confirmed Docker's data root is `/srv/data/docker`, `docker.service` requires `srv-data.mount`, and `projecthub.service` requires both that mount and Docker. If `/srv/data` is absent, keep both dependencies intact and restore the NVMe mount before starting the application.

## Management access

All management routes require authenticated sessions, CSRF protection, and same-origin checks. Only Admin may start, stop, or restart services; Viewer and Operator accounts can read status. The Agent separately enforces its root-owned exact-target policy. The initial Orange Pi policy is status-only, so no existing host service can be controlled. To authorize a service later, a host administrator must review and explicitly add its exact identity and actions to `/etc/projecthub/agent-policy.json`, restart only `projecthub-agent.service`, and then create an Admin-managed target mapping. Do not authorize SSH, networking, firewall, Docker, BT Panel, dnsmasq, or mihomo for transitions.

## Troubleshooting

- **Unit waits/fails during boot:** it waits up to 180 seconds for the configured LAN address before Compose starts, then systemd retries after failure. Check `journalctl -u projecthub -b` and Docker/network status. Confirm the reserved address exists.
- **Docker or Project Hub stays down after boot:** check `findmnt /srv/data`, `lsblk -f`, `systemctl status srv-data.mount docker projecthub`, and the Docker unit drop-ins. If Docker's data root is on a separate filesystem, restore that exact device/mount first; do not create an empty `/srv/data/docker` as a fallback, since that can hide the original data and prevent a safe start.
- **Apparent multi-minute boot gap:** compare `journalctl -b -k -o short-monotonic` with `systemd-analyze critical-chain` and chrony logs; NTP can step wall time after boot. If the expected NVMe UUID is absent and PCIe reports link-training errors, keep Docker's data-mount dependency intact and inspect the device/connection before trying a rescan or changing boot timeouts.
- **Port bind failure:** use `ss -ltnp 'sport = :443'`; change the Phase 9 binding only after reviewing the conflicting service. Do not stop an unrelated listener.
- **Agent unavailable:** check `systemctl status projecthub-agent`, `journalctl -u projecthub-agent`, socket owner/mode, and the root-owned policy file.
- **Certificate warning:** install the exported public root certificate and verify its fingerprint; never bypass the browser warning.
- **State is unknown:** check Agent connectivity and `systemctl status <unit>` directly. An Agent outage is not reported as a stopped service.
- **Database issue:** preserve the Docker volume and logs, run the documented integrity/restore procedure, and never remove the volume to make startup succeed.
- **NVMe/PCIe snapshot:** run `sudo /usr/local/bin/projecthub-nvme-health-check`. It prints one JSON line with SMART summary, temperature, media/error-log counters, PCIe endpoint/root-port speed and width, and Root Port link state. It does not write to the drive or change PCIe state, and it omits the SSD serial number. To keep operator-managed history, redirect stdout to a protected log on `/srv/data`; no timer or cron job is installed.
