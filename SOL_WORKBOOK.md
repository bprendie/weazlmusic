# WeazlTunes server workbook for Sol

Created 2026-10-03. Source baseline: `75da1c5`. Status: **implemented; release checks passed; overnight soak and Apple acceptance pending**.

Implement WeazlTunes as the primary backend for the native Subweazl app and add
Flight Recorder to the server and web UI. Read this workbook, `README.md`,
`docs/PHASES.md`, `docs/DEPLOYMENT.md`, and `docs/SUBWEAZL_API_V1.md` first.
The API contract is the handoff to the independently developed iOS client.

## User decisions

- Subweazl connects to WeazlTunes in place of connecting directly to Navidrome.
  Navidrome remains behind WeazlTunes for library, users and music permissions.
- Flight Recorder captures up to **six saved favorite presets simultaneously**.
- Default recording duration is **four hours**, configurable. A scheduled window
  of 22:00–04:00 is six hours and must cross midnight correctly.
- Recording runs unattended on the always-on server, even if the phone is off,
  the browser closes, or the initiating login expires or logs out.
- Recordings download to the phone for fully local offline listening.
- All stations share one playback timeline. Selecting a preset solos that station
  at the current session offset. Returning twenty minutes later advances it by
  twenty minutes. Pause pauses the whole session; seeking moves all stations.
- Preserve the compact Weazl aesthetic and existing working browser music flows.
  No decorative meters, fake visualizers, promotional heroes or implementation
  details in everyday product screens.

Do not build the iOS application in this repository. Its owner is working from
`~/Code/iOS/subweazl_app/WEAZLTUNES_WORKBOOK.md`. Implement this server and its web
management surface; supply fixtures and evidence for the native client.

## Defaults and scope boundaries

These are proposed engineering defaults, reversible without a new product gate:

| Choice | Default |
| --- | --- |
| Deployment | Existing single Go service and one durable data volume; one replica |
| API | `/api/v1`, explicit capability/version response, preserve legacy web routes |
| Device identity | Independent revocable device sessions with short-lived access and rotating refresh credentials |
| Presets | Keep eight saved slots; at most six selected for any recording session |
| Selection | First six favorites in saved order; user can choose another subset before scheduling |
| Schedules | One-off first; then daily/selected weekdays in an explicit IANA time zone |
| Capture | Public HTTP(S) MP3/AAC first; preserve existing radio URL/DNS/redirect checks |
| Worker cap | Six simultaneous capture streams per installation initially; reject conflicting reservations clearly |
| Duration limit | 12 hours initially; publish actual limit in capabilities |
| History | Keep recordings until deleted or an explicitly enabled retention policy removes them |
| Storage | Configurable capture budget plus a disk-free reserve; reject before overcommitting |
| Browser playback | One audible source; shared logical recorder timeline |
| HLS | Advertise unavailable until implemented and tested; do not mislabel as supported |

Make capacity, storage and retention visible before scheduling. The user selected
the iPod profile: FDK AAC-LC160 kbps/44100/stereo. Six streams for six hours encode
roughly 2.59 GB before packaging; reserve 5.18 GB with headroom. A lower-bitrate
input can grow. Never silently delete
pinned phone downloads when server retention runs. Removing a phone copy and
removing a server recording are distinct actions.

## What the audit established

- `internal/server/server.go` already serves library, media, playlist, favorite,
  radio, state, Mood and identity endpoints.
- `navidrome.go` proxies reads and range requests but has track-only favorites,
  no scrobble endpoint and no explicit original download route.
- `accounts.go` invokes `startSession(..., true)` on ordinary Navidrome login;
  this revokes other sessions. `auth.go` holds sessions in memory for 24 hours.
- `store.go` rewrites stations and queue together. Stations lack stable IDs and
  eight presets are allowed. Multi-device writes can overwrite unrelated state.
- `radio.go` resolves direct/PLS/M3U audio. `icy.go` strips metadata without a
  second audio connection. Current streams belong to HTTP/login lifetimes.
- No scheduler, recording persistence, media indexer, disk-budget manager or
  recorder API exists. The container has a durable `/data` volume and tiny `/tmp`;
  recordings and staging belong under `/data`, not `/tmp`.
- Baseline read-only review ran `go test -race -json ./...`: 20 tests passed.
  This does not establish multi-hour recording or native media compatibility.

## Execution rules

Work in reviewable commits. Preserve source and data migrations; do not replace
working browser routes wholesale. Update this task board, `docs/VERIFICATION.md`
and the final handoff with commands, commit IDs, evidence and limitations.
Never use real users' playlists or listening history for mutation tests.
Use synthetic radio, fake Navidrome and temporary storage. The user explicitly
authorized commit, push to main and production deployment on 2026-10-03, overriding
the original no-deploy boundary. Preserve the existing volume/key and rollback
image; do not mutate real playlists or listening history for tests. The user
authorized Luna to run the six-hour isolated soak after implementation/deployment.

Do not assume the iOS app can store a web cookie and be finished. Device refresh,
server restarts, independent login and authenticated Apple media requests are
explicit acceptance requirements. Coordinate contract changes by editing
`docs/SUBWEAZL_API_V1.md`, publishing fixture changes, and calling them out in the
handoff. Do not change payloads silently while the native client is being built.

## S0 Contract and deterministic fixtures

- [x] Freeze the initial v1 contract in `docs/SUBWEAZL_API_V1.md`; turn its JSON
  shapes into checked-in sanitized fixtures and an OpenAPI document or equivalent
  request/response contract tests. Do not advertise endpoints before they work.
- [x] Add injectable wall/monotonic clocks, temporary media storage and radio
  fixtures with known audio duration, metadata changes, stalls and reconnects.
- [x] Record Go/container/media-tool versions. Evaluate a maintained media
  demuxer/packager, including a pinned FFmpeg toolchain if chosen; record license,
  CPU and image impact. Do not implement byte-count-to-time seeking.
- [x] Test the proposed media output in a standard player and hand samples to the
  native owner for AVPlayer validation. Mark Apple acceptance pending on Linux.

Exit: two users, two library identities, deterministic audio/manifest examples,
contract validation and existing race tests pass. Deliver capability/error/auth/
library/preset/schedule/partial/complete-manifest fixture examples early.

## S1 Native accounts and media authorization

- [x] Add v1 login, refresh, logout and device list/revoke. Store refresh verifiers
  durably; never store plaintext bearer credentials in logs or returned account
  objects. Store upstream tokens in the existing encrypted account store.
- [x] Login on one device must not revoke another. Explicit account security
  revocation still works. Existing browser cookies/CSRF protection remain valid.
- [x] Refresh rotation handles concurrent requests and a lost response using the
  contract's idempotency rule. Scope sessions to account and library identity.
- [x] Restart retains authorized device refresh sessions. Upstream downtime must
  not block valid local device refresh or access to owned recordings.
- [x] Add scoped media leases usable by native playback and resumable downloads.
  Exercise Range/HEAD/416, lease expiry/revocation and renewal. Logs redact lease
  URLs. Do not forward device credentials to Navidrome or radio destinations.
- [x] Preserve server-side upstream permission checks. Password/connection changes
  have an explicit revocation and library-identity migration policy.

Exit: browser + two native devices remain signed in; revoke one independently;
restart/refresh succeeds; anonymous/wrong-owner media requests fail. No Apple
header workaround or undocumented AVURLAsset keys are assumed.

## S2 Complete the music and preset API

- [x] Implement normalized v1 library read shapes, pagination, capabilities and
  library identity. Preserve optional metadata, repeat playlist entries and IDs.
- [x] Add artist/album/track favorites, durable scrobble idempotency and playlist
  ordered replacement with ownership/conflict checks. Never report upstream
  mutation success without confirmation; return ambiguous outcomes explicitly.
- [x] Add original-file download, cover and stream leases; declare quality/seek
  capabilities. Original download must not silently use a transcoded stream.
- [x] Migrate stations to stable IDs and ordered favorites. Provide dedicated
  versioned station/preset CRUD; concurrent edits return conflicts, and changing
  presets cannot overwrite a playback queue.
- [x] Keep queue restore per device. Shared presets and library are account-wide;
  do not force cross-device queue takeover as part of this feature.
- [x] Expose server Mood/directory capability and document its native contract,
  including cancellation and stream events, so the client can support it without
  reverse-engineering browser internals. Admin UI may remain in the web app.

Exit: fixture integration covers all existing native core operations through
WeazlTunes alone, plus browser regressions. Artist/album favorites, playlist
reorder/duplicates, scrobble retry and original download cannot regress.

## S3 Durable scheduler and recorder ownership

- [x] Introduce versioned durable job/session records and recovery migrations.
  Prefer SQLite for transactional scheduler claims, manifests and quotas; record
  the dependency/toolchain choice. Keep encrypted account settings compatible.
- [x] Separate application-owned worker lifecycle from HTTP and login contexts.
  Jobs belong to stable account IDs and freeze chosen station IDs/URLs at creation.
- [x] Implement create/edit/cancel/list schedules, immediate record, start/end
  windows and capacity checks. Server enforces one to six unique owned presets.
- [x] Resolve local dates to UTC before saving one-off jobs. Persist zone/rule for
  recurrence. Return actual UTC instances and explain DST corrections. Never
  silently reinterpret a saved one-off job when the phone changes time zones.
- [x] Atomically claim jobs, limit streams, checkpoint progress and make retries
  idempotent. On restart, resume an active remaining window, mark downtime gaps,
  and mark fully missed windows missed; do not shift the original end time.
- [x] Stop finalizes the captured prefix; cancel-before-start records nothing.
  Cancel future repeats does not delete finished sessions. Delete is explicit.
- [x] Integrate clean worker shutdown with `cmd/weazltunes/main.go`; recover from
  forced termination without advertising unfinished files as complete.

Exit: fake-clock tests cover 22:00–04:00, DST folds/skips, overlap rejection,
restart, cancellation, logout, worker failure and no duplicate firing. Real
capture start tolerance must be measured and recorded; a server cannot capture
broadcast time while it is powered off.

## S4 Capture and seekable session files

- [x] Run bounded independent capture workers sharing one scheduled timeline.
  Reuse radio connection validation and ICY parsing. Never attach library secrets
  to stations; retry with backoff inside the original recording window.
- [x] Timestamp media on a common server clock, mapping media presentation time
  to session time. Record initial delay, disconnects, codec changes and gaps.
  Preserve the experience of simultaneously received streams; do not claim
  sample-accurate alignment of broadcasters with unrelated delivery delays.
- [x] Write durable, seekable segments with exact durations, byte sizes and SHA-256
  checksums. Stage files, validate them, then atomically publish immutable assets.
- [x] Store ICY title changes at session offsets, never as library scrobbles.
- [x] Enforce disk reserve, user/installation budgets, per-station limits and
  stalled-transfer deadlines. One failed station must not kill five healthy ones.
- [x] Finalize a manifest that distinguishes complete/partial/failed and describes
  every track's coverage. A gap must not compress any station's elapsed time.
- [x] Retention/deletion cannot race downloads into silent corruption. Published
  assets are immutable; use tombstones/conflict responses and documented leases.

Exit: generated tone/cue streams prove switching at minute 30 and returning at
minute 50 selects the expected media; gaps preserve alignment. Test VBR, partial
writes, full disk, process kill and reconnect. Run a six-stream, six-hour fixture
soak with disk/CPU/timing results before calling overnight recording validated.

## S5 Download and management surface

- [x] Serve owned session list/detail/manifests, conditional ETags, immutable
  segment assets with HEAD/Range, checksums and renewable scoped media leases.
- [x] Add web Flight Recorder entry under Internet Radio plus upcoming recordings,
  new/edit schedule, active capture, per-station failures, completed sessions,
  storage use and delete/cancel/stop controls.
- [x] Preset picker enforces six, preserves saved order, and freezes its selection.
  Duration defaults to four hours; explicit end time controls overnight windows.
- [x] UI separates server capture status from any device's local download status.
  Server complete never means a phone has a usable offline copy.
- [x] If browser playback is included, implement one shared session clock, global
  pause/seek, preset switch at current time, gap handling and resume position.
  Keep browser background/offline claims separate from native device evidence.
- [x] Provide read-only status polling with backoff initially; SSE is optional.
  Background recording must never depend on an open event-stream connection.

Exit: browser fixture flow schedules, edits/cancels, records, sees partial capture,
plays/switches presets and deletes owned recordings; another user cannot read or
mutate them. API client downloads assets, validates checksums and seeks locally.

## S6 Release and native handoff

- [x] `go test -race ./...`, `go vet ./...`, existing browser smoke and new recorder
  browser/integration checks pass. Run with fresh and migrated temporary volumes.
- [x] Verify container shutdown/restart, volume backup/restore, retention, health
  versus recorder degradation and media-tool deployment. Preserve port 4000.
- [x] Document routes, final DTOs, errors, auth lifetimes, capability flags, limits,
  migration policy and tested media format. Commit fixtures and example clients.
- [ ] Record six-hour soak evidence and any platform/format gaps. Update README,
  DEPLOYMENT, VERIFICATION and this workbook. No blanket "complete" from mocks.
- [x] Hand native owner the exact commit/contract revision, fixture account setup,
  sample manifests/audio and runnable fixture server. No production passwords.

## Progress and resume handoff

| Stage | Status | Evidence |
| --- | --- | --- |
| S0 | Implemented | Contract2026-10-03.2, checked-in DTO/error/media fixtures, pinned tools, actual Chromium/FFmpeg decode; Apple pending |
| S1–S2 | Implemented | Independent devices/restart/replay/revocation, leases/ranges, normalized library and confirmed mutation tests |
| S3 | Implemented | Encrypted SQLite, app-owned workers, injected-clock/DST/capacity/ownership/missed/restart tests |
| S4 | Implemented; six-hour soak run/report recorded, root review pending | Six-stream FDK run, AAC/VBR input tests, forced kill/recovery; 21,600-second synthetic soak completed partial with 4,322 verified assets and minute30/50 tone checks; see SOAK_2026-10-03.md |
| S5 | Implemented | Browser overnight create/edit/cancel, tab-close capture, real AAC playback switch/pause/seek, deletion/mobile |
| S6 | Release checks passed; soak review and Apple acceptance pending | Race/vet, existing/new browser tests, image builds, volume recovery; six-hour soak report is recorded in VERIFICATION.md and SOAK_2026-10-03.md |

Post-release resume prompt:

> Read SOL_WORKBOOK.md, docs/VERIFICATION.md and docs/NATIVE_HANDOFF.md. The
> implementation is deployed from55c005e; preserve production data and the
> recorded rollback backup. Review Luna's six-hour isolated soak report and
> coordinate Apple AVPlayer/background-transfer acceptance with the native owner.
> Do not infer overnight or Apple validation from short fixture runs.

Runtime release `55c005e1dad800a0cebad7ce32b12874c1cc7dab` is committed, pushed to
main and deployed; production health and encryption-key preservation verified.

Evidence boundaries: physical ENOSPC was not induced on the shared filesystem;
reserve exhaustion, quota rejection and invalid partial media are tested.
Timestamp mapping preserves detected disconnect/restart gaps; it does not
reconstruct sub-deadline stalls or synchronize independent broadcaster clocks.
Recurrence capacity uses a one-year horizon and runtime enforcement. Apple
acceptance and six-hour soak are explicit outstanding gates, not mock-based passes.
