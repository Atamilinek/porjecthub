# Backup, restore, and troubleshooting

## Consistent backup

The `backup` subcommand uses SQLite `VACUUM INTO` to write a consistent standalone database snapshot while the API is online. It refuses to overwrite an existing file, restricts the output to mode 0600, and rejects using the live database path as the output.

```sh
stamp=$(date -u +%Y%m%dT%H%M%SZ)
docker compose exec -T api /usr/local/bin/projecthub-api backup --file "/backups/projecthub-$stamp.db"
chmod 600 "backups/projecthub-$stamp.db"
sha256sum "backups/projecthub-$stamp.db" > "backups/projecthub-$stamp.db.sha256"
```

Copy backups off-host, encrypt them at rest, and define/verify retention appropriate to your organization. Backing up only to the same Docker volume or host is not disaster recovery. A backup is not verified until it has passed a restore drill.

## Restore

Restore replaces the live database atomically after staging it in the same directory, applying available migrations to the staged copy, and running SQLite `integrity_check`. It refuses the same source and destination. **Stop the API first** so no process can write during replacement. The command intentionally does not stop services itself.

```sh
sha256sum -c backups/projecthub-YYYYMMDDTHHMMSSZ.db.sha256
docker compose stop api caddy
docker compose run --rm --no-deps api restore --file /backups/projecthub-YYYYMMDDTHHMMSSZ.db
docker compose up -d
docker compose logs --tail=100 api
```

The overlay volume keeps the database path writable; the restore command installs a mode-0600 database. Preserve the failed database separately before attempting recovery if the restore command reports an error. Test upgrades/restores on a copy before production changes.

## Agent policy backup

The Agent policy is separate from the application database. Back up `/etc/projecthub/agent-policy.json` securely, preserve root ownership and mode 0600, and record the exact Agent binary version. Restore the policy only after reviewing every target identity and action. Never add a target based only on a display name.

## Troubleshooting

| Symptom | Checks |
|---|---|
| HTTPS certificate unavailable | Confirm `DOMAIN`, DNS A/AAAA, inbound TCP 80/443, and `docker compose logs caddy`. Do not publish the API port as a workaround. |
| API returns 503 / container restarts | Check `docker compose logs api`, free disk space, and that `projecthub-data` is writable by UID 10001. Preserve the volume before repair. |
| Login fails after restore | Confirm the restored database path and timestamp, then inspect API logs. Passwords are not reset by restore. |
| Agent shown offline | On host run `systemctl status projecthub-agent`, inspect `journalctl -u projecthub-agent`, verify socket path/mode/GID, and check overlay `AGENT_SOCKET_HOST`. |
| Action rejected | Check role, target mapping, Agent policy `actions`, immutable Docker ID or exact systemd unit name, and Agent logs. Denials are expected for unknown/protected targets. |
| Health check remains unavailable | Inspect the configured URL/port, timeout/thresholds, DNS answers, and the narrow `PROBE_ALLOW_CIDRS`. Redirects and unsafe/private targets outside the allowlist are rejected. |
| Migration fails | Stop rollout, retain the original database/backup and logs, and restore the pre-upgrade snapshot. Do not manually edit migration tables. |

Never debug by mounting the Docker socket into the API, running commands against real business containers, disabling auth, or broadening probe CIDRs without a specific review.

## Verified coverage and limits

The database command tests create temporary SQLite files, verify backup/restore content and integrity, and confirm corrupt input leaves the live database intact. They do not operate on a production database. Docker image builds/deployment smoke tests require a Docker daemon and must be reported as unverified when unavailable. Backup files are plaintext; protect and encrypt them outside the application.
