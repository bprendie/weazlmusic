# Subweazl and Flight Recorder API contract

Revision: **draft-2026-10-03.1**. Status: implementation target, no v1 routes exist
at the audited baseline. Canonical copy: WeazlTunes `docs/SUBWEAZL_API_V1.md`.
The iOS repository carries an identical review snapshot at
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

Success entities use `{ "data": ... }`; lists use
`{ "data": [...], "nextCursor": null }`. Pagination uses opaque `cursor` and
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
upstream authenticated URLs. Use 401/403/404/409/410/422/429/502/503 appropriately.
Mutation responses may be 200/201 with data or 204 with no body; document per route.

Create, stop and mutation requests carry `Idempotency-Key` UUID. Scope by account,
route and normalized body hash; replay same request/result, reject reused key with
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

Proposed lifetimes: access 15 minutes, refresh 30 days sliding with explicit revoke.
Use `Authorization: Bearer` for native JSON APIs; legacy browser cookie requests
retain origin/CSRF rules. Refresh idempotency must survive restart and lost response:
retain the encrypted exact result for a bounded retry window (at least 2 minutes),
then reject reuse. Client serializes refresh; do not log refresh or login bodies.
Account/security revocation remains authoritative over idempotency replay.

Capabilities shape:

```json
{"data":{"library":true,"originalDownloads":true,"favorites":["track","album","artist"],"playlistReplace":true,"scrobble":true,"radio":true,"mood":false,"flightRecorder":{"enabled":true,"maxStations":6,"defaultDurationMs":14400000,"maxDurationMs":43200000,"maxConcurrentStreams":6,"recurrence":["once","daily","weekly"],"inputFormats":["mp3","aac"],"outputProfiles":["aac-lc-m4a"]}}}
```

This example describes the target, not current support. Output profile is
provisional until S0 packager/player fixture evidence; pin the actual format in
capabilities and fixture manifests before both clients implement it.

## Library and music mutations

| Method and route | Request or response |
| --- | --- |
| GET `/library/albums` | cursor/limit/sort → Album list + snapshot revision |
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

URL is resource-scoped authorization, never an upstream credential. Proposed
maximum lease lifetime 12 hours, bounded by ownership/device revocation, sufficient
for the first overnight transfer scenario; clients renew before retrying expired
Range requests. Prefer same-origin URLs. Do not log query strings. GET and HEAD
support ranges, 206/416, ETag/If-Range and stable content length for immutable files.
Live streams have null byteLength/hash and no byte-seek claim. Original download
means original bytes; transcoding is only a separately advertised quality.

This design lets Apple media APIs open a normal URL without undocumented custom
header injection. Proxy headers at the outer reverse proxy still need a supported
native path or authenticated download-to-local fallback and device validation.

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
| GET `/radio/directory` | provider, query, cursor/limit; provider capability advertised |
| GET `/radio/events` | playbackId issued with radio lease; authenticated metadata SSE, never owns recording jobs |

Queue and playback position remain per device; no native client writes legacy
`/api/state`. Legacy browser state writes must migrate through the same preset
store or they can undo native edits. Return stale-write conflicts after migration.

Mood extension (capability gated): POST `/mood/jobs` `{seedTrackId}` → jobId;
GET `/mood/jobs/{id}/events` → typed selection/progress/completed/failed events;
DELETE `/mood/jobs/{id}` cancels. Reuse current ownership and confirmed incremental
playlist semantics. GET/PUT `/preferences/curator` and POST
`/preferences/curator/models` mirror existing curator options without returning
saved secrets. Define fixtures before enabling native Mood. Admin installation
settings remain browser-managed in this release.

## Flight Recorder scheduling and ownership

| Method and route | Contract |
| --- | --- |
| GET `/flight-recorder/storage` | `{usedBytes,budgetBytes,availableBytes,reserveBytes,retention}` |
| GET `/flight-recorder/schedules` | Owned schedule list |
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

Schedule response adds `{id,version,state,nextStartsAt,nextEndsAt,stationSnapshots}`.
Recurring request also includes recurrence `{kind:"daily"|"weekly",localStart:"22:00",
localEnd:"04:00",weekdays:[1,2,3,4,5],endDate:null}`; ISO weekday 1=Monday. Equal
start/end is invalid, not a silent 24-hour recording. For a one-off fold/gap the
UI must resolve and confirm exact instants before submitting. For recurrence:
nonexistent local boundary shifts forward by the DST gap; ambiguous boundary
uses its first occurrence; compute both UTC boundaries and show actual duration.
If DST produces a window over duration/capacity limits, skip that occurrence with
an explicit reason. Record occurrence identity so restart cannot fire it twice.

Freeze selected station IDs, labels and resolved-input source URLs per occurrence;
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
Station progress includes `{stationId,state,capturedDurationMs,bytes,error?}`.

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
no station shifts earlier to close them. Metadata uses the same timeline.

Manifest revision and assets are immutable after finalization. Corrections publish
a new revision. Download identity is installation/account/session/revision/asset;
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

S0 publishes sanitized JSON fixtures for every DTO/error and a fixture server
with two users, independent devices, repeat playlist entries, original/ranged
media, expired/renewable leases, six tracks and a reconnect gap. Include 30-minute
leave/20-minute return timeline assertions, global pause, restart recovery,
capacity failure, partial download and checksum failure. Contract changes require
fixture and client-snapshot updates. Apple background transfer/AVPlayer support
remains native-device evidence even when server tests pass.
