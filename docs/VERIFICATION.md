# Verification — native API and Flight Recorder, 2026-10-03

Encoding follow-up, 2026-10-08: new captures use 128 kbps AAC-LC.
[FLIGHT_SYNC_PERFORMANCE.md](FLIGHT_SYNC_PERFORMANCE.md) records the actual encode
test and native sync findings; earlier 160 kbps evidence below is historical.

Web buffering follow-up, 2026-10-06: see [WEB_MEDIA_BUFFER.md](WEB_MEDIA_BUFFER.md)
for the five-minute/four-minute refill policy, continuous AAC browser playback,
Freestyle diagnosis, dependency provenance and regression evidence.

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

Luna's user-authorized six-stream, six-hour local fixture soak completed on 2026-10-03. The scheduled 21,600-second session finalized `partial` as expected for the deliberate station 5 reconnect and station 6 stall fixtures. Actual start tolerance was201ms; 4,322 assets / 2,578,402,060 bytes passed size and SHA-256 verification, local seek decoded, and all twelve station/cue checks at shared minute30 and50 offsets passed. Coverage ranged99.9196–99.9984%; sampled memory was179.6–286.0MiB, sampled Docker CPU16.3–113.46%, and the container had zero restarts. Container `weazl-luna-soak-20261003` was removed after completion; volume `weazl-luna-soak-20261003-data` and ignored artifacts under `test-results/luna-soak-20261003/` are preserved. Full per-station coverage, resource trends, tone readings, image/source IDs and limitations: [SOAK_2026-10-03.md](SOAK_2026-10-03.md). This evidence awaits root review: product `overnightValidated:false` remains unchanged, and `appleValidated:false` stays explicit. AVPlayer, background URLSession/proxy-header paths, locked-screen playback and Apple interruptions remain device-side acceptance.
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

## Ten-minute recorder finalization (2026-10-08)

Go race tests and vet, real AAC packet/seek checks, transactional rollback and cleanup checks, ten-minute browser playback, buffer/gap tests, recorder UI acceptance and the general browser suite passed. See [evidence and limits](RECORDER_CHUNKS.md).

## Native radio probe relay — 2026-10-10

Release `v1.0.1` fixes the radio playback slot being claimed by Apple’s `Range: bytes=0-1` format probe. Race-tested direct relay and native lease route checks returned 200 for overlapping probe/audio, 409 for duplicate actual audio, and 200 when audio reopened after disconnect with a retained feed. Invalid playback ID and other Range values stay rejected. Full `go test -race ./...` and `go vet ./...` passed. See [server relay workbook](../SERVER_RADIO_RELAY_WORKBOOK.md) for deployment image, backup receipt and remaining phone acceptance. Production service is healthy, but a production authenticated overlap test and audible physical-iPhone confirmation are still pending.

## WeazlMusic mascot refresh — 2026-10-12

The provided `AppIcon.png` became `web/appicon.png`, replacing the old web logo in
the home hero, favicon, sidebar, login/account views and fallback playback art.
The hero layout was visually checked in Chromium at 1440 px desktop and 390 px
mobile widths. `tests/run-browser.sh` passed after the asset references changed;
`go build ./cmd/weazltunes` and `go test ./web` passed, and the final CSS preview
kept title/buttons clear of the mascot.

Code commit `30b7a6a` deployed to `weazlmusic.teralab.local` through the trusted
jumpbox route. Production image:
`sha256:3b6ade89a3051d35107ce23127433475c1e3b1b18d198eb242fd7ee008b74669`.
Container healthy with zero restarts; health/root checks passed, anonymous native
identity stayed 401, and installation info/key digest matched before and after.
The served `/appicon.png` SHA-256
`ac3ba33b587a0a56a017042d06d803e9dba7071777f638e89db43c2dd9d5dec8`
matched the checked-in production asset. No capture was active at either
preflight check.

Production rollback receipts (old source/image, override, build log and health):
`/home/bobp/weazlmusic-backups/20261012T140544Z`, with the previous running
image tagged `weazltunes-web:rollback-20261012T140544Z`. To preserve free space
on production, the verified full-volume archive is stored on this workstation at
`/home/bobp/weazlmusic-backups/20261012T140544Z/data.tar.gz` (0700 directory,
0600 archive). Its SHA-256 is
`fd46c1c6655a956bc9eb245e636f766e1a7f38b7bd63821b61c04e0edc820d80`.
The production receipt points to this external archive. It contains the data
volume and encryption key; restore requires transferring it back through the
trusted route before following the restore procedure.

## Weazlpod server extension — 2026-10-20

Baseline `250181bf92c71846e57951b1e3eab370bb1f1c86`. All new media acceptance uses
synthetic WAV/FLAC/AAC/MP3, fake Navidrome, isolated radio origins and temporary
storage. No production accounts, listening-history/playlist mutations, push or
production deployment. Existing contract revision stays **2026-10-03.2**; optional
Weazlpod extension revision is **2026-10-20.1**. Both exact-revision iOS checks
were inspected, along with the Codable capability/lease DTOs (unknown added fields
are ignored); server/fixture discovery stays compatible without iOS edits.

Commands and evidence:

- `make test` (`go test -race ./...`): passed all Go packages, including existing
  recorder/native/browser-server regressions and new Weazlpod tests.
- `go vet ./...`, `git diff --check`: passed.
- `tests/run-browser.sh`: passed library audio, random queue, curator/Mood,
  favorites/playlists/search, presets, ICY lifecycle, persistence, logout,
  second-user and mobile checks. No production fixture traffic.
- `docker build --target production -t weazltunes:weazlpod-review .`: passed
  with unchanged pinned Alpine FFmpeg8.0.1-r1 and installed libmp3lame/lame-libs
  3.100-r5. No package/source change to the encoder image dependencies.
- `scripts/verify-weazlpod.sh weazltunes:weazlpod-review`: passed the focused
  API/encoder/radio/recovery/contract fixture tests against actual packaged
  ffmpeg/ffprobe, nonroot/read-only/no-network, 8 MiB /tmp and isolated mounted
  data. The initial attempt to put all test data on a 128 MiB tmpfs correctly
  received 507 admission errors; the reusable verifier now places fixture data
  on disk, mirroring production staging. No production volume is mounted.
- `scripts/generate-weazlpod-fixtures.sh weazltunes:weazlpod-review`: generated
  only the synthetic MP3/ready-response/representation/capability artifacts using
  production code's encoder settings, probe and response builder.
- `(cd fixtures/v1/media && sha256sum -c SHA256SUMS)`: both samples pass. MP3 is
  81,083 bytes, SHA-256
  `21491ed489b84cd40167a8125b119651fdb01f0e81ffe26459ccd7958ccc6a57`,
  221,184 presentation samples, 5,016 rounded ms; raw LAME encoder delay/padding
  576/576 and FFmpeg reference skip/discard 1105/47 (529 decoder-delay samples).

Focused acceptance covers the header/default/conflict matrix; unchanged original
WAV downloads despite upstream default AAC streaming; header-free MP3 GET/HEAD;
stream/download byte identity and renewed leases; 206/416/If-Range 200 fallback;
lease expiry/revocation; foreign preparation/recording rejection; changed bytes
under unchanged source metadata; concurrent in-flight deduplication; responsive
unrelated API while two workers block; admission backoff, quota exhaustion and
active-reader protection; cancellation, corrupt/truncated input, abandoned
staging/orphan cleanup and restart/lease persistence. Encoder matrix includes
WAV, FLAC, AAC/M4A, CBR/VBR MP3, mono and differing input sample rates. Tests
inspect every encoded MPEG frame for CBR, fully decode source/output to compare
presentation sample counts, and seek near beginning/middle/end.

Live tests use both MP3 and AAC origins with ICY interleaving. They verify actual
MP3 CBR128k/44100/stereo output, metadata separation, no static file claims,
duplicate ownership rejection, per-account worker admission, disconnect/reopen,
Range rejection, and active-device revocation ending the encoder promptly.
Recorded derivatives preserve AAC master hashes and manifest station starts/gaps;
known-tone start/middle/end seeks across two station timelines verify presentation
mapping. A deleted master makes its cached derivative inaccessible.

Representative uncached preparation of a synthetic 60-second mono8000Hz WAV was
**575.6 ms** in the host race-test run (includes source fetch/hash, two full
decodes, encode/probe, publication and polling). This is a synthetic short-run
measurement, not a large-library or ESP32 latency bound. A packaged non-race
example was **659.6 ms**. Packaged verification
logs include their corresponding preparation time. Source/encoder digests and
actual ready DTO are checked in; detailed local logs are retained under ignored
`test-results/weazlpod/`. Reproducible commands and firmware request/response,
resume, trim and preroll guidance: [WEAZLPOD_FIRMWARE.md](WEAZLPOD_FIRMWARE.md).

Still pending: physical ESP32 decode/playback, decoder-specific trimming and
reservoir preroll, uninterrupted segment transitions, SD throughput and power;
sustained maximum-size/load and long live-transcode soak; actual ENOSPC and
abrupt SIGKILL during publication (reserve rejection, encoder cancellation,
ordinary restart and simulated abandoned staging are tested). No Apple device or
hardware acceptance is inferred. Preparations are deliberately ephemeral across
restart; published leases/representations are durable. Live reconnect is
client-driven. No production deployment was performed.

### Weazlpod production deployment receipt — 2026-10-20

After the isolated implementation checks above, the user explicitly authorized
push and deployment. Source `e2af997d1d1f86ac4be821980e5db7bb6a52a367` was pushed
to origin/main and deployed using direct, trusted SSH to
`bobp@weazlmusic.teralab.local`, checkout `/home/bobp/weazlmusic`. The production
checkout fast-forwarded from `250181b`. The previous runtime remained live during
the build. Read-only encrypted-record checks found zero active captures and no
session due within five minutes, both before build and immediately before stop.

The full existing volume/key was archived with the service stopped. Tar integrity
and SHA-256 verification passed before replacement startup. Retained backup and
receipts on production: `/home/bobp/weazlmusic-backups/20261020T194836Z`
(directory0700, archive0600). Archive SHA-256:
`7bb1bc8ff812b1ffd64f50c2d61dc931ca10242dcee7274bd860abe70d2af923`.
Rollback image: `weazltunes-web:rollback-20261020T194836Z`. Its compose override,
old source/image, build log and before/after checks are in the receipt directory.

Deployed runtime:
`sha256:c3c4a90a7c6e06cd0745ac4818f9ab0f26cfcb67a44137e6184cfcd8050c89c5`.
Container healthy, zero restarts, user weazl, original named volume retained.
Local and LAN health endpoints pass; root 200, anonymous browser/native identity
401. Installation `a185535e72bc82b7eef2dca766a4161ea5cc45cb8cd195ac6aeeefd6e424c3ad`,
contract revision2026-10-03.2, encryption-key digest, 12 devices, 5,995 master
assets and six manifests remained unchanged. A synthetic lavfi source encoded
inside the actual production image probed MP3/44100/2/128000 and fully decoded.
No production login, music mutation or real-account transcode served as a fixture.
Physical ESP32 and long-run acceptance boundaries remain open. The later receipt
commit updates documentation only; it does not require another runtime restart.

## Parallel playlist preparation with two application cores reserved

Follow-up source baseline `8e6fb62`. TRANSCODE_WORKERS now defaults to automatic
(0), with positive settings acting as ceilings. Effective admission is bounded
by remaining physical-core groups and the existing cache reservation budget.
Every production FFmpeg/ffprobe/fdkaac subprocess uses the same restricted CPU
pool; the API process keeps its original affinity. Two whole core groups are
excluded, SMT siblings are handled, visible cgroup v1/v2 ancestor quotas are
floored, and unsafe/missing CPU headroom disables encoding rather than allowing
unrestricted execution. MP3 encoder settings, fixture bytes/checksum and cached
representation IDs are unchanged; optional capability fields describe capacity.

Verification:

- `make test`: all packages pass with the race detector (115.073 s for the server
  package), including four simultaneous playlist preparations and excess-work
  backoff, parent/child affinity checks, cancellation, SMT/noncontiguous CPU sets,
  explicit worker ceiling clamping and cgroup ancestor limits.
- `go vet ./...`, `git diff --check`: pass.
- `tests/run-browser.sh`: existing isolated browser playback/library/radio/Mood,
  persistence and mobile acceptance passes.
- `docker build --target production -t weazltunes:weazlpod-cpu-review .`: passes.
  No Alpine package or FFmpeg version change. x/sys v0.36.0 was already pinned
  transitively; it is now a direct dependency for Linux affinity syscalls.
- `scripts/verify-weazlpod.sh weazltunes:weazlpod-cpu-review`: actual packaged
  media/API/fixture tests pass as nonroot with a read-only root/no network. Its
  extra four-core container verifies restricted worker affinity. Two-core and
  2.9-CPU-quota containers verify encoding is unavailable even with a configured
  worker ceiling of 999. Existing original/file/API operations stay available.
- The production binary entrypoint was exercised in the isolated image using
  `--weazl-media-exec ffmpeg -hide_banner -h encoder=libmp3lame`; it execs the
  actual tool and reports no internal MP3 encoder threading support.
- `go test ./internal/server -run '^$' -bench BenchmarkPodPlaylist -benchtime=1x
  -count=1`: four distinct five-minute 48 kHz stereo FLAC fixtures, cold derivative
  stores and full source/output validation. One worker: **9.015 s**; two workers:
  **5.907 s**, about **1.53× throughput** in this local example. Another loaded
  development run measured 18.104/9.497 s. These are fixture measurements, not a
  promise for every codec, disk, hardware or network.
- Synthetic MP3 SHA256SUMS and encoder/profile/representation receipts remain
  byte-identical. Capability fixtures describe a deterministic four-core example;
  live capacities depend on the actual host, quota and budget.

The production VM changed from four to 24 vCPU cores during this work, with
approximately 3.4 GiB RAM. The new policy leaves 22 cores in the shared media
pool, while the unchanged 4 GiB budget admits five worst-case reservations. The
client should pipeline up to advertised maxWorkers track preparations while
transferring completed files; serialized clients remain serialized. No original
library, recording master, playlist or listening-history mutation was used for
acceptance. Media integration tests need sufficient CPU headroom (four usable
core groups for two-worker checks); restricted-resource behavior is tested
separately. Detailed local logs are under ignored `test-results/weazlpod-cpu/`.

### CPU-reserve production receipt — 2026-10-20

Deployed source `50e178e3f475c442947743c062a8bbd8ccd28375` directly to
`bobp@weazlmusic.teralab.local`. Automatic worker sizing is enabled in the
production compose configuration (`TRANSCODE_WORKERS=0`); no cache budget changed.
Runtime image:
`sha256:e8a348249e11416e8f0f9a6791d0d7fb78feb69dd1ed789bc0be8d58b96749e0`.
Container healthy, zero restarts, original named volume and nonroot user retained.
No active capture or session due within five minutes was found at either
pre-build or immediate pre-stop check. Existing service stayed live during build.

Verified stopped-volume backup/receipt directory:
`/home/bobp/weazlmusic-backups/20261020T205121Z` on production (0700 directory,
0600 archive). SHA-256:
`1112beee06d44677196b7686fa2e4b9b30df51e69f42c30f4a1e7a201f5254f8`.
Rollback image: `weazltunes-web:rollback-20261020T205121Z`; receipts include the
previous image/source/environment, rollback override, build log, pre/post checks,
encoder proof and CPU/API proof. Archive listing and checksum were verified
before new runtime startup.

Installation identity/key digest and 17 device records, 5,995 master assets and
six manifests match pre-deployment counts; the single existing MP3 derivative
remains present. API revision2026-10-03.2 and discovery response are unchanged.
Root/health and anonymous browser/native auth boundaries pass. The production
image still encodes/decodes MP3/44100/stereo/128000.

The exact image was also started as a disposable isolated installation with
private tmpfs data, no network, no production volume/account and the production
transcode settings. Authenticated capability discovery returned:

```json
{"reservedApplicationCores":2,"transcodeCores":22,"maxWorkers":5,"storageWorkerLimit":5,"encoderThreadsPerJob":1,"workerModel":"parallel-processes"}
```

Its API process retained CPU affinity0–23; an actual FFmpeg subprocess used0–21.
The disposable account, database and container were removed afterwards. No
authenticated production account, library mutation or listening-history update
was used for validation. The receipt commit is documentation only and does not
require a second runtime restart. Hardware playback acceptance remains separate.

### Authorized production backup cleanup — 2026-10-20

The user authorized retiring old backups to conserve production disk space.
Confirmed the current runtime image above remained healthy with zero restarts,
and recomputed the retained `20261020T205121Z/data.tar.gz` SHA-256 against the
recorded value above before deletion. Removed the older
`20261020T194836Z/data.tar.gz` and 11 older rollback image tags (nine image IDs).
Retained the latest verified full-volume archive and immediate rollback image;
historical receipt metadata/checksums remain, with an archive-retirement marker.
No production data volume was modified and no service restart was needed.

The production receipt `20261020T205121Z/cleanup.txt` records the exact deletions,
image identities, health and before/after filesystem availability. Net reclaimed:
2,729,304,064 bytes (2.54 GiB). Available space afterwards: 14,202,728,448 bytes
(13.23 GiB); filesystem usage 56%. Runtime remains
`sha256:e8a348249e11416e8f0f9a6791d0d7fb78feb69dd1ed789bc0be8d58b96749e0`,
healthy, zero restarts. This receipt update is documentation only.

### Automatic device artwork and API reference — 2026-10-24

Added the advertised `rgb565-112` cover lease profile without changing original
cover/audio defaults. Tests cover RGB565 byte order and crop, progressive JPEG,
malformed/oversized input, authenticated lease delivery, SHA-256, HEAD, Range,
ETag, missing art, incompatible audio quality and revoked access.

Validation before deployment: `go test ./internal/server -run TestPodCover -count=1`,
`go test -race ./...`, `go vet ./...`, and production Docker build passed. The
complete [API reference](API_REFERENCE.md) was checked against route registration,
dispatch handlers and response schemas, including browser compatibility behavior.
Production deployment and physical-device artwork acceptance are pending; the
source checks above do not claim those have completed.

#### Production deployment receipt

Deployed source `045a364` on 2026-10-24 using the prebuilt production image
`sha256:ba841acfd544ba924b2858724a22aca5d05daf0e9bcad860a53927559f07467b`.
The runtime is healthy with zero restarts. `/healthz` and `/api/v1/info` passed;
authenticated capabilities advertise `rgb565-112`. Three real album thumbnails
returned exactly 25,088 bytes each with matching response SHA-256.

Before stopping the service, read-only encrypted-state audit found zero active
captures and zero schedules due within five minutes. A stopped-service full-volume
backup was created and gzip/archive validation passed before replacement:

- Receipt directory: `/home/bobp/weazlmusic-backups/20261024T165248Z` on production.
- Archive: `data.tar.gz`; SHA-256
  `a923b8f01f9bb97b8ef61af6e6023f955689ef8e31ca86f7e7442631aad085e7`.
- Rollback tag: `weazltunes-web:rollback-20261024T165248Z`, retained runtime image
  `sha256:e8a348249e11416e8f0f9a6791d0d7fb78feb69dd1ed789bc0be8d58b96749e0`.
- Before/after audit preserved the encryption-key digest and all 5,995 asset,
  six manifest, six job, 67 scrobble and four station collection records.

Browser sessions expired on restart as expected. No playlists, listening history,
recordings or SD audio files were deleted for this deployment. Runtime code is
unchanged by this documentation receipt.

Physical yellow-board backfill downloaded three distinct covers for all five
existing audio files without re-grabbing audio. Each SD payload matched the
server checksum before and after reset. Cached playback displayed all three
album covers with Wi-Fi off. Steady playback logged zero underruns, but the
combined pause/resume test exposed a transition underrun in firmware; a firmware
fix has been flashed and its uninterrupted acceptance run remains pending.
This does not affect the server thumbnail checks or the verified SD downloads.

#### Final firmware acceptance

The follow-up yellow-board build passed the uninterrupted hardware check at
2026-10-24 13:01–13:03 America/New_York. It preserves queued PCM while asynchronous
Bluetooth pause suspension is pending. Two offline boots retained the cache;
all five original tracks' cover payloads matched server hashes, three album
selections displayed cached art and played over A2DP, and quick pause/resume,
navigation and track changes completed with zero logged underruns. Repeated
artwork refresh used existing files without waking Wi-Fi. Six audio tracks were
present and preserved at the final check (one additional track had been selected
during manual interaction). Artwork refresh reported six ready, zero failures.

Hardware workspace evidence:
`/home/bobp/hardware/weazlpod/test-results/artwork-20261024-130154.log`;
repeat with `tools/check-artwork-device.py` and saved headphones powered on.
Host cache/strip/checksum tests also pass under AddressSanitizer and UBSan.
The board was left stopped. This is a short device acceptance run, not a long
battery/endurance test or subjective audio-quality assessment.

### Radio quality profiles — 2026-10-25

Added live-only 64/96/128 kbps CBR MP3 selection at 44100 Hz, two-channel joint
stereo. Existing Weazlpod requests default to 128 kbps; finite audio derivatives
and ordinary browser/iOS relay behavior remain unchanged. Selection is bound to
the authenticated station lease; conflicting/invalid selectors return 422.

Before deployment, `go test -race ./...` (server 193.724 seconds), `go vet ./...`,
production Docker build and `scripts/verify-weazlpod.sh` against the exact image
passed. The live integration matrix covers MP3 and AAC sources at all three
bitrates, every complete captured MP3 frame, ffprobe format, ICY stripping/SSE
metadata, duplicate/account limits, disconnect/reconnect and device revocation.
The packaged checks include CPU affinity and restricted-quota rejection.
Logs are in ignored `test-results/radio-quality-20261025/`.

Production deployment and real Prendie acceptance will be recorded below. No
firmware was changed or flashed for this work. Server format/decode tests are not
subjective listening or yellow-board playback acceptance.
