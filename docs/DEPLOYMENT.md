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

Reservations include twice the nominal FDK AAC-LC128k output rate. Native storage
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

Production is directly reachable from the current workstation with its trusted SSH entry:

```sh
ssh bobp@weazlmusic.teralab.local
```

The historical nested route, for networks requiring the jumpbox:

```sh
ssh bobp@jumpbox.prendie.io \
  'ssh bobp@weazlmusic.teralab.local "cd /home/bobp/weazlmusic && docker compose ps"'
```

No production playlists/listening history are mutated by acceptance tests. The
fixture command's trusted radio transport is absent from the production command.
Fixtures and six-hour soak run on isolated local volumes.

## Optional Weazlpod cache configuration

`TRANSCODE_BUDGET_BYTES` defaults to 4294967296 (4 GiB); `TRANSCODE_WORKERS`
defaults to 0 (automatic). Automatic concurrency is the minimum of the media
CPU-core budget and derivative-budget preparation slots. A positive value is an
additional ceiling, never an override of the two-core application reserve. Compose
and the standalone binary accept both.
`CAPTURE_RESERVE_BYTES` is also the derivative disk-free reserve; pending
recording commitments take precedence over derivative admission. Each finite
preparation reserves 768 MiB for its bounded source/output, so small remaining
budgets can reject even a small cached-track preparation. Unexpired leases pin
derivatives; pressure evicts only unpinned, unread disposable MP3 files.
DATA_DIR/derivatives holds both durable finalized MP3s and transient staging;
never place it on the container's small /tmp. Startup verifies indexed hashes
and removes only derivative orphans/staging. Backups may retain these expendable
files alongside the authoritative full-volume backup. No migration or revision
bump is required; old AAC assets and native leases stay valid. Restart after media
tool upgrades so the encoder/library fingerprint refreshes. See
[firmware/server contract](WEAZLPOD_FIRMWARE.md). This change was deployed on 2026-10-20; see VERIFICATION.md for its receipt.

## CPU reserve and playlist throughput

All server-launched FFmpeg, ffprobe and fdkaac subprocesses share a CPU affinity
pool excluding **at least two application cores**. Where topology is available,
whole SMT sibling groups are excluded. The Go API retains its full affinity.
Cgroup v1/v2 CPU quotas and visible ancestors are also honored: floor fractional
CPU quotas before subtracting two, and cap worker affinity to that remaining
logical-CPU-time budget. With no safe pool (two or fewer available cores/CPU quota,
or unreadable affinity/quota), new encoding is unavailable; originals, existing
cached-file transfers and other API operations remain usable. No client header,
quality or worker configuration can lower the reserve. Restart after changing
CPU topology/affinity/container quotas.

MP3 libmp3lame reports no internal threading support. Throughput comes from
parallel independent FFmpeg processes, one encoder thread per job, with all
probes, validation, live radio and Flight Recorder media processes using the same
restricted pool. No additional runtime executable/package is required: the server
launches its own short-lived media-exec entrypoint, applies Linux affinity there,
and execs the tool while preserving PID, pipes and cancellation.

The API advertises reservedApplicationCores, transcodeCores, storageWorkerLimit,
maxWorkers, encoderThreadsPerJob and workerModel. At 24 vCPU cores without SMT and
4 GiB cache, media has 22 cores but five conservative preparation reservations;
maxWorkers is five unless an explicit lower ceiling is configured. At four cores
it is two. Actual free-space pressure or existing cached files can further reduce
admission. Increasing workers alone does not enlarge the disk budget. Firmware
can pipeline that many playlist preparations while transferring completed files.

## Weazlpod radio quality verification

The radio-quality extension needs no new environment settings, volume migration,
ports or reverse-proxy route. Preserve streaming/no-buffering behavior on native
media asset routes. `X-Weazl-Radio-Quality` is used at authenticated lease creation;
media GET uses the existing scoped URL. See
[quality contract](API_REFERENCE.md#radio-quality-selection) and
[rollout plan](WEAZLPOD-RADIO-QUALITY-PLAN.md).

Before deployment run `go test -race ./...`, `go vet ./...`, build the production
image, then run `scripts/verify-weazlpod.sh <image>`. This exercises real MP3/AAC
inputs through all three profiles with the image's packaged FFmpeg.

After deployment, verify real HTTPS delivery without firmware:

```sh
python3 scripts/verify-radio-quality.py \
  --credentials /private/path/server-credentials.json \
  --output test-results/radio-quality-production --seconds 60
```

The credentials file contains `server.public_url`, `server.username`, and
`server.password`; never commit it. The default source is
`https://radio.prendie.io/radio.mp3`; `--station-url` selects another source.
The verifier reuses a matching saved station or creates/removes a temporary
non-preset station, creates/logs out its own native session, and never alters
library playlists or listening history. A temporary station changes the station
collection revision even after cleanup. It leaves only private captured audio
and token-free format/timing receipts in the output directory. These are short
server acceptance checks, not listening tests or ESP32 endurance verification.
