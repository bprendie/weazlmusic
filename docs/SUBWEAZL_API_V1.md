# Subweazl and Flight Recorder API contract

Revision: **2026-10-03.2**. Status: implemented; six-hour fixture soak and Apple
media acceptance remain pending. Canonical copy: WeazlTunes `docs/SUBWEAZL_API_V1.md`.
The iOS repository carries a review snapshot at
`docs/WEAZLTUNES_API_V1.md`. Change the canonical contract and fixtures together;
update the native snapshot before consuming a changed contract.

## Boundaries and conventions

The phone knows WeazlTunes URLs and its device credentials. WeazlTunes alone knows
upstream Navidrome credentials. Preserve home/remote WeazlTunes URLs and optional
proxy headers, scoped to explicitly configured origins. Never forward them to
radio streams or unrelated redirects. Normal native operations must not fall back
to Navidrome silently. Existing `/api/*` browser routes remain compatible.

Base path `/api/v1`. JSON UTF-8; timestamps RFC3339 UTC; durations and offsets
integer milliseconds; sizes integer bytes; IDs opaque strings; arrays preserve
order. One server-generated `installationId`, stable `accountId` and `libraryId`
namespace all caches, files and mutations. Changing the upstream library changes
`libraryId`; changing home/remote address for the same installation does not.
The server must verify installation identity before the client reuses credentials.

Success entities use `{ "data": ... }`; paged lists use
`{ "data": [...], "nextCursor": null, "snapshotRevision": "opaque" }`.
Devices and saved stations use an unpaged `{ "data": [...] }` envelope. Pagination uses opaque `cursor` and
`limit` (default 100, max 200); stable ordering, no silent truncation. A multi-page
scan must retain its snapshot revision or fail with `snapshot_expired`, requiring
restart without deleting the last valid local library.

Errors use HTTP status plus:

```json
{"error":{"code":"capacity_conflict","message":"This recording overlaps another six-station session.","retryable":false,"details":{}}}
```

Required codes: `unauthorized`, `session_revoked`, `forbidden`, `not_found`,
`validation_failed`, `version_conflict`, `snapshot_expired`, `capacity_conflict`,
`storage_full`, `upstream_unavailable`, `upstream_rejected`, `outcome_unknown`,
`unsupported`, `asset_expired`, `rate_limited`. Never include credentials or raw
upstream authenticated URLs. Also `idempotency_conflict` is not a separate code: key/body conflicts use
`version_conflict`. Unsupported methods use 405, missing preconditions use 428.
Mutation responses may be 200/201 with data or 204 with no body; document per route.

Create, stop and mutation requests carry `Idempotency-Key` UUID. Scope by account, library, method,
route and normalized body hash (including If-Match); replay same request/result, reject reused key with
different body. Retain job-create dedupe at least as long as that schedule/session;
retain music mutation results at least 30 days. Do not promise exactly-once
Navidrome scrobbles after an ambiguous upstream failure; record `outcome_unknown`
and avoid blindly repeating an irreversible write.

Versioned edits require `If-Match` from entity `ETag`; missing precondition is 428,
stale is 409 `version_conflict`. These guarantees serialize WeazlTunes clients;
external Navidrome clients can still race. Re-read and report discovered conflicts.

## Identity and capability discovery

| Method and route | Contract |
| --- | --- |
| GET `/info` | Public `{installationId, apiVersion:1, contractRevision, minimumClientVersion}`; no accounts or secrets |
| POST `/auth/login` | `{username,password,deviceName,clientId}` → credential response; use current normal/admin identity rules |
| POST `/auth/refresh` | `{refreshToken}` + `Idempotency-Key` → rotated credential response |
| POST `/auth/logout` | Revoke caller's device session, 204; recording jobs survive |
| GET `/auth/devices` | Owned device IDs/names/lastSeen/current, no secrets |
| DELETE `/auth/devices/{id}` | Revoke owned device, 204; other devices/jobs survive |
| GET `/me` | `{accountId,username,libraryId,libraryAvailable,roles}` |
| GET `/capabilities` | Working feature flags, recording limits, supported media/qualities and endpoint contract revision |

Credential response:

```json
{"data":{"accessToken":"fixture-only","accessExpiresAt":"2026-10-04T02:15:00Z","refreshToken":"fixture-only-refresh","refreshExpiresAt":"2026-11-03T02:00:00Z","deviceId":"device-a","accountId":"account-a","libraryId":"library-a"}}
```

Lifetimes: access 15 minutes, refresh 30 days sliding with explicit revoke.
Use `Authorization: Bearer` for native JSON APIs; legacy browser cookie requests
retain origin/CSRF rules. Refresh idempotency must survive restart and lost response:
retain the encrypted exact result for a bounded retry window (2 minutes),
then reject reuse. Client serializes refresh; do not log refresh or login bodies.
Account/security revocation remains authoritative over idempotency replay.

Capabilities shape:

```json
{"data":{"library":true,"originalDownloads":true,"favorites":["track","album","artist"],"playlistReplace":true,"scrobble":true,"radio":true,"mood":true,"flightRecorder":{"enabled":true,"maxStations":6,"defaultDurationMs":14400000,"maxDurationMs":43200000,"maxConcurrentStreams":6,"recurrence":["once","daily","weekly"],"inputFormats":["mp3","aac"],"outputProfiles":["aac-lc-m4a"],"outputEncoding":{"encoder":"fdkaac","profile":"AAC-LC","bitRateKbps":128,"sampleRateHz":44100,"channels":2},"overnightValidated":false,"appleValidated":false}}}
```

Recorder enabled is false when ffmpeg, ffprobe or fdkaac is absent.
New captures use the AAC profile from `~/ipod_script/ipod.py` at the reduced
128 kbps rate (from 2026-10-08): FDK AAC-LC,
44.1 kHz, stereo, M4A. ADTS is an internal pipe transport only. Assets use
`audio/mp4` and `codec:"aac-lc"`. Ordinary FFmpeg decode/seek and Chromium playback
are tested; AVPlayer/background transfers await Apple device validation.
The server reserves 32,000 bytes/second/station (twice the nominal encoded rate).
Six streams for six hours encode roughly 2.07 GB plus packaging and reserve
4.15 GB of capacity. Existing 160 kbps assets remain immutable and valid; clients
use manifest byteLength/hash rather than deriving sizes from the current encoding. Capabilities also
include `maxPlaylistTracks:1000`, `radioDirectory:["somafm","icecast"]`, and
`contractRevision`. Existing library defaults remain original quality; the optional Weazlpod extension below adds negotiated MP3.

## Library and music mutations

| Method and route | Request or response |
| --- | --- |
| GET `/library/albums` | cursor/limit/sort (`newest` or default alphabeticalByName) → Album list + snapshot revision |
| GET `/library/albums/{id}` | `{album,tracks:[Track]}` in disc/track order |
| GET `/library/artists` | Artist list |
| GET `/library/artists/{id}` | `{artist,albums:[Album]}` |
| GET `/library/tracks` | Stable paged Track list for full local sync |
| GET `/library/tracks/{id}` | Track |
| GET `/library/search` | query + cursor/limit per typed scope `track`, `album`, `artist`, `playlist` |
| GET `/library/favorites` | Typed references `{kind,id}` with pagination |
| PUT `/library/favorites/{kind}/{id}` | `{enabled:true}` → confirmed `{kind,id,enabled}`; kind track/album/artist |
| GET `/library/playlists` | Playlist summaries with owner and canEdit |
| GET `/library/playlists/{id}` | Playlist + ordered entries + ETag |
| POST `/library/playlists` | `{name,trackIds:[]}` → Playlist |
| PUT `/library/playlists/{id}` | If-Match + `{name,trackIds:[]}` → confirmed ordered Playlist; preserve duplicates |
| DELETE `/library/playlists/{id}` | If-Match, owner check, 204 |
| POST `/library/scrobbles` | `{occurrenceId,trackId,playedAt}` → `{occurrenceId,status:"accepted"}` after upstream confirmation |

Track: `{id,title,artist,artistId?,album,albumId?,durationMs,discNumber?,trackNumber?,
coverId?,contentType?,bitRateKbps?,starred,mediaRevision?}`. Album:
`{id,name,artist,artistId?,year?,genre?,coverId?,songCount,durationMs,addedAt?,starred}`.
Artist: `{id,name,coverId?,albumCount?,starred}`. Playlist:
`{id,name,owner,canEdit,version,entries:[{entryId,track:Track}]}`. Summaries omit
entries. Null optional fields stay null; do not invent dates, format support or
media revisions. Entries distinguish repeated occurrences. Cap writes initially
at 1,000 tracks and advertise that limit; never truncate silently.

For album/track media revision, return an evidence-backed upstream revision or a
stable content fingerprint if available. Unknown is null. Do not use session or
request timestamps, which would invalidate every offline file on each sync.

## Media access

POST `/media/leases` body `{kind,resourceId,quality?}` where kind is `trackStream`,
`trackOriginal`, `cover`, `radioLive` or `recordingSegment`:

```json
{"data":{"url":"https://music.example.invalid/api/v1/media/assets/asset-a?lease=fixture-only","expiresAt":"2026-10-04T14:00:00Z","contentType":"audio/mp4","byteLength":123456,"sha256":null,"supportsRanges":true}}
```

URL is resource-scoped authorization, never an upstream credential. Maximum lease lifetime 12 hours, bounded by ownership/device revocation, sufficient
for the first overnight transfer scenario; clients renew before retrying expired
Range requests. Returned URLs are **relative same-origin** paths; resolve against the configured
WeazlTunes origin. `playbackId` is non-null only for `radioLive`. Do not log query strings. GET and HEAD
support ranges, 206/416, ETag/If-Range and stable content length for immutable files.
Live streams have null byteLength/hash and no byte-seek claim. A live radio GET with exactly `Range: bytes=0-1` is a bounded Apple format probe: it receives HTTP 200 and up to 16 KiB, without a static Content-Length, Content-Range or Accept-Ranges header. It does not claim the playback ID, so an overlapping ordinary audio GET on the same lease can stream concurrently. Only one actual audio GET owns that playback ID at a time (a duplicate receives 409); disconnect releases ownership for a later retry. Invalid or revoked leases remain unauthorized. This is compatible with contract revision `2026-10-03.2`. Original download
means original bytes; transcoding is only a separately advertised quality.

This design lets Apple media APIs open a normal URL without undocumented custom
header injection. Proxy headers at the outer reverse proxy still need a supported
native path or authenticated download-to-local fallback and device validation.

### Small device cover profile

`GET /capabilities` advertises `coverProfiles: ["rgb565-112"]`. A cover lease may
explicitly request `{kind:"cover",resourceId:coverId,quality:"rgb565-112"}`.
The result retains the existing scoped lease URL and reports that profile,
`contentType:"application/vnd.weazlpod.rgb565"`, `byteLength:25088`,
`sha256:null`, and `supportsRanges:true`. No default cover representation changes.

GET returns exactly 112 × 112 row-major RGB565 pixels, little-endian, with no
file header. The server requests a 112-pixel upstream thumbnail, decodes JPEG
(including progressive), PNG or GIF, center-crops a square and scales it.
Transparent pixels are composited over black. The response's
`X-Weazl-Cover-SHA256` and quoted ETag identify the returned pixel bytes; verify
the checksum and length before publishing an offline cache entry. HEAD, Range,
and conditional GET use those same bytes. The source is re-read for each asset
request, so the lease does not promise immutable cover contents across upstream
changes; clients should make one complete 25,088-byte request rather than combine
partial downloads from different representations.

Original covers still return their original representation. This profile is
rejected for audio. Existing lease expiration, account/library binding and device
revocation apply. Missing upstream covers return 404; decode/transfer failures
return retryable 502, and occupied thumbnail workers return 429 with Retry-After.
Do not persist a temporary failure as evidence that the album has no artwork.
Two concurrent conversions, 20-second requests, 4 MiB encoded input and 16 MiPixels
decoded input bound the conversion work. This is additive to revision 2026-10-03.2.

## Radio presets and directories

Station: `{id,name,streamURL,preset,order,version}`. IDs survive URL/name edits.
Server owns stream resolution, validates public destinations at request and dial,
and accepts live/recording operations by owned station ID rather than arbitrary
credential-bearing URLs.

| Method and route | Contract |
| --- | --- |
| GET `/radio/stations` | Ordered station list + collection ETag |
| POST `/radio/stations` | `{name,streamURL,preset}` → Station |
| PATCH `/radio/stations/{id}` | If-Match + changed fields → Station |
| DELETE `/radio/stations/{id}` | If-Match, 204; existing recording snapshots survive |
| PUT `/radio/presets` | If-Match collection version + `{stationIds:[...]}` → ordered list; max eight |
| GET `/radio/directory` | provider, query, cursor/limit → `{name,url,preset}` rows; provider capability advertised |
| GET `/radio/events` | playbackId issued with radio lease; authenticated metadata SSE, never owns recording jobs |

GET/PUT `/queue` returns `{queue:[savedTrack],current:savedTrack|null}` and
requires a native device. PUT accepts that same shape (max 1,000 entries), with
Idempotency-Key. Saved tracks use legacy Subsonic fields (`id`, `title`, `artist`,
`album`, `duration` seconds, optional `albumId`/`coverArt`/`radio`).
Queue and playback position remain per device; no native client writes legacy
`/api/state`. Legacy browser state writes must migrate through the same preset
store or they can undo native edits. Return stale-write conflicts after migration.

Mood extension (capability gated): POST `/mood/jobs` `{seedTrackId}` → jobId;
GET `/mood/jobs/{id}/events` → typed selection/progress/completed/failed events;
DELETE `/mood/jobs/{id}` cancels. Reuse current ownership and confirmed incremental
playlist semantics. GET/PUT `/preferences/curator` and POST
`/preferences/curator/models` mirror existing curator options without returning
saved secrets. GET `/mood/jobs/{id}` also returns saved `{jobId,state,username,deviceId,events}`.
SSE supports Last-Event-ID and emits `id`, `event` and JSON data
`{sequence,type,playlist?,track?,count,target,message?}`; playlist is
`{id,name,songCount}`, track uses the normalized Track DTO.
States are running/completed/failed. DELETE and native device logout cancel
generation, but already confirmed selections remain upstream. Restart marks
interrupted jobs failed; reconnect replays durable events without resubmitting.
Curator GET/PUT responds `{provider,url,model,hasKey}`. PUT and models POST accept
`{provider:"off"|"ollama"|"vllm",url,model,apiKey:null|string}`; null preserves a
saved key for the same endpoint, empty string clears it. Models response is
`{models:[string]}`. Never return the saved API key. Admin installation
settings remain browser-managed in this release.

## Flight Recorder scheduling and ownership

| Method and route | Contract |
| --- | --- |
| GET `/flight-recorder/storage` | `{usedBytes,budgetBytes,availableBytes,reserveBytes,reservedBytes,accountUsedBytes,accountBudgetBytes,accountAvailableBytes,retention:{enabled,days}}` |
| GET `/flight-recorder/schedules` | Owned paged schedule list |
| POST `/flight-recorder/schedules` | Create schedule, 201 + Schedule |
| PATCH `/flight-recorder/schedules/{id}` | If-Match, edit future occurrence(s), Schedule |
| DELETE `/flight-recorder/schedules/{id}` | Cancel future occurrence(s), 204; does not delete recorded sessions |
| POST `/flight-recorder/sessions` | `{name,stationIds,durationMs}` immediate recording, 201 + Session |
| GET `/flight-recorder/sessions` | cursor/limit/state → owned list |
| GET `/flight-recorder/sessions/{id}` | Session + per-station progress + ETag |
| POST `/flight-recorder/sessions/{id}/stop` | Idempotent stop/finalize, 202 + Session; keep captured prefix |
| DELETE `/flight-recorder/sessions/{id}` | If-Match; active session must be stopped first; 204 tombstone |
| GET `/flight-recorder/sessions/{id}/manifest` | Finalized immutable manifest + ETag; 409 until published |

Example one-off schedule (22:00–04:00 America/New_York):

```json
{"name":"Friday flight","stationIds":["radio-prendie","defcon"],"startsAt":"2026-10-04T02:00:00Z","endsAt":"2026-10-04T08:00:00Z","timeZone":"America/New_York","recurrence":{"kind":"once"}}
```

GET `/flight-recorder/schedules/{id}` returns an owned Schedule and ETag.
Schedule response adds `{id,version,state,nextStartsAt,nextEndsAt,stationSnapshots,
timeCorrections,username,libraryId,stateKey,error?}`. Treat additional fields as
forward-compatible. States: active/cancelled/finished.
Recurring request also includes recurrence `{kind:"daily"|"weekly",localStart:"22:00",
localEnd:"04:00",weekdays:[1,2,3,4,5],endDate:null}`; ISO weekday 1=Monday. Equal
start/end is invalid, not a silent 24-hour recording. For a one-off fold/gap the
UI must resolve and confirm exact instants before submitting. For recurrence:
nonexistent local boundary shifts forward by the DST gap; ambiguous boundary
uses its first occurrence; compute both UTC boundaries and show actual duration.
If DST produces a window over duration/capacity limits, skip that occurrence with
an explicit reason. Record occurrence identity so restart cannot fire it twice.

Freeze selected station IDs, labels and configured source URLs per occurrence;
future recurring occurrences use the schedule's explicit station selection, not
whatever later becomes the first six presets. Updated/deleted sources require an
explicit schedule edit or show a validation failure. Before dispatch, revalidate
current URL policy. Save job owner account separately from device/session IDs.

Session states: `scheduled → recording → finalizing → complete|partial|failed`;
`scheduled → cancelled|missed`. Finalized early stop is partial with
`stopReason:"user"`. A one-off schedule creates one session; recurrence creates
one session per occurrence. The schedule and its occurrence are distinct IDs.
Session fields: `{id,scheduleId?,name,state,startsAt,endsAt,actualStartedAt?,
durationMs,stationCount,capturedBytes,stations,stopReason?,manifestRevision?,version}`.
Station progress includes `{stationId,station,state,capturedDurationMs,bytes,error?,segments,metadata}`.
Session also returns `username`, `libraryId`, `deleted`, `reservedBytes`.
Progress arrays may be null before capture; finalized manifest arrays are always arrays.

## Offline manifest and shared timeline

```json
{"data":{"schemaVersion":1,"sessionId":"flight-a","revision":"r1","name":"Friday flight","startsAt":"2026-10-04T02:00:00Z","durationMs":21600000,"state":"partial","totalBytes":123456,"tracks":[{"stationId":"radio-prendie","name":"Radio Prendie","order":0,"segments":[{"id":"segment-a","assetId":"asset-a","startMs":0,"durationMs":1200000,"mediaStartMs":0,"byteLength":123456,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","contentType":"audio/mp4","codec":"aac-lc"}],"gaps":[{"startMs":1200000,"durationMs":20400000,"reason":"station_unavailable"}],"metadata":[{"atMs":0,"title":"Fixture track","artist":"Fixture artist"}]}]}}
```

This deliberately small partial fixture is not a claim of real capture. Manifest
contains no expiring URLs or credentials. Resolve each asset through a media lease.
`startMs` is position on the session timeline; `mediaStartMs` is the offset within
that asset. At timeline T select its containing segment and seek to
`mediaStartMs + T - startMs`. Validate finite positive durations, coverage bounds,
non-overlap, unique IDs and hashes. Partial capture lists explicit uncovered gaps;
no station shifts earlier to close a detected disconnect/restart gap.
Timeline anchors use first received audio time and exact media presentation
durations. Sub-deadline stalls within one decoder attempt and broadcaster delivery
delays are not sample-accurately reconstructed. The idle deadline is 15 seconds;
reconnects create new anchors. Do not claim synchronized broadcaster clocks. Metadata uses the same timeline.

Manifest revision and assets are immutable after finalization. This release does
not provide a correction mutation. Download identity is installation/account/session/revision/asset;
validate bytes and SHA-256 before atomic promotion. Client readiness requires all
assets referenced by its selected manifest, plus the manifest itself, locally
verified. A partially captured session can be fully downloaded: show “Ready offline
· gaps” rather than conflating capture gaps with missing downloads.

Playback stores `{sessionId,revision,positionMs,selectedStationId,paused}` locally.
One clock advances only during session playback. Switching station keeps T; pause
freezes T globally. A gap displays “No recording at this time”; the shared clock
continues until paused or the session ends. No background decoding of all six
tracks is required if active-track switching meets measured latency requirements.
No recorded radio scrobbles are submitted as Navidrome library tracks.

## Required shared fixtures and evidence

Checked-in `fixtures/v1` provides sanitized DTO examples, errors, sample audio
and manifests. Equivalent request/response contract tests live in
`internal/server/v1*_test.go` and `recorder*_test.go`. `cmd/weazlfixture` provides
a fixture server
with two users, independent devices, repeat playlist entries, original/ranged
media, expired/renewable leases, six synthetic streams and a reconnect gap. The native handoff is
`docs/NATIVE_HANDOFF.md`; recorder acceptance client is `scripts/recorder-client.py`. Include 30-minute
leave/20-minute return timeline assertions, global pause, restart recovery,
capacity failure, partial download and checksum failure. Contract changes require
fixture and client-snapshot updates. Apple background transfer/AVPlayer support
remains native-device evidence even when server tests pass.

## Persistence and migration

One process owns one durable data directory; an advisory lock rejects a second
writer. Native credentials, idempotency results, stations, schedules and manifests
are AES-GCM encrypted in SQLite WAL alongside the existing encrypted account store.
Keep the encryption key, database/WAL and recordings together in backups. Browser
cookies still expire at restart; native refresh and recording ownership survive.
Ordinary device/browser login does not revoke other devices. Successful password
or account-connection changes revoke native sessions and leases; failed changes
preserve them. Changed library identity namespaces presets/queues/cache IDs anew,
while existing recordings remain owned by the stable account.

Native stations migrate lazily from legacy favorites with stable IDs. Browser
station writes require the shared stationsVersion and return 409 when stale. Web
queues use a browser-local device ID and do not overwrite native queues. Recorder
workers belong to the application and survive logout/tab closure; shutdown leaves
remaining windows recoverable. A stopped session retains its original logical
duration with a stopped gap after the captured prefix. Missing whole windows become
missed, never shifted. Default retention is disabled. Explicit server deletion or
retention produces tombstones; already open transfers may finish, new leases fail.
An offline phone copy is never deleted by a server retention operation.

## Ten-minute recorder assets (2026-10-08)

New recordings retain short capture checkpoints, then finalize contiguous audio into approximately ten-minute AAC M4A assets. Gaps and tails yield shorter assets; failures can retain the original short files. Clients must use startMs, durationMs and mediaStartMs, never assume a fixed segment length. Existing finalized IDs, hashes and bytes remain unchanged. The schema and revision remain 2026-10-03.2. See [chunk policy](RECORDER_CHUNKS.md).

## Weazlpod additive extension — 2026-10-25.1

The default contract revision remains **2026-10-03.2**. The existing iOS client
checks that exact revision at discovery and capability loading. This extension
adds optional fields and operations; no existing field changes type or meaning.
Browser/iOS requests keep their current defaults. AAC quality profiles from
SUBWEAZL_QUALITY_HANDOFF.md remain unavailable. Discover the optional
`capabilities.data.weazlpod` object before consuming this extension; its
`extensionRevision` is `2026-10-25.1`. See checked-in `weazlpod-*.json` fixtures
and [firmware examples](WEAZLPOD_FIRMWARE.md).

### Selection

`X-Weazl-Client: weazlpod` is a request-local hint, independent of authentication.
Trim surrounding whitespace and compare case-insensitively to the exact value;
unknown, repeated or comma-separated values do not enable the hint. Device
preferences are not persisted. Native bearer authentication is unchanged. The
hint is never forwarded upstream. It changes audio lease defaults, not JSON or
cover-image types.

| Request | Effective behavior |
| --- | --- |
| No/unknown hint, existing kind, omitted/original quality | Existing browser/iOS behavior |
| Weazlpod, `trackStream` or `trackDownload`, omitted quality | `mp3-128` preparation |
| Either client, explicit `mp3-128`, `trackStream`/`trackDownload` | Same preparation and immutable representation |
| Weazlpod, `trackOriginal` or conflicting explicit quality | 422 `unsupported`; use `trackDownload` for MP3 |
| No/unknown hint, `trackOriginal` | Original upstream **download** bytes |
| `trackDownload` without `mp3-128` or Weazlpod hint | 422 `unsupported` |
| `cover` with hint and omitted/original quality | Existing image representation |
| Explicit MP3 for `cover`, `trackOriginal`, unknown kind | 422 `unsupported` |
| Weazlpod `recordingSegment` | Prepare derivative of owned finalized immutable AAC asset |
| Weazlpod `radioLive` | Separate live MP3 lease, never a static-file lease |

Live radio also accepts `mp3-64` and `mp3-96`, at the same 44,100 Hz and two
channels. `X-Weazl-Radio-Quality: low|med|high` selects 64/96/128 kbps when paired
with the valid Weazlpod hint on radio lease creation. Explicit JSON quality must
agree with that header. Missing selectors preserve the 128-kbps Weazlpod default;
ordinary clients keep original relay behavior. Invalid/duplicate/conflicting
headers or lower profiles on finite media return 422. Capabilities expose
`radioProfiles`, `radioQualityHeader`, and `radioDefaultQuality` separately from
finite `profiles`. All requested MP3 radio profiles enable joint stereo.
See [full selection and transfer contract](API_REFERENCE.md#radio-quality-selection).

`mp3-128` means MPEG-1 Layer III, **128,000 bit/s CBR, 44,100 Hz, stereo**,
`audio/mpeg`; normalize even already-low-bitrate/MP3 sources. No original or AAC
fallback is permitted. Finite output omits ID3 tags, chapters and artwork and
contains an Info/Xing seek header and encoder trim information. Encoder build and
all settings participate in the cache key. An unavailable encoder gives 422
`unsupported` and empty capability profiles with audio-operation flags false.

### Finite readiness and leases

POST `/media/leases` retains its original body `{kind,resourceId,quality?}`.
For finite MP3 a new preparation returns **202**, `Retry-After: 2` and
`Location: /api/v1/media/preparations/{preparationId}`:

```json
{"data":{"preparationId":"fixture-preparation","state":"preparing","kind":"trackDownload","resourceId":"fixture-track","createdAt":"2026-10-20T12:00:00Z","expiresAt":"2026-10-20T13:00:00Z"}}
```

There is no playable URL, estimated length or unfinished-file lease in a 202.
GET that Location with bearer authentication; it needs no identification header.
202 means keep polling with Retry-After. On success **200** returns a normal
lease extended with `state:"ready"`, `preparationId`, `profile`, `codec`,
`bitRateKbps`, `bitRateMode`, `sampleRateHz`, `channels`, `representationId`,
`sourceSha256`, `durationMs`, `decodedSamples`, `encoderDelaySamples`,
`encoderPaddingSamples`, `decoderSkipSamples`, `decoderDiscardSamples`,
`referenceDecoderDelaySamples`, `decoderTrimBasis:"ffmpeg"`, `mediaStartMs:0`, `seekOrigin`,
`requiresGaplessTrimming:true`, and finalized byteLength/SHA-256. Stop polling
when ready. Every ready GET issues a fresh resource-scoped 12-hour lease of the
same bytes; it does not change their ETag or representation identity. Media
GET/HEAD needs only the issued URL, without bearer or identification headers.
Leases still verify device/browser validity and current source authorization on
**every** GET/HEAD, including cached representations. Existing recorder manifests
and original leases retain their prior shapes and meanings.

Preparations are account/library scoped, expire one hour after creation, and
are bounded to 1,000 retained entries. Concurrent POSTs for a running resource
join its preparation. A POST after completion starts fresh source revalidation:
fetch authorized original download bytes and SHA-256 them, then reuse matching
cached output. Unknown upstream revisions are never guessed. A ready preparation
is an immutable source snapshot; renewing it retains that snapshot even if the
upstream source subsequently changes. POST again to discover replacement bytes.
Source digest, library identity, encoder build and settings define the cache key;
expiring URLs, devices and request times do not. Different resource IDs with
identical source bytes in the same library can share the encode; authorization
remains per resource.

GET of a failed/cancelled preparation returns **200** with
`state:"failed"`/`"cancelled"`, no URL; failed responses include `errorCode`:
`upstream_unavailable`, `upstream_rejected`, `unsupported` (input size),
`storage_full` or `transcode_failed` (invalid audio, encoder/validation/timeout
failure). Start a new POST to retry; failures are never cached as media.
DELETE the Location cancels a shared preparing job for that account, **204**;
existing completed leases are unaffected. Cancellation frees the worker after
process/network cleanup; another consumer of that same preparation must retry.
These reversible preparation operations do not require Idempotency-Key; running
resource deduplication is their idempotency rule. Unknown/restarted preparation
IDs give 404 `not_found`; expired retained IDs give 410 `asset_expired`. A restart
discards ephemeral preparations; restart the POST. Published representations and
unexpired durable leases survive restart, subject to ordinary revocation.

Admission returns **429** `rate_limited`, `Retry-After: 2` when workers/retained
job slots are full; **507** `storage_full`, `Retry-After: 30` when disk/budget
reservations cannot be made. No unbounded work queue exists. Default finite
worker concurrency is automatic (`TRANSCODE_WORKERS=0`), bounded by available media
cores after a mandatory two-core application reserve and by the configured
derivative budget divided by the per-job reservation. A positive worker setting
is a further ceiling; it cannot consume reserved cores. Per-job deadline is 15 minutes,
source limit 512 MiB, output limit 256 MiB, decoded presentation limit 4 hours.
Each admitted job reserves 768 MiB conservatively, including when it ultimately
reuses a cached encode. Reservations protect recorder commitments and the
configured CAPTURE_RESERVE_BYTES. Staging lives under DATA_DIR/derivatives;
TRANSCODE_BUDGET_BYTES defaults to 4 GiB and includes published files plus active
reservations. Full probe/decode, presentation sample-count comparison, length,
SHA-256, file sync, rename, directory sync and encrypted durable metadata precede
publication. On startup hash-check indexed output and discard damaged/orphaned
output and all abandoned staging. No master is removed. Admission evicts only
least-recently-used derivatives whose lease/preparation pins have elapsed and
which have no active readers. Ready-but-unleased output has a one-hour pin;
issued leases extend that through their expiry. A pinned-full cache returns 507.

Immutable ETag is the quoted representation SHA-256, shared across renewed
leases. HEAD returns final Content-Length. GET supports ordinary 200, 206 with
Content-Range, 416, and If-Range: mismatch returns full 200. A resuming client
must replace/revalidate its partial file on 200, **never append**. Verify size and
SHA-256 before atomic offline promotion. Do not use AAC-manifest hash/length to
validate MP3 derivatives.

### Recorded timeline and MP3 priming

Authoritative AAC assets, manifests, logical starts/durations, station order,
metadata and gaps stay unchanged. Fetch the original manifest, then request each
selected asset as `recordingSegment` with MP3 quality (or the hint). Store the MP3
lease metadata with that asset as its selected representation. There is no
separate MP3 manifest or replacement of bytes under AAC asset IDs. Only assets
belonging to finalized, nondeleted owned sessions are accepted. Deleting a master
session revokes access to its derivatives as well.

`durationMs` is rounded **decoded presentation** duration, not MP3 padded frame
time; `decodedSamples` is exact at 44,100 Hz. `mediaStartMs:0` and
`seekOrigin:"gapless-decoded-sample-zero"` refer to PCM after Info/Xing/LAME
trimming. The decoder must skip the Info frame and honor the LAME-tag encoderDelaySamples
and encoderPaddingSamples using its own decoder-delay convention. The explicit
`decoderSkipSamples`/`decoderDiscardSamples` describe FFmpeg's reference decoder,
including `referenceDecoderDelaySamples`; they are not universal embedded-decoder
trim counts. In the checked-in sample the file stores 576/576, while FFmpeg's
529-sample decoder delay produces skip/discard 1105/47. A decoder that already
honors gapless metadata must not apply these trims twice. For a
manifest segment at session offset S and original media origin M, seek to
`M + T - S` in that trimmed presentation. Retain the manifest's logical duration
and gaps even if its final interval clips a padded source tail. Source AAC
priming is removed by the server decoder before MP3 encoding.

For accurate MP3 random access, seek to an earlier frame, decode reservoir
preroll and discard samples to the requested presentation position; alternatively
decode forward from the start. Raw byte-offset seeking and immediately sounding
the first frame after an indexed seek are not timeline parity. Firmware must
verify its decoder's Info/LAME handling, reservoir preroll and trim behavior on
hardware. Server tests verify source/output sample counts and known-tone seeks at
start/middle/end on two station timelines with gaps; ESP32 acceptance is pending.

### Live MP3

`radioLive` with MP3 quality/hint produces a normal station-owned lease with
profile/encoding fields, non-null playbackId, and null byteLength, SHA-256,
representationId and durationMs; supportsRanges is false. It uses the existing
validated Go resolver/relay for URLs, DNS, redirects and playlists. FFmpeg receives
only ICY-stripped audio over stdin. Metadata remains on the authenticated
`/radio/events?playbackId={playbackId}` SSE feed and is never inserted into MP3.
No ETag, Content-Length, Content-Range or Accept-Ranges is emitted. MP3 live GETs
with any Range are 422 `unsupported`; HEAD is 405. Existing non-MP3 Apple probe
and relay behavior is unchanged.

At most **2 live transcodes globally, 1 per account**, separate from finite
workers and capture workers. A duplicate actual GET on a playbackId is 409;
capacity rejection is 429 with Retry-After: 2. A disconnect releases ownership;
reopen the URL with exponential backoff (suggested 1–30 seconds), renew an expired
lease, and do not resume bytes from the old stream. The server ends an attempt
on disconnect, 15-second input idle/write deadlines, lease expiry, revocation or
shutdown. Revocation is rechecked every second for active streams. It does not
silently reconnect across an audio discontinuity: the firmware opens a fresh
attempt. Once HTTP audio has started, encoder/origin failure closes the stream;
there is no JSON/audio fallback. Live audio is not an offline download; use Flight
Recorder plus finalized segment derivatives for offline radio.

### Parallel playlist preparation and application CPU reserve

Optional additive capability fields (base revision remains 2026-10-03.2):

```json
{"reservedApplicationCores":2,"transcodeCores":2,"storageWorkerLimit":5,"maxWorkers":2,"encoderThreadsPerJob":1,"workerModel":"parallel-processes"}
```

These example values describe a four-core installation with the default 4 GiB
derivative budget, not universal constants. On 24 available cores with no SMT or
CPU quota, transcodeCores is 22 and default maxWorkers is five because storage
reservations cap admission. maxWorkers is the minimum of the media CPU-core count,
storageWorkerLimit and any positive TRANSCODE_WORKERS ceiling. Used cache space,
recording commitments and other clients can still cause 429/507 at admission. A
budget too small for one reservation returns 507 rather than falsely advertising
a playable preparation.

The client should keep up to maxWorkers distinct playlist tracks preparing in
parallel through existing lease POSTs and poll each readiness Location. Transfer
ready MP3s while preparing later tracks; unchanged bytes remain deduplicated.
Respect 429/507 Retry-After and maintain the existing checksum/atomic-promotion
rules. This requires no new batch route, changed lease shape or extra media GET
header. It does not automatically change existing clients' scheduling behavior.

All media subprocesses (including live/recorder/validation workers) share CPU
affinity excluding two whole core groups, including their allowed SMT siblings
where topology is available. The API process keeps its full CPU affinity. Visible
cgroup v1/v2 quotas and ancestor limits are honored conservatively, flooring
fractional quotas before reserving two. No safe CPU headroom means empty MP3
profiles and disabled encoding capabilities; a media subprocess cannot fall back
to unrestricted execution. Existing original/proxy/file delivery is unaffected.
Restart the service after changing CPU allocation. libmp3lame itself remains
single-threaded, so parallelism is across tracks, not chunks of one MP3. Existing
immutable representation IDs, hashes and encoder settings remain unchanged.
