# WeazlTunes web

A browser listening desk and native Subweazl backend for Navidrome and Internet radio, with
`weazlhead` branding. One Go binary serves the UI, authenticated API, and audio.
No frontend framework, Node runtime, cloud identity, or telemetry.

## Run with Docker

```sh
docker compose up --build -d
```

Open **http://localhost:4000**, or your machine's IP on port 4000. Compose publishes
**0.0.0.0:4000**. No Navidrome server is configured in Docker or environment variables.

Fresh data volumes include a local-only administrator: `weazladmin` / `admin`.
Sign in and use **Account → Change password** immediately. This password is for
WeazlMusic and is never sent to Navidrome. The administrator can open
**Account → Installation settings** to save the shared Navidrome backend and
curator configuration.

1. Sign in as `weazladmin` / `admin` on a fresh data volume and change the local
   password immediately.
2. Open **Account → Installation settings** and save the shared Navidrome URL.
3. Sign out, then sign in with any existing Navidrome username and password.

Normal users authenticate against that configured Navidrome server. The app stores
only the Navidrome API token and salt, never the password. The local administrator
login remains available if Navidrome is offline. Existing pre-passthrough accounts
can finish migration through the compatibility path; new users are created in the
app on their first successful Navidrome login.

Your reverse proxy handles the domain and TLS. The app serves plain HTTP and
requires no public URL setting. Forward to port 4000 and preserve the request
Host header. For an exclusively HTTPS entry point, set `COOKIE_SECURE=true`;
leave it false for direct HTTP access. Stream routes send `X-Accel-Buffering: no`
for radio and Mood; proxies should stream responses rather than buffer them.

| Setting | Default | Purpose |
| --- | --- | --- |
| `LISTEN_ADDR` | `0.0.0.0:4000` | HTTP listener |
| `DATA_DIR` | `./data` (`/data` in Docker) | Encrypted user settings, SQLite, recording assets and encryption key |
| `COOKIE_SECURE` | `false` | Restrict session cookies to HTTPS when enabled |

`localhost` inside a container means that container. Use a reachable server URL
or a service name on a shared Docker network for Navidrome and your LLM endpoint.

## What works

- Navidrome sign-in, an independent local administrator, browser sessions,
  persistent revocable native devices, and encrypted upstream connection settings.
- A horizontal shelf of 16 recently added albums, paginated album browsing,
  a scrolling sidebar, search, real cover art,
  and favorites saved to Navidrome.
- Browser playback, seek, volume, previous/next, shuffle, queue reordering,
  queue restoration, and mobile queue access.
- Create a playlist or save the current track and queue as a playlist. Rename,
  append queued tracks, remove tracks, and delete playlists. **All playlist
  operations write directly to the configured Navidrome user's account.**
  Other users' playlists are readable only as allowed by Navidrome and cannot
  be edited through this app. A successful save means Navidrome accepted it.
- Seven public Weazltunes presets, in their original order. Per-user
  station additions, preset toggles, and station removal.
- On-demand SomaFM and Icecast directories, PLS/M3U resolution, and a same-origin
  radio relay for HTTP/HTTPS audio. Switching to radio keeps the library queue;
  live ICY titles show artist and track when the station supplies them, using
  the same audio connection.

Shortcuts: `/` search, `1` home, `2` albums, `3` favorites, `4` playlists,
`5` radio, `6` queue, `M` build Mood, Space play/pause, `N`/`P` next/previous, `?` help.

## Random play and Mood

**Pick up the needle** immediately plays a random library track and replaces the
queue with 30 more random tracks (or as many as a smaller library supplies).

Under **Account → LLM curator**, choose Ollama or vLLM, enter its reachable
endpoint, load models, select a model, and save. An optional API key is encrypted
with your account settings and never returned to the browser.

Play a library track, then press **M** or the **✧** player button. Mood uses that
track as its seed and creates or replaces your Navidrome user's **Mood** playlist.
The target is 20 tracks including the seed. Each validated selection is written
to Navidrome and appears in the playlist as the model streams; upcoming selections
also enter the queue without interrupting the current track. Selection uses up
to 240 library candidates, and Go enforces unique, real IDs and the track count.

**Stop Mood**, closing the page, or signing out cancels generation. Tracks already
accepted remain saved, including after an endpoint failure. No LLM runs while idle.
Playlists have one navigation surface; the redundant sidebar Mixtapes list is gone.

## Storage and operation

The named Docker volume `weazltunes-data` contains an AES-GCM key and encrypted
per-user queue/radio settings. Back up the **whole volume, including the key**.
Playlists and favorites remain in Navidrome. Avoid `docker compose down -v`
unless you intend to discard the web app's stored settings.

Local passwords are salted and hashed using PBKDF2-HMAC-SHA256 (600,000 rounds).
Navidrome passwords are exchanged for API authentication tokens, then discarded;
those tokens and each account's connection settings are encrypted at rest with
AES-GCM. Neither passwords nor upstream tokens are sent back to the browser.
The password-hashing work factor follows the
[OWASP password storage guidance](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html#pbkdf2).

Restarting invalidates browser cookies; native refresh sessions, accounts,
connections, schedules and recording jobs survive. Logging out cancels the session's streams. Changing a
Navidrome connection rotates the current session and revokes other sessions for
that web-app user. Saved queues/settings are isolated by both local account and
upstream identity, so changing servers does not replay IDs from the previous
library. Previous first-release queue/radio settings are copied forward after
successfully reconnecting to the same Navidrome server and user; originals remain.

Run one instance per data volume. A volume lock prevents a second writer. Preset edits use versions and reject
stale writes; queues are scoped to each browser/native device. Navidrome URLs may
point to reachable LAN or loopback servers, while link-local/metadata addresses
are rejected. Redirects are not followed for authenticated Navidrome requests.

Recorder status polls while its management view is open; captures continue
independently when the view closes. One audible source handles playback. Browser media events drive playback updates; writes follow user actions.
Docker's health check runs every 30 seconds against `/healthz`. No battery or
idle-power claim has been measured.

## Current limits

- Library playback uses browser-supported formats. Recorder captures public
  MP3/AAC and encodes seekable AAC-LC M4A. No HLS or YouTube/mpv URL resolver. If a library file cannot play natively, configure
  a compatible Navidrome transcoding policy. Radio stations can be offline.
- Radio destinations must be public Internet addresses; private/LAN radio URLs
  are intentionally rejected by the relay. Redirects and DNS results are checked.
- Native library scrobbles are supported with durable dedupe and explicit
  ambiguous outcomes. Recorded radio is not submitted as library scrobbles. Radio titles depend on station-supplied ICY metadata.
- Search shows up to 100 tracks; playlist writes accept up to 1,000 tracks per
  operation. Album browsing loads 40 at a time.
- Navidrome remains authoritative for its users and music permissions. If its
  password changes, update the saved connection; the separate web-app login remains
  valid. Local account administration is limited to changing the current user's
  password.
- The app supports a dedicated hostname/root path, not a reverse-proxy subpath.

## Development and verification

Go 1.26 or newer; SQLite uses pinned modernc.org/sqlite. Recording requires
ffmpeg, ffprobe and fdkaac; Docker includes the tested packages.

```sh
make dev                 # same .env, HTTP on 0.0.0.0:4000
make test                # Go integration tests with race detector
make build               # bin/weazltunes
make mockup              # original static mockup on localhost:4001
```

Stop the Docker service before `make dev` to free port 4000.

Browser tests use an isolated fake Navidrome with separate local web accounts connected to `alice` and `bob`, generated
WAV audio, and temporary storage. They never touch real playlists:

```sh
npm --prefix tests install
# Chromium at /usr/bin/chromium, or set BROWSER_BIN to your browser executable.
tests/run-browser.sh
```

The runner uses temporary test ports 4002 and 4534, then removes its processes and
state. `PLAYWRIGHT_MODULE` can point to an existing Playwright installation.

See [the phase plan](docs/PHASES.md), [verification record](docs/VERIFICATION.md),
and [deployment notes](docs/DEPLOYMENT.md).
API contracts follow OpenSubsonic's
[createPlaylist](https://opensubsonic.netlify.app/docs/endpoints/createplaylist/)
and [updatePlaylist](https://opensubsonic.netlify.app/docs/endpoints/updateplaylist/)
endpoints. Container connectivity follows
[Docker Compose networking](https://docs.docker.com/compose/how-tos/networking/).

## Subweazl backend and Flight Recorder

Implementation and outstanding acceptance are tracked in [SOL_WORKBOOK.md](SOL_WORKBOOK.md).
Its versioned native API is [docs/SUBWEAZL_API_V1.md](docs/SUBWEAZL_API_V1.md).
Contract revision **2026-10-03.2** is implemented. See
[the native handoff](docs/NATIVE_HANDOFF.md) for fixtures and acceptance boundaries.

Flight Recorder sits under Internet Radio. Choose one to six saved favorites;
record now or schedule once, daily, or selected weekdays in an IANA time zone.
Four hours is the default; twelve hours is the maximum. A 22:00–04:00 window
crosses midnight, with actual UTC boundaries and DST corrections returned.
Logout and closing a tab do not stop server capture. Restart resumes the
remaining original window and records unavailable time as gaps.

The output matches profile 1 in `~/ipod_script/ipod.py`: **FDK AAC-LC, 160 kbps,
44.1 kHz, stereo, M4A**. Six stations for six hours use roughly 2.59 GB before
packaging; reservations include twice the nominal rate. Encoding a 128 kbps
source at this profile increases its size. Defaults: 20 GiB installation budget,
10 GiB per account, 1 GiB disk-free reserve, retention off. Configure
`CAPTURE_BUDGET_BYTES`, `CAPTURE_ACCOUNT_BUDGET_BYTES`, `CAPTURE_RESERVE_BYTES`,
and `CAPTURE_RETENTION_DAYS` in Compose's environment. Storage is checked before
reservation and during capture; one failed station does not stop the others.

Listen through one shared session timeline: switch presets at the current
offset, pause globally, or seek all stations together. The highlighted Now Playing
footer shows the selected station and recording, with play/pause and seeking
available even after navigating away from Flight Recorder. The web player buffers
**five minutes ahead**, refilling to five minutes when less than **four minutes
remain**. Buffer ahead offers 1, 3, 5 or 10 minutes and remembers this browser’s
choice. Supported browsers play the AAC segments on one continuous media timeline;
network buffering freezes playback instead of skipping unheard audio. Old
recordings benefit immediately. See [web media buffering](docs/WEB_MEDIA_BUFFER.md)
for browser support and verification. Keep multiple saved
recordings; select their checkboxes and use **Delete selected** to remove only
the chosen server copies. Server capture status
and phone offline readiness are separate. Server deletion/retention never
silently removes phone copies. Native download leases support HEAD/Range,
renewal, immutable checksums and verified atomic download promotion.

Run isolated acceptance with `scripts/run-recorder-fixture.sh 60`. Six-hour
soak uses `scripts/recorder-client.py --duration 21600`; see the handoff for
a fixture container. Overnight validation and AVPlayer/background transfer
acceptance remain false until their respective evidence is reviewed.
