# Deployment and recovery

One service, one durable data volume, one replica. Port4000 remains unchanged.
Compose runs as UID/GID10001 with init, dropped capabilities, a read-only root,
8 MiB /tmp and a 45-second stop grace period. Both staging and published recordings
live under /data. An advisory volume lock rejects a second writer. The image pins
Go/Alpine base digests and FFmpeg8.0.1-r1/fdkaac1.0.6-r0 packages.

Your proxy owns TLS/DNS/access policy; preserve Host and disable buffering for
radio/Mood/SSE, including v1 routes. No WebSocket is needed. Allow long-lived media
responses and do not log lease query strings. Set COOKIE_SECURE=true only when
all browser access is HTTPS. The administrator saves Navidrome/curator settings
through the app; no upstream URL or credential environment variables are needed.

| Environment | Default | Meaning |
| --- | --- | --- |
| DATA_DIR | /data | Encryption key, encrypted records, SQLite/WAL, media |
| CAPTURE_BUDGET_BYTES | 21474836480 | 20 GiB installation capture budget |
| CAPTURE_ACCOUNT_BUDGET_BYTES | 10737418240 | 10 GiB per-account capture budget |
| CAPTURE_RESERVE_BYTES | 1073741824 | 1 GiB filesystem free reserve |
| CAPTURE_RETENTION_DAYS | 0 | Retention disabled; positive days removes old server copies |

Reservations include twice the nominal FDK AAC-LC160k output rate. Native storage
and the management view expose limits, used and available space. Scheduled repeats
reserve their next occurrence, then revalidate storage/source/duration when
advancing; a failed future occurrence is visible. Capacity is checked against a
one-year recurrence horizon and enforced again at dispatch. Keep the server clock
synchronized. DST behavior uses embedded IANA timezone data. Native refresh and
recordings persist; browser cookies expire on restart. /healthz checks the HTTP
process, not station availability or proof of overnight capture. Inspect session
and per-station status for recorder degradation.

## Backup and upgrade

Back up the **whole volume, including key**, with the service stopped. Never copy
a live database without its WAL, never restore only a key or only media, and do
not use compose down -v during upgrades. Archives contain private account data;
protect them with mode0700 directories and restricted access.

```sh
# Substitute the actual Compose project's existing volume name.
mkdir -m 700 -p ~/weazlmusic-backups/release
# Retain the current image before a build replaces the local tag.
# Tag the running image: a prior failed build may have changed the local tag.
docker tag "$(docker inspect weazlmusic-weazltunes-1 --format '{{.Image}}')" \
  weazltunes-web:rollback-release
docker compose stop
docker run --rm --user 0 --entrypoint tar \
  -v weazlmusic_weazltunes-data:/source:ro \
  -v "$HOME/weazlmusic-backups/release:/backup" alpine:3.23 \
  -czf /backup/data.tar.gz -C /source .
# The root archive helper creates a root-owned file; assign it to this operator.
docker run --rm --user 0 --entrypoint chown \
  -v "$HOME/weazlmusic-backups/release:/backup" alpine:3.23 \
  "$(id -u):$(id -g)" /backup/data.tar.gz
chmod 600 ~/weazlmusic-backups/release/data.tar.gz
sha256sum ~/weazlmusic-backups/release/data.tar.gz
# Verify the archive before advancing the checkout.
git pull --ff-only
docker compose up -d --build
curl -fsS http://127.0.0.1:4000/healthz
curl -fsS http://127.0.0.1:4000/api/v1/info
docker compose ps
```

Legacy encrypted files are preserved. Shared presets migrate lazily into encrypted
SQLite; original queue files remain archival. Native sessions/leases, jobs,
manifests and idempotency outcomes use SQLite WAL/FULL synchronous transactions.
Never discard that database to repair an authentication problem. Media assets
are intentionally ordinary M4A files protected by filesystem ownership and scoped
HTTP leases; SQLite encryption does not encrypt the media files themselves.

## Restore and rollback

Stop the current service first. Restore into a **new** empty named volume and
verify key/database/media together before switching Compose to it. This preserves
the original volume for investigation. A data rollback discards changes since the
backup; consider that explicitly before restoring. Start the retained image with
a Compose image override and no build; do not rebuild an old source tree over the
rollback image. Native/recorder features disappear on the pre-v1 image; its
preserved legacy settings remain usable. Current SQLite state is authoritative
for preset edits made after upgrade, so an old server will not see those newer
edits without a deliberate export. See VERIFICATION.md for the deployment's exact
source, backup and rollback tag.

Production access uses nested SSH (the destination is reached through the jumpbox):

```sh
ssh bobp@jumpbox.prendie.io \
  'ssh bobp@weazlmusic.teralab.local "cd /home/bobp/weazlmusic && docker compose ps"'
```

No production playlists/listening history are mutated by acceptance tests. The
fixture command's trusted radio transport is absent from the production command.
Fixtures and six-hour soak run on isolated local volumes.
