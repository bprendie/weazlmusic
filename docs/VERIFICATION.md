# Verification — native API and Flight Recorder, 2026-10-03

Contract revision **2026-10-03.2**. Implementation release: `55c005e1dad800a0cebad7ce32b12874c1cc7dab`.
Backend/fixtures commit `f1ee390`; web UI commit `981cb2b`. Tests use isolated synthetic
Alice/Bob accounts; production library mutations are excluded.

## Release checks

- `go test -race ./...` and `go vet ./...`: passed, including independent native
  device login/refresh/restart/lost-response replay/revocation, encrypted SQLite,
  normalized library paging, typed favorites, duplicate playlist replacement,
  scrobble dedupe, original-file Range, presets and per-device queue isolation.
- `tests/run-browser.sh`: passed existing library/playlist/radio/Mood/settings,
  real audio, persistence, two-user and mobile flows, without JavaScript errors.
- `node tests/recorder.cjs` against the isolated fixture container: passed
  22:00–04:00 schedule/edit/cancel, six-stream capture after initiating tab close,
  actual AAC audio readyState/currentTime advancement, shared switch/pause/seek,
  correct server-recording deletion, mobile layout. Screenshot:
  `test-results/recorder-browser-mobile.png` (ignored local artifact).
- AAC input and VBR MP3 are converted to the exact FDK AAC-LC160k/44100/stereo
  profile; all generated segments pass ffprobe and FFmpeg local decode/seek.
  Invalid/incomplete MP4 is rejected. Manifest/asset HEAD/Range/416/ETag, lease
  expiry/renewal/revocation, reserve exhaustion and deletion tombstones pass.
- Injected-clock, overnight, DST fold/gap, capacity, cancellation, ownership,
  missed-window/no-duplicate firing and explicit retention checks pass.
  Native refresh works with the fixture upstream shut down.
- Production/fixture image builds pass with pinned base digests and media packages.
  Go files remain under300 lines. Tool versions/licenses/short-run impact are
  recorded in MEDIA_TOOLCHAIN.md. Existing encrypted files remain readable;
  preset migration retains stable IDs and prevents stale browser overwrites.

## Flight Recorder web controls follow-up

The Now Playing footer now shows the recorder station/session and highlights the
active recorder source. Footer and recorder play/pause controls share the same
clock; footer seek controls the session and remains usable after navigation.
Mood/favorites and library transport buttons are disabled for recorder playback.
Stopping or deleting the playing recording clears its playback selection.
Saved recordings have individual/select-all checkboxes and a selected count;
bulk deletion preserves unselected copies, uses each entity's ownership/version
checks, and reports per-recording failures without dropping failed selections.

`tests/run-browser.sh`: existing music/radio/Mood browser checks passed.
`node tests/recorder.cjs` against a fresh isolated fixture volume: passed actual AAC
playback, station metadata/highlight, footer/local play/pause synchronization,
footer seek/stop after navigating away, three retained recordings, checkbox
selection across status refreshes, cancel then delete-two preserving the third,
select-all/unselect-all and mobile layout. No JavaScript errors. Screenshot:
`test-results/recorder-browser-mobile.png`. Recorder backend/API/encoding is
unchanged; Luna's six-hour fixture container continues uninterrupted on its
original release image. This UI follow-up does not establish overnight acceptance.

## Real synthetic capture evidence

`scripts/run-recorder-fixture.sh 60 test-results/recorder-aac160` passed with six
simultaneous inputs. Final state partial, 6,769,131 bytes,69 immutable assets,
60.36 seconds wall time. Four uninterrupted streams each covered98.703% of the
logical window; disconnect stream97.073%, stall stream70.317%. Every downloaded
asset matched its size/SHA-256; corrupt partial download was rejected and replaced;
local seek decoded. Initial/disconnect/stall gaps partition the original timeline.
A representative M4A, complete sample manifest and actual partial manifest are
checked into fixtures/v1. Apple validation is not implied by this Linux run.

`python3 scripts/recorder-client.py --base http://127.0.0.1:4005 --duration 180
--output test-results/recorder-restart-final --container weazl-workbook-restart
--kill-after 45` passed after a real SIGKILL/container restart. The original
180-second end remained fixed. Start tolerance was268ms; final partial recording
contained18,749,808 bytes/189 verified assets. Healthy coverage88.907% reflects
intentional downtime plus discarded unfinished staging; the forced-restart gate
allows at most30 seconds unavailable, separately from the90% uninterrupted gate.
Short-run CPU samples17.76–78.41% of one core, last active memory222.8MiB.
This is recovery evidence, not a six-hour performance bound.

`scripts/verify-volume-recovery.py` stopped the fixture, archived its entire
volume/key, restored into a new named volume, and verified identical installation,
owned sessions, exact encrypted refresh-replay response and media checksum.
Local evidence is under test-results/volume-recovery. Production rollback uses
its own restricted backup; this fixture test does not modify production data.

## Production deployment receipt

Deployed via `bobp@jumpbox.prendie.io` → `bobp@weazlmusic.teralab.local`, checkout
`/home/bobp/weazlmusic`, source `55c005e1dad800a0cebad7ce32b12874c1cc7dab`.
The clean main branch fast-forwarded; the new image built while the prior service
remained live. Service stopped only for the full-volume archive and replacement.

- Container `weazlmusic-weazltunes-1`: healthy, zero restarts, user weazl/UID10001,
  port4000, existing `weazlmusic_weazltunes-data` volume retained.
- `/healthz`200; `/`200; legacy `/api/me`401; native `/api/v1/me`401.
  Public info returns revision2026-10-03.2 and installation
  `a185535e72bc82b7eef2dca766a4161ea5cc45cb8cd195ac6aeeefd6e424c3ad`.
- ffmpeg, ffprobe and fdkaac present. Encryption-key SHA-256 identical before/after.
  No production login/password, library mutation, scrobble or recording was used
  as an acceptance test.
- Backup: `/home/bobp/weazlmusic-backups/20261003T151855Z/data.tar.gz`, restricted
  parent directory0700. Archive contents and SHA-256 verified:
  `d1c2e7a964be20bfbc96d0d04b4716ee9e199cb84399ec5e511e3b8ded032396`.
- Rollback image: `weazltunes-web:rollback-20261003T151855Z`; override YAML and old
  source/image receipts are in that same backup directory. Runtime image:
  `sha256:0bafa4f89a63112f71381a71c32804970c403c1fb8040cdd27dd9ecc006712db`.

Documentation-only follow-up commits do not change this tested runtime image;
production checkout is advanced to the documentation receipt without restarting.

### Recorder web controls deployment

The recorder web controls follow-up is deployed from source
`f33caaaa8de2d0cef695ce4288105dcee52190bb`, runtime image
`sha256:cbd92a82d2cccfb9158fc6da28704a55dee53c4814bb554c5dd6a1a89bb5bed7`.
The container is healthy with zero restarts. HTTP health/root and anonymous
authentication boundaries passed; the served JavaScript contains the recorder
highlight and recording-selection controls. The installation ID, API revision
and encryption-key digest remain unchanged. No production recordings or library
data were created or deleted for these checks.

The stopped-volume backup is
`/home/bobp/weazlmusic-backups/20261003T154900Z/data.tar.gz`, mode0600 owned by
the deployment operator, inside a mode0700 directory. Verified archive SHA-256:
`1b42d6f6b0683aeb881902eec18cdd6d073e5de822a8159aa6890a6ac8b99e6e`.
Rollback image `weazltunes-web:rollback-20261003T154900Z` retains the prior running
image; the same directory contains its override and source/key receipts. An
earlier attempt failed at archive checksum verification because the archive
helper created a root-owned file; automatic rollback restored the prior service.
Assigning the archive to the operator before checksum verification resolved the
issue, and DEPLOYMENT.md now includes that step.

## Outstanding acceptance

Luna's user-authorized six-stream, six-hour fixture soak started at
**2026-10-03 15:23:36 UTC**, expected capture end21:23:36 UTC (17:23:36 EDT),
followed by download/hash/decode checks. Container `weazl-luna-soak-20261003`,
new isolated volume `weazl-luna-soak-20261003-data`, fixture session
`848f9f0765eb0b5bc4a3435bb65a2175922731bd697da788f46562a3fbed97f2`,
image `sha256:8f9856f28266b1ce2620e94d310249207e96bd374d542ab18db8fadd887e8bc8`.
Artifacts/log are under `test-results/luna-soak-20261003`. Status: **running**,
not yet accepted. The other local test containers were removed after verification. It must record compact resources, timing, bytes, all asset checksums,
local seeking and decoded station/cue identity at shared minute30 and50 offsets.
Use fixture production30-second segments and actual21600-second wall time.
`overnightValidated:false` remains until that report is reviewed. AVPlayer,
background URLSession/proxy-header paths, locked-screen playback and Apple
interruptions remain device-side acceptance; `appleValidated:false` stays explicit.

Physical ENOSPC was not induced on this shared filesystem; reserve/quota rejection
and incomplete media are tested. Timestamp anchors preserve detected restart/
disconnect gaps, but do not reconstruct sub-deadline stalls or synchronize
broadcaster delivery clocks. Capacity scans a one-year recurrence horizon and
runtime dispatch enforces six streams. Directory/provider and entire real codec
collection compatibility are not claimed from fixture tests. No phone offline
readiness is inferred from a completed server capture.

---

The following is the historical browser-release evidence, retained for context.

# Verification — 2026-09-21

## Automated checks

- `go test -race ./...`: passed. Authentication, CSRF/origin enforcement,
  logout revocation, two-user playlist ownership, playlist create/update/delete
  write-through, user settings isolation, encryption at rest, reopening the
  settings store with its persisted key, track-state validation, media Range
  requests, cover art, endpoint allowlisting, and radio URL/PLS/M3U handling.
- `go vet ./...`: passed.
- `tests/run-browser.sh`: passed in Chromium using Playwright 1.63.0. Login,
  square/loaded album covers, actual HTMLAudioElement playback of a generated
  WAV fixture, favorites, queue movement, playlist creation/rename/track removal/
  deletion, search, installed preset order, station persistence on reload,
  logout, second-user isolation, and desktop/mobile layout. No JavaScript errors.
- All Go files remain below the 300-line ceiling. Production uses the Go standard
  library and static browser modules without an npm build or runtime dependency.
- Fresh and existing data directories bootstrap the local-only `weazladmin` /
  `admin` account when that account is absent. The default password can be
  changed through the account dialog; it is never used as a Navidrome credential.
- Admin installation settings are encrypted at rest and protected from
  non-admin sessions. The admin can save the shared Navidrome URL and LLM
  provider configuration without exposing API keys to the browser.
- Normal users authenticate directly against the configured Navidrome server;
  the app stores only the generated API token and salt. New Navidrome users are
  provisioned on first successful login, and playlist writes use their upstream
  owner identity. The login surface has no local registration control.
- Radio acceptance remains green: all installed presets render in order, radio
  playback relays through same-origin routes, ICY metadata updates the player,
  user stations persist, and private/link-local radio targets are rejected.

## Container

`docker compose up --build -d` successfully builds and starts the application.
The service publishes `0.0.0.0:4000`, runs as UID/GID 10001, and uses a named volume
for `/data`, with an init process and a 15-second graceful stop window. Direct HTTP and the health endpoint return 200. The filesystem is
read-only except for the data volume and temporary directory. The application
requires no public-domain setting or bundled reverse proxy.

## Existing live services

Using the Navidrome connection already configured in Subweazl, verified:

- Login and logout through the app, without printing or saving credentials.
- Four recent albums, one album's 13 tracks, and ten existing playlists.
- Real JPEG cover art returned through the authenticated media route.
- Installed preset entries (private station subsequently removed from bundled defaults).
- DEF CON Radio PLS resolution and 8,192 bytes of MP3 audio through the relay.
- Live SomaFM directory (46 results) and Icecast directory (100-result page).

No real Navidrome playlist was created, edited, or deleted during verification.
Playlist mutations were tested against an isolated Navidrome fixture, with
separate Alice and Bob accounts. User acceptance should include creating a small
playlist in the web app and seeing it in that same user's Navidrome interface.

## Remaining acceptance boundaries

The user's reverse-proxy configuration is outside this app and has not been
changed or tested. Browser playback of the user's entire codec collection,
all external stations, multi-browser compatibility, concurrency/load,
and long-run power use are not claimed as verified. The first release does not
include HLS, bundled transcoding, or cross-device
queue coordination. See the README for deployment and operational details.

## Revised account flow and scrolling

After the first smoke test, local web-app accounts replace direct Navidrome login.
New integration checks prove that local registration/login do not contact
Navidrome; passwords differ between local and upstream identities; saved
connections survive an application restart; failed connection tests preserve the
previous valid connection; and different local users can choose different servers.
Playlist writes use the configured upstream owner, not the local username.
Previous settings migrate only after the matching upstream credentials verify.

Browser checks now register local accounts, configure Navidrome through the UI,
then exercise the existing playback and playlist workflow. They also verify
horizontal recent-album scrolling and sidebar scrolling on a short viewport.
The redundant sidebar playlist list has since been removed. The first-release
live-service checks above describe the original test run, not automatic server
configuration in the revised app. Deployment no longer accepts NAVIDROME_URL.

## Radio, Mood, and random playback revision

Go tests cover ICY stripping without audio corruption, metadata/session isolation,
stream cleanup, both Ollama NDJSON and vLLM SSE, fragmented IDs, duplicate/invented
ID rejection, 20-track enforcement, same-owned-playlist reuse, immediate Navidrome
write-through, settings/key isolation, concurrent build rejection and cancellation.
Browser tests cover curator settings/model discovery, incremental Mood rendering,
unchanged current playback, random play with 30 queued tracks, the Weazl favicon,
radio title/artist rendering and metadata connection cleanup when paused.
A bounded live read of the installed 80s/90s station returned ICY metadata for
Michael Jackson — Billie Jean. LLM generation is tested with isolated streaming
fixtures; the user's actual model endpoint remains a smoke-test boundary.
