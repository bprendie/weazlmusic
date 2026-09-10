# WeazlTunes web

A lightweight browser listening desk for Navidrome and Internet radio, with
`weazlhead` branding. One Go binary serves the UI, authenticated API, and audio.
No frontend framework, Node runtime, cloud identity, or telemetry.

## Run with Docker

```sh
docker compose up --build -d
```

Open **http://localhost:4000**, or your machine's IP on port 4000. Compose publishes
**0.0.0.0:4000**. No Navidrome server is configured in Docker or environment variables.

1. Choose **Create a web-app account**, with its own username and password.
2. Enter your **Navidrome server URL** in the connection screen.
3. Enter that server's **Navidrome username and password**, then **Test & save**.

Afterward, sign in with your web-app credentials. The connection is saved for
that account and can be changed under **Account → Configure Navidrome**. Different
web-app users can connect to different servers or different Navidrome users.
Local login works even if the connected Navidrome server is offline. Registration
is available on the login screen; usernames are case-insensitive and local
passwords require at least 10 characters.

Your reverse proxy handles the domain and TLS. The app serves plain HTTP and
requires no public URL setting. Forward to port 4000 and preserve the request
Host header. For an exclusively HTTPS entry point, set `COOKIE_SECURE=true`;
leave it false for direct HTTP access. Stream routes send `X-Accel-Buffering: no`
for radio and Mood; proxies should stream responses rather than buffer them.

| Setting | Default | Purpose |
| --- | --- | --- |
| `LISTEN_ADDR` | `0.0.0.0:4000` | HTTP listener |
| `DATA_DIR` | `./data` (`/data` in Docker) | Encrypted user settings and encryption key |
| `COOKIE_SECURE` | `false` | Restrict session cookies to HTTPS when enabled |

`localhost` inside a container means that container. Use a reachable server URL
or a service name on a shared Docker network for Navidrome and your LLM endpoint.

## What works

- Local web-app accounts, 24-hour sessions, sign-out, and encrypted per-user
  Navidrome connection settings.
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

Restarting the application invalidates sessions, but local accounts and saved
connections survive. Logging out cancels the session's streams. Changing a
Navidrome connection rotates the current session and revokes other sessions for
that web-app user. Saved queues/settings are isolated by both local account and
upstream identity, so changing servers does not replay IDs from the previous
library. Previous first-release queue/radio settings are copied forward after
successfully reconnecting to the same Navidrome server and user; originals remain.

Run one instance per data volume. Multiple tabs use last-save-wins queue/radio
settings; live cross-device coordination is not implemented. Navidrome URLs may
point to reachable LAN or loopback servers, while link-local/metadata addresses
are rejected. Redirects are not followed for authenticated Navidrome requests.

No directory polling, progress animation timer, or second audio decoder runs in
the app. Browser media events drive playback updates; writes follow user actions.
Docker's health check runs every 30 seconds against `/healthz`. No battery or
idle-power claim has been measured.

## Current limits

- Browser-supported audio formats only. No bundled transcoder, HLS radio, or
  YouTube/mpv URL resolver. If a library file cannot play natively, configure
  a compatible Navidrome transcoding policy. Radio stations can be offline.
- Radio destinations must be public Internet addresses; private/LAN radio URLs
  are intentionally rejected by the relay. Redirects and DNS results are checked.
- No play-history/scrobble reporting yet. Radio titles depend on station-supplied ICY metadata.
- Search shows up to 100 tracks; playlist writes accept up to 1,000 tracks per
  operation. Album browsing loads 40 at a time.
- Navidrome remains authoritative for its users and music permissions. If its
  password changes, update the saved connection; the separate web-app login remains
  valid. Local password reset/account administration is not implemented yet.
- The app supports a dedicated hostname/root path, not a reverse-proxy subpath.

## Development and verification

Go 1.26 or newer; production has no external Go modules.

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

See [the phase plan](docs/PHASES.md) and [verification record](docs/VERIFICATION.md).
API contracts follow OpenSubsonic's
[createPlaylist](https://opensubsonic.netlify.app/docs/endpoints/createplaylist/)
and [updatePlaylist](https://opensubsonic.netlify.app/docs/endpoints/updateplaylist/)
endpoints. Container connectivity follows
[Docker Compose networking](https://docs.docker.com/compose/how-tos/networking/).
