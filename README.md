# WeazlTunes Web: The Sovereign Acoustic Deck

**Release 1.0** · Version `1.0.0` · Git tag `v1.0.0`

**WeazlTunes is a frontend for Navidrome**, with Internet radio and a native Subweazl backend, stamped with `weazlhead` branding. Navidrome manages your music library; WeazlTunes provides the listening interface. One Go binary serves the UI, the authenticated API, and the audio pipeline.

**WeazlTunes and Navidrome can run on the same host**, or on separate machines. Connect WeazlTunes to your existing Navidrome installation, or install Navidrome alongside it.

No frontend framework bloat. No Node runtime. No cloud identity brokers. Zero telemetry. Just pure, unadulterated acoustic grindage on your local bare metal.

## Ignition

Boot the rig using Docker. This Compose file starts WeazlTunes only; run Navidrome separately on the same host or another reachable machine, then configure its URL under **Account → Installation settings**. No Navidrome URL is preconfigured.

```sh
docker compose up --build -d
```

Hit **http://localhost:4000**, or your machine's IP on port 4000. Compose publishes to `0.0.0.0:4000`.

### Sysop Bootstrap & Access

Fresh data volumes spin up with a local-only administrator account. This local vault is isolated; the password is for WeazlMusic only and is never sent to your Navidrome instance.

1. Sign in as `weazladmin` / `admin`. **Change this password immediately** under **Account → Change password**.
2. Navigate to **Account → Installation settings** and lock in your shared Navidrome backend URL.
3. Sign out, then drop back in using your actual Navidrome credentials.

Normal users authenticate against the configured Navidrome server. WeazlTunes stores only the Navidrome API token and salt—never the plaintext password. The local `weazladmin` login acts as your emergency hatch if Navidrome goes dark. New users are provisioned in the app automatically upon their first successful Navidrome login.

### Network Ops

Your reverse proxy handles domain routing and TLS. WeazlTunes serves plain HTTP. Just forward to port 4000 and preserve the request `Host` header. Stream routes explicitly send `X-Accel-Buffering: no` for radio and Mood; ensure your proxy streams responses instead of choking on buffer limits.

| Variable | Default | Payload |
| --- | --- | --- |
| `LISTEN_ADDR` | `0.0.0.0:4000` | HTTP listener bind. |
| `DATA_DIR` | `./data` (`/data` in Docker) | Encrypted user settings, SQLite vault, recording assets, and AES-GCM key. |
| `COOKIE_SECURE` | `false` | Set to `true` to restrict session cookies to HTTPS entry points. |

*Same-host setup: when both services run directly on the host, WeazlTunes can reach Navidrome through its loopback URL and listening port. When they run in separate containers, put them on a shared Docker network and use Navidrome’s service name and container port. For a container connecting to a host service, use a host address reachable from that container. `localhost` inside a container means the container itself. The same networking rules apply to your LLM endpoint.*

## The Deck: What Works

* **Sovereign Sessions:** Navidrome sign-in, independent local admin, persistent revocable native devices, and AES-GCM encrypted upstream connection settings.
* **Library Interface:** Horizontal 16-album recent shelf, paginated browsing, scrolling sidebar, live search, real cover art, and Navidrome-synced favorites.
* **Playback Control:** Browser playback, seek, volume, shuffle, queue restoration, and mobile queue access.
* **Playlist Mutability:** Create, append, remove, or nuke playlists. Operations write directly to the configured Navidrome user's account. Read-only for other users' playlists based on Navidrome permissions.
* **Radio Integration:** Seven public Weazltunes presets. On-demand SomaFM and Icecast directories, PLS/M3U resolution, and a same-origin radio relay. Switching to radio keeps your library queue intact. Live ICY titles scrape artist/track data when stations provide it.

**Deck Hotkeys:** `/` (search), `1` (home), `2` (albums), `3` (favorites), `4` (playlists), `5` (radio), `6` (queue), `M` (build Mood), `Space` (play/pause), `N`/`P` (next/previous), `?` (help).

## DJ-Weazl & Mood Generation

Drop the needle: **Pick up the needle** immediately fires a random library track and overwrites the queue with 30 random cuts (or as many as a smaller library supplies).

For algorithmic curation, navigate to **Account → LLM curator**. Point it to your reachable Ollama or vLLM endpoint, load the models, pick your brain of choice, and save. API keys are vaulted with account settings and never returned to the browser.

Play a library track, then hit **M** (or the **✧** button). Mood uses that track as the seed, firing up the local LLM to build or replace your **Mood** playlist in Navidrome. It hunts for 20 tracks, including the seed, from up to 240 library candidates, enforcing unique, valid IDs. Tracks write to Navidrome and queue up seamlessly as the model streams its output. Nuke the generation at any time by hitting **Stop Mood**, closing the page, or signing out. Tracks already accepted remain saved. No LLM cycles are burned while idle.

## Cryptography & Storage

The named `weazltunes-data` Docker volume holds the AES-GCM key and encrypted per-user configurations. **Back up the entire volume, including the key.**

Local passwords are salted and hashed via PBKDF2-HMAC-SHA256 (600,000 rounds). Navidrome passwords are exchanged for API auth tokens and are not persisted. Tokens and connection settings are encrypted at rest.

Restarting the container invalidates browser cookies, but native refresh sessions, schedules, and recording jobs persist. Swapping Navidrome connections rotates the current session and revokes native devices for that user to prevent ID collisions. Volume locks prevent dual-writer database corruption.

## Flight Recorder (The Tape Deck)

Subweazl backend implementation tracking: [SOL_WORKBOOK.md](SOL_WORKBOOK.md). Native API V1 contract (rev `2026-10-03.2`): [docs/SUBWEAZL_API_V1.md](docs/SUBWEAZL_API_V1.md).

Flight Recorder lives under Internet Radio. Lock in up to six saved favorites and schedule rips (once, daily, or specific weekdays) within local IANA time zones. Default capture is 4 hours; max is 12. Sessions crossing midnight handle UTC and DST bounds automatically. Logging out or closing the UI does not kill the server capture. Restart resumes the remaining scheduled window and records unavailable time as gaps.

Audio is ripped matching the `ipod.py` profile at a reduced bitrate: **FDK AAC-LC, 128 kbps, 44.1 kHz, stereo, M4A**. Six stations over six hours eats roughly 2.07 GB before packaging.

* **Storage Limits:** Configure `CAPTURE_BUDGET_BYTES`, `CAPTURE_ACCOUNT_BUDGET_BYTES`, `CAPTURE_RESERVE_BYTES`, and `CAPTURE_RETENTION_DAYS` in your Compose `.env`. Defaults: 20 GiB installation budget, 10 GiB per account, 1 GiB disk-free reserve, retention off.
* **Chunking:** Capture holds 30-second recovery checkpoints. Finalization wraps contiguous audio into **~10-minute M4A files** without destructive re-encoding. Gaps and tails yield shorter files; existing finalized recordings stay unchanged. See [recording chunks](docs/RECORDER_CHUNKS.md).

The web player handles multi-station playback on a single timeline, syncing pauses and seeks across all recorded streams. Browsers target 5 minutes ahead, refilling when less than 4 minutes remain. Downloads use whole files, so ten-minute chunks can exceed that target; the selector offers 1, 3, 5 or 10 minutes. Cached seeks reuse downloaded audio. See [web buffering](docs/WEB_MEDIA_BUFFER.md). Download leases support HEAD/Range requests, immutable checksums, and verified atomic promotion.

The Now Playing footer lights up for recorded playback, with play/pause and seeking available across views. Keep multiple recordings and use their checkboxes with **Delete selected** to remove chosen server copies. Server deletion never silently removes phone copies. Native-device acceptance and verification boundaries are tracked in the [native handoff](docs/NATIVE_HANDOFF.md).

## System Boundaries & Limits

* Library playback relies on native browser codec support. Radio rips to seekable AAC-LC M4A. No HLS or YouTube URL resolving. Use Navidrome transcoding policies for unsupported local formats.
* Radio origins default to public internet. For LAN/split-DNS stations, set `RADIO_PRIVATE_ORIGINS=https://radio.yourdomain.com` in your Compose `.env` and recreate the service. Use comma-separated exact origins without paths or wildcards. Redirects and playlist targets are checked independently; metadata, link-local and loopback addresses remain blocked. See [radio troubleshooting](docs/RADIO_PRIVATE_ORIGINS.md).
* Native library scrobbles are supported with durable deduplication. Recorded radio does *not* submit library scrobbles.
* Navidrome retains ultimate authority over music permissions and normal-user authentication. The independent local administrator controls shared installation settings and its own password.
* Dedicated hostname or root path is required. No reverse-proxy subpathing.

## Dev Ops & Verification

Built for Go 1.26+. SQLite utilizes `modernc.org/sqlite`. The Docker image bundles required `ffmpeg`, `ffprobe`, and `fdkaac` binaries.

```sh
make dev                 # Local HTTP on 0.0.0.0:4000 (stop Docker first)
make test                # Go integration tests with race detector
make build               # Compiles bin/weazltunes
make mockup              # Legacy static mockup on localhost:4001
```

Browser acceptance tests use Playwright against an isolated, simulated Navidrome instance (`alice` and `bob` test accounts) with generated WAV audio. They never touch your production library.

```sh
npm --prefix tests install
tests/run-browser.sh     # Override Chromium path with BROWSER_BIN if needed
```

See the [phase plan](docs/PHASES.md), [verification record](docs/VERIFICATION.md), and [deployment notes](docs/DEPLOYMENT.md) for operational details and acceptance evidence.
