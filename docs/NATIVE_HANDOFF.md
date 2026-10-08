# Subweazl handoff — 2026-10-03.2

2026-10-08 follow-up: new captures now use 128 kbps AAC-LC. See
[FLIGHT_SYNC_PERFORMANCE.md](FLIGHT_SYNC_PERFORMANCE.md) for the encoding change
and read-only native sync review. Canonical capabilities/fixtures now advertise
128; refresh the native documentation snapshot when adopting this handoff. Native
source and its historical 160 kbps snapshot were not edited in this pass. Existing
assets keep their bytes, hashes and codec. No API shape/revision change is needed.

The original release receipt follows.

Canonical contract: [SUBWEAZL_API_V1.md](SUBWEAZL_API_V1.md). The identical native
snapshot is `~/Code/iOS/subweazl_app/docs/WEAZLTUNES_API_V1.md`; no native source
was edited or built here. Runtime release commit: `55c005e1dad800a0cebad7ce32b12874c1cc7dab`
(backend `f1ee390`, web UI `981cb2b`). Production is healthy on this release;
VERIFICATION.md records its backup and deployment receipt. The implemented
server preserves legacy browser routes and serves the native `/api/v1` surface.

## Isolated server

Install ffmpeg, ffprobe and fdkaac locally, or use the pinned container toolchain:

```sh
docker build --target fixture -t weazltunes-fixture:v1 .
docker run -d --name weazltunes-native-fixture --init --read-only \
  --cap-drop ALL --security-opt no-new-privileges:true \
  --tmpfs /tmp:size=8m,mode=1777 -p 127.0.0.1:4003:4000 \
  -e LISTEN_ADDR=0.0.0.0:4000 -v weazltunes-native-fixture-data:/data \
  weazltunes-fixture:v1
docker logs weazltunes-native-fixture
python3 scripts/recorder-client.py --duration 60 --output test-results/native
```

Wait for `Fixture ready`. Alice and Bob both use `test-password`. The fixture
initializes its own synthetic Navidrome on 127.0.0.1:4536 and radio transport;
these credentials never authenticate production. For an Apple device on the
local network, bind the fixture port to a suitable interface and configure that
machine's reachable address; the fixture itself is still isolated. Native login
requires username/password/deviceName/clientId. Ordinary login preserves other
devices. API info exposes installation identity; every cache/download also uses
accountId and libraryId. Library IDs persist across fixture container restart.

`go run ./cmd/weazlfixture` is the host alternative (port4003, plus4536). Give it
a temporary DATA_DIR. There is no production environment flag to bypass radio
URL validation: injection exists only in this fixture command and Go tests.

## Payloads and media

Checked-in [fixtures](../fixtures/v1/README.md) cover discovery, independent auth,
normalized optional library fields, duplicate playlist entries, typed favorites,
scrobbles, presets, schedules, sessions, storage, queue, curator, Mood and errors.
The media sample and complete sample manifest have matching size/SHA-256.
The partial manifest contains real six-stream disconnect/stall gaps. Its assets
are runtime IDs from the isolated capture; reproduce them with the client rather
than trying to resolve the static IDs against another installation.

AAC profile changed from the workbook's provisional output: FDK AAC-LC 160 kbps,
44.1 kHz, stereo M4A, matching the user's iPod script. Manifest codec is aac-lc;
content type audio/mp4. Assets are independently seekable faststart segments.
The lease URL is relative; resolve against the verified WeazlTunes origin.
No undocumented AVURLAsset header injection is required. Outer proxy headers
still require a supported authenticated route or download-to-local fallback.

All native mutations need a UUID Idempotency-Key except login, logout, device
revoke and leases. Refresh needs its own stable key across retries; rotation
replays the encrypted exact response for two minutes across server restart.
Use If-Match for versioned updates/deletion. Native snapshot cursors expire in
five minutes or restart; on snapshot_expired restart the scan while preserving
the last valid local cache. Devices/stations lists are unpaged data arrays.

A completed server recording is not phone readiness. Download all selected
manifest assets, verify byteLength and SHA-256, then atomically promote each
file. A fully downloaded partial session is ready offline with explicit gaps.
At shared session T seek `mediaStartMs + T - startMs`; switching does not reset T.
Gap time continues until pause or session end. The browser uses one audio source.
Broadcast delivery offsets/sub-15-second in-attempt stalls are not reconstructed
sample accurately; independent broadcaster clocks are not synchronized.

## Remaining device acceptance

Linux checks cover actual Chromium AAC decode/playback, global switch/pause/seek,
range/resume/hash verification, restart/logout, DST and synthetic radio failures.
AVPlayer seeks/switch latency, background URLSession renewal, locked-screen
playback, interruption handling, proxy headers and six-hour offline use require
Apple hardware. `appleValidated:false` and `overnightValidated:false` remain
explicit until reviewed evidence warrants changing them. Luna runs the six-hour
local fixture soak after deployment; see VERIFICATION.md for status/artifacts.
No production passwords or real playlist mutation tests are part of this handoff.

## Ten-minute chunks (2026-10-08)

New server recordings finalize into approximately ten-minute AAC assets through the existing variable-duration manifest. Native FlightRecorder/FlightPlayback source was reviewed read-only and uses manifest timing; no native changes were made. Existing short assets remain valid. Server packet preservation, seeking, publication rollback and browser playback passed; physical iPhone/background-download throughput and relaunch acceptance are not claimed. See [chunk verification](RECORDER_CHUNKS.md).
