# Weazlpod server workbook for Sol

Created 2026-10-20. Status: **implemented, verified and deployed 2026-10-20; physical ESP32 acceptance pending**.

Implement a Weazlpod client profile in the music server so an ESP32-S3 player can stream and download predictable **MP3 at 128 kbps**. Sol owns the server header handling, profile negotiation, transcoding, and delivery. The implementation, finalized contract and evidence are recorded below; physical-device acceptance remains separate.

## User decisions

- Weazlpod is a compact hardware music player using this WeazlMusic backend for streaming and local caching.
- Strongly prefer ESP32-S3 because it runs firmware and starts quickly. Shape the server output to suit the device.
- The device identifies itself with a Weazlpod header; the server recognizes it and supplies MP3 at 128 kbps for playback and downloaded caches.
- Orange Pi Zero 2W with 2 GB RAM is a contingency for a demonstrated essential requirement the S3 cannot meet.
- This is presently a feasibility study. Deliver the server work and a reproducible client contract; physical-device acceptance happens separately.

The device study lives at `/home/bobp/hardware/weazlpod/README.md`. A candidate board is Waveshare ESP32-S3-Touch-LCD-1.47 with 8 MB PSRAM, 16 MB flash, touch display, and microSD. It needs an external audio output stage. This task does not implement firmware or change the iOS app.

## Read first and verify the baseline

Read `README.md`, `SOL_WORKBOOK.md`, `docs/SUBWEAZL_API_V1.md`, `docs/SUBWEAZL_QUALITY_HANDOFF.md`, `docs/MEDIA_TOOLCHAIN.md`, `docs/RECORDER_CHUNKS.md`, and `docs/VERIFICATION.md`. The canonical API contract governs current behavior; older workbook descriptions may be historical.

Inspected local HEAD: `250181b`. Recheck current source and local changes before implementing. No server modifications or production requests were made for this handoff.

| Source | Relevant current behavior |
| --- | --- |
| `internal/server/v1_media.go` | Lease requests reject qualities other than `original`; owns asset delivery and capabilities |
| `internal/server/v1_http.go` | Native API routing and `/info` |
| `internal/server/v1_store.go` | Contract revision `2026-10-03.2` |
| `internal/server/v1_fixture_test.go`, `fixtures/v1/` | Contract and deterministic fixture consistency |
| `internal/server/recorder_encoder.go`, `recorder_compact.go` | Finalized AAC-LC/M4A recording files and faststart packaging |
| `Dockerfile` | Pinned FFmpeg/ffprobe and fdkaac toolchain; MP3 encoder availability still needs checking |

`trackOriginal` fetches Navidrome download bytes. `trackStream` proxies upstream streaming without explicitly overriding upstream player defaults. Neither establishes the new profile. Existing recording output is AAC-LC at 128 kbps, 44.1 kHz stereo; older immutable assets may differ.

## Proposed contract and defaults

The following names and encoding details are implementation defaults for Sol to finalize in the canonical contract. The settled user requirements are client identification and MP3 at 128 kbps for streaming and downloads.

| Item | Proposed default |
| --- | --- |
| Identification header | `X-Weazl-Client: weazlpod` |
| Profile identifier | `mp3-128` |
| Encoding | MPEG Layer III, 128 kbps CBR, 44,100 Hz, stereo |
| Content type | `audio/mpeg` |
| Container metadata | Accurate duration and usable MP3 seek information; omit embedded artwork and bound tags |
| Finite media delivery | Immutable finalized file shared by streaming and downloads |
| Initial cache-miss behavior | Bounded preparation job; expose readiness and retry information before leasing a completed file |
| Live radio | Separate bounded live transcode pipeline; no static-file range/length claims |

### Identification and profile selection

Normalize and validate the header value. Apply the Weazlpod default only to supported audio operations; cover images and JSON must retain their normal types. Unknown client headers must not accidentally enable this profile. Reject conflicting unsupported quality requests explicitly instead of returning bytes in an unexpected format.

The header is a client hint, never authentication. Retain native bearer tokens, device revocation, resource ownership, and installation/account/library isolation. Do not forward the identification header, credentials, or proxy headers to Navidrome or radio origins unnecessarily.

Bind the effective profile and representation into the media lease. Audio GET/HEAD requests must deliver the bound bytes even if they omit the identification header. Do not require an ESP audio library to inject headers into every request. Decide and document whether device identity retains a profile preference or the client sends the header on each JSON request.

Preserve the existing explicit `trackOriginal` meaning. Prefer a profile-aware download operation or separate download kind; do not silently transcode an original download. For a Weazlpod request attempting an incompatible original operation, return a documented conflict/unsupported result with the correct operation to use. Existing clients retain original-download semantics.

Example request intent, with route/kind finalized by Sol:

```http
POST /api/v1/media/leases
Authorization: Bearer <device-access-token>
X-Weazl-Client: weazlpod
Content-Type: application/json

{"kind":"trackStream","resourceId":"<track-id>","quality":"mp3-128"}
```

If preparation is asynchronous, specify its response, status route, ownership, retry interval, cancellation, and retry/idempotency behavior. A client must be able to distinguish preparing, ready, unsupported, storage full, and failed. Do not return an ordinary playable lease for an unfinished file.

### Capabilities and compatibility

Advertise the profile only after checking the actual installed encoder and delivery path. Define response fields for effective profile, codec, bitrate mode, sample rate, channels, representation identity, duration, length, checksum, range support, and readiness. Keep estimated sizes distinct from finalized sizes.

The iOS client at `../iOS/subweazl_app/Packages/SubweazlCore/Sources/SubweazlCore/WeazlTunesClient.swift` currently rejects any contract revision other than `2026-10-03.2`, both in discovery and capabilities. **Do not globally bump the revision and strand the existing app.** Choose and document an additive compatible extension, explicit contract negotiation, or a coordinated client release before changing the default revision. Publish sanitized fixtures with the chosen behavior.

Existing browser/iOS audio defaults and the proposed AAC quality work remain independent. Do not enable unfinished AAC profiles as a side effect of this task.

## Transcoding and cache implementation

Fetch source bytes through existing authorized Go upstream access. Use original/download bytes when generating a derivative; an upstream default transcode must not masquerade as the original source. Feed local files or controlled pipes to the encoder rather than handing it authenticated or arbitrary network URLs. Preserve existing radio URL/DNS/redirect restrictions.

Use the installed FFmpeg MP3 encoder after verifying its presence and output. A potential encoder configuration is `libmp3lame` with 128k bitrate, 44100 Hz, two channels, explicit audio stream selection, and no video/artwork streams. Confirm this against the packaged toolchain and validate encoded samples; this workbook does not assert that encoder is currently installed. Set process time/resource limits and bounded source/input/output sizes.

For finite media:

1. Authorize the resource and establish source identity/version.
2. Look up a derivative keyed by library/source identity, evidence-backed source revision or source digest, and the complete encoder profile/version.
3. Deduplicate concurrent preparation for the same key. Do not hold the global API mutex while fetching or encoding.
4. Reserve storage before work; write staging on the durable data filesystem, not the small container `/tmp`.
5. Validate the completed file with probe and decode evidence, including format, rate/channels, duration, truncation handling, byte length, and SHA-256.
6. Atomically publish it, then issue resource-scoped leases and range responses against that immutable representation.

If the upstream lacks a reliable revision, establish content identity or a documented revalidation strategy before reusing a derivative. Request timestamps, session IDs, and signed URLs are not source revisions. A changed source/profile must not share a representation ID with old bytes. Renewing a lease must retain identity and validators for unchanged bytes.

Keep shared storage deduplication separate from access control. Every job, lease, HEAD, and GET must verify the requesting device/account can access the source. Server caching must not make another user's tracks available by hash or predictable identifier. Revocation remains authoritative.

Define worker/concurrency limits, admission/backoff, configurable disk budget/reserve, eviction of unused derivatives, active-reader protection, restart recovery, and orphan staging cleanup. Transcode caching must not consume capacity reserved for scheduled recordings. No silent deletion of authoritative originals or recording masters.

First uncached playback may wait for full preparation. Measure that wait and expose honest status. Prepare queued or requested pinned media ahead of playback. A later progressive path must have separate semantics; it must not claim immutable byte ranges or finalized checksums while bytes are still being produced.

### Live radio and Flight Recorder

Cover these explicitly in the delivery report; do not assume normalizing library tracks normalizes every audio path.

For Weazlpod live radio, produce the specified MP3 stream through the existing secured relay. Keep its lifetime, disconnect cancellation, reconnect behavior, per-user limits, and ICY metadata handling explicit. Do not claim Content-Length, static ETag, checksum, or byte seeking for an infinite live stream. Keep metadata separate from MP3 audio bytes expected by the decoder. Preserve existing live ownership/probe behavior for other clients.

For recorded radio, retain authoritative AAC assets and existing manifests. Expose cached MP3 derivatives of immutable segments for Weazlpod with representation-specific lengths/checksums. Do not replace AAC bytes under an existing asset identity. Preserve logical session offsets, gaps, duration, and station switching; account for MP3 encoder delay/padding when reporting seek origins. Sequential playback alone is not proof of timeline parity.

If delivery is staged, complete finite library playback/downloads first, then live radio and recorded derivatives. Advertise exactly what works and mark later stages pending.

## Task board

- [x] **W0 Baseline and contract:** inspect current changes, settle header/profile precedence and download operation, document compatibility strategy and readiness responses.
- [x] **W1 Encoder proof:** verify host/container encoder, encode synthetic input fixtures, validate the complete output profile and seek behavior.
- [x] **W2 Finite derivative store:** implement identity, source validation, deduplication, bounded workers, reservations, atomic publication, and recovery.
- [x] **W3 Native delivery:** add capability/header handling, authenticated preparation and leases, immutable HEAD/Range/If-Range, expiration and revocation behavior.
- [x] **W4 Live radio:** implement bounded MP3 conversion and metadata/disconnect handling; retain legacy behavior.
- [x] **W5 Recorded derivatives:** expose MP3 segment variants with timeline/seek-origin metadata and correct identity.
- [x] **W6 Regression and handoff:** run isolated tests, publish fixtures and example requests, update verification and complete the delivery record below.

## Required acceptance evidence

Use synthetic audio, fake Navidrome, isolated radio origins, and temporary data directories. Production libraries, playlists, scrobbles, and user caches are not test fixtures.

| Check | Required result |
| --- | --- |
| Header/default matrix | Weazlpod gets MP3 profile; absent/unknown headers preserve existing behavior; explicit conflicts are deterministic |
| Lease binding | Media GET without custom header still returns the negotiated MP3 bytes |
| Original semantics | Existing original downloads remain byte-identical to fixture source despite upstream streaming defaults |
| Input coverage | FLAC, AAC/M4A, WAV, CBR/VBR MP3, mono, differing sample rates, corrupt and truncated inputs |
| Output validation | MP3, 128 kbps CBR, selected sample rate/channels, accurate duration/seek information, bounded tags, no artwork stream |
| Playback/download identity | Full download equals streamed finite representation; checksum and validators survive lease renewal |
| Range handling | HEAD and 206/416 correct; If-Range match/mismatch correct; no append corruption after 200 fallback |
| Source replacement | Changed source gets a new derivative; old/new bytes never mix |
| Concurrent requests | One bounded encoding per key; unrelated API calls remain responsive |
| Isolation/revocation | Two-user authorization, guessed IDs, revoked devices, expired leases, and inaccessible sources rejected |
| Failure/recovery | Encoder absent/failure, interrupted source, process restart, storage full, quota contention, orphan staging, active-reader eviction |
| Live radio | MP3 output from differing station codecs, clean metadata separation, reconnect/disconnect cleanup, bounded workers |
| Recorded timeline | Start/middle/end seeks, segment transitions, gaps, station changes, encoder delay, derivative hash/length distinct from AAC |
| Compatibility | Existing native contract discovery and browser/media regression checks continue to pass |

Run the repository's appropriate tests, including `make test` for Go/race coverage and browser acceptance when existing web playback is affected. Add focused contract and media integration tests. Exercise the packaged container encoder, not only the workstation FFmpeg. Record actual commands and output summaries; no ESP32 playback claims without device evidence.

## Delivery record for Sol

Fill in as work lands:

- Source baseline: `250181bf92c71846e57951b1e3eab370bb1f1c86`. Implementation commit `e2af997d1d1f86ac4be821980e5db7bb6a52a367`, pushed to origin/main and deployed after explicit user authorization on 2026-10-20.
- Final header/profile: `X-Weazl-Client: weazlpod`, trimmed case-insensitive exact single value. Unknown/repeated/comma values preserve legacy defaults. Request-local preference; explicit MP3 also works without the hint. Conflicting Weazlpod original/other quality fails 422. `cover` stays an image. `mp3-128`; new `trackDownload` preserves separate `trackOriginal` semantics.
- Encoding: packaged FFmpeg 8.0.1/libmp3lame (lame-libs 3.100-r5); MP3 CBR128000/44100/stereo, first audio stream, no video/artwork/tags, finite Info/Xing/LAME header. Exact args in `weazlpod_encoder.go`; executable/linked-library/version/argument fingerprint in the representation fixture. Host FFmpeg n9.0.2/Go1.27.2. Packaged input-matrix, frame-by-frame CBR, full decode/sample-count, seek and real API tests pass via `scripts/verify-weazlpod.sh`.
- Compatibility: keep discovery/capabilities revision `2026-10-03.2`; optional `weazlpod.extensionRevision:2026-10-20.1`. Both inspected iOS exact-revision checks still accept it; no iOS files changed. Existing browser regression and original-integrity/native tests pass. No proposed AAC quality enabled.
- Routes: POST `/api/v1/media/leases`; finite MP3 gives 202/Location/Retry-After 2. Authenticated GET `/api/v1/media/preparations/{id}` polls and issues a ready lease; DELETE cancels pending shared work (204). 429 busy/Retry-After 2, 507 storage_full/Retry-After 30, failed/cancelled state without URL. One-hour ephemeral preparations; restart POST after 404/restart. Media URL alone binds MP3. Full examples: `docs/WEAZLPOD_FIRMWARE.md`; canonical contract and preparing/ready/failed/live fixtures updated.
- Cache: library + freshly fetched original SHA-256 + actual encoder build/libraries/args. Every new POST after completion re-fetches/hash-checks original; in-flight requests deduplicate. Representation and immutable bytes survive renewed leases/restart; current source authorization is checked on polls and GET/HEAD. Two finite workers by default (TRANSCODE_WORKERS, cap 8); 15-minute deadline, 512 MiB source/256 MiB output/4-hour presentation limits; 768 MiB reservation each. TRANSCODE_BUDGET_BYTES defaults to 4 GiB. Recorder reservations and CAPTURE_RESERVE_BYTES protected in both admission directions. LRU eviction excludes pinned leases and readers; durable staging, full validation, atomic publish, startup indexed-hash checks/orphan cleanup. Conservative reservations can reject tiny cache hits when less than 768 MiB admission headroom remains.
- Finite library streaming/downloads, live MP3 and finalized recording-segment derivatives are implemented. Live: secured existing resolver, metadata separate, no finite claims, two global workers/one per account, disconnect cleanup/reopen, idle/write deadlines and one-second revocation checks. Recorder AAC masters/manifests unchanged; derivative metadata includes exact presentation sample count, raw LAME encoder delay/padding and separately labeled FFmpeg reference decoder trim. Station gaps/offsets remain authoritative.
- Tests/evidence: see `docs/VERIFICATION.md` Weazlpod entry and local ignored logs in `test-results/weazlpod/`. Full make test (Go race suite), vet, browser regression, packaged production build and packaged focused API/encoder/fixture tests. Synthetic 60-second library preparation measured by TestPodFiniteContract; timing recorded in final evidence. Start/middle/end tone seeks tested on two station timelines, including gaps and changed station offsets; this is Linux reference-decoder evidence, not ESP playback.
- Firmware artifacts: `fixtures/v1/media/mp3-128-stereo.mp3` = 81,083 bytes, SHA-256 `21491ed489b84cd40167a8125b119651fdb01f0e81ffe26459ccd7958ccc6a57`, 221,184 decoded samples / 5,016 rounded ms. Raw encoder delay/padding 576/576; FFmpeg reference skip/discard 1105/47 includes 529 decoder-delay samples. Sample identities/source/encoder digests in weazlpod-representation.json; ready/capability/state/live DTOs checked in. Info frame skipping, decoder-specific trim and reservoir preroll are explicit firmware requirements. Renewal/polling/range/backoff examples in the firmware guide.
- Pending: physical ESP32 audible playback, decoder trim/preroll and seamless transitions, SD/throughput/power acceptance; arbitrary production codec collections; sustained maximum-size/load/long-live soak. Actual disk ENOSPC and abrupt SIGKILL during derivative publication were not induced; admission/reserve failure, encoder cancellation, restart/lease persistence, and abandoned staging recovery are isolated tests. Preparing snapshots are intentionally ephemeral on restart. Live reconnect is client-driven and has no seamlessness/offline guarantee. No iOS device acceptance is claimed. Subsequent user-authorized production deployment is recorded below.

Deliver reviewable server changes and update this workbook, the canonical contract/fixtures, and `docs/VERIFICATION.md`. This handoff requests implementation planning and code work; it does not itself request a production deployment.

## Implementation checkpoint — 2026-10-20

Baseline `250181bf92c71846e57951b1e3eab370bb1f1c86`; only this untracked workbook existed. Read the required API, quality, media, recorder, verification and repository handoffs before editing. Confirmed both iOS exact-revision gates in WeazlTunesClient.swift (lines 37 and 163). Keep default revision `2026-10-03.2`; deliver an additive, capability-gated `weazlpod` extension. No iOS files changed.

Selected `X-Weazl-Client: weazlpod` (trimmed, case-insensitive exact value), `mp3-128`, and a separate `trackDownload` operation. Explicit original with the Weazlpod hint conflicts; absent/unknown hints preserve legacy behavior. Header preference is per JSON request, never persisted on devices. Preparations return 202 with authenticated polling; only finalized bytes receive leases. Original bytes are fetched and hashed for each new preparation, avoiding fabricated upstream revisions.

## Final verification record

`make test`, `go vet ./...`, `tests/run-browser.sh`, production-target Docker build, `scripts/verify-weazlpod.sh`, synthetic fixture generation and sample SHA256SUMS verification passed. Full Go suite includes the race detector; focused host race/API tests and packaged encoder tests also pass. Local sanitized logs are under ignored `test-results/weazlpod/`. Host race-test first preparation of the synthetic 60-second WAV: 575.6 ms; packaged non-race example: 659.6 ms. These are fixture measurements, not device or maximum-input performance guarantees. Both the native revision checks and Codable capability/lease DTOs were inspected; added fields are ignored by those existing DTOs and mediaProfiles remains absent. No native app files changed.

Implementation stages were finite library first, then live relay and recorded derivatives. See `docs/WEAZLPOD_FIRMWARE.md` for concrete requests/responses and the important distinction between LAME encoder priming and reference decoder trim. All task-board implementation items are complete; the remaining physical/load/ENOSPC/SIGKILL acceptance limits above stay explicit. At the implementation handoff, no production push or deployment had been performed; subsequent user-authorized deployment is recorded below.

## Production deployment — 2026-10-20

The user subsequently authorized commit/push and production deployment, overriding the original task boundary. `e2af997` was pushed to origin/main and deployed directly via trusted SSH to `bobp@weazlmusic.teralab.local`, checkout `/home/bobp/weazlmusic`. No jumpbox was needed. Both pre-build and immediate pre-stop checks found zero active captures and no session scheduled within five minutes. The image built while the old service remained live; the service stopped only for a full-volume backup and container replacement.

- Runtime image: `sha256:c3c4a90a7c6e06cd0745ac4818f9ab0f26cfcb67a44137e6184cfcd8050c89c5`; container healthy, zero restarts, nonroot user weazl, original data volume retained.
- Local and LAN HTTP health passed; root 200, anonymous browser/native identity 401. Discovery, installation identity, encryption-key digest, 12 device records, 5,995 master asset records and six manifests matched before/after. Contract revision remains 2026-10-03.2.
- Synthetic encoding in the production image produced MP3/44100/stereo/128000 and fully decoded. No production-account media preparation, playlist or listening-history mutation was used as acceptance.
- Verified stopped full-volume backup and receipts: `/home/bobp/weazlmusic-backups/20261020T194836Z` on production, directory0700/archive0600. Archive SHA-256 `7bb1bc8ff812b1ffd64f50c2d61dc931ca10242dcee7274bd860abe70d2af923`.
- Rollback image: `weazltunes-web:rollback-20261020T194836Z`; receipt directory includes rollback.yaml, previous source/image, build log, before/after checks and encoder proof. Physical-device and stress acceptance limitations above are unchanged.
