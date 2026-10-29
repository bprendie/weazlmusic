# WeazlTunes API reference

Implementation reference updated 2026-10-29. Covers every supported HTTP API route
in this repository, including the browser compatibility API, native API v1,
Weazlpod media extension, and health check. It describes the code in this revision;
it is not a statement that a particular installation has deployed it.

The native contract revision is `2026-10-03.2`; the MP3 extension revision is
`2026-10-20.1`. Thumbnail support is additive and advertised by `coverProfiles`.
Discover capabilities at runtime. The normative native lifecycle contract is
[SUBWEAZL_API_V1.md](SUBWEAZL_API_V1.md). Firmware integration is documented in
[WEAZLPOD_FIRMWARE.md](WEAZLPOD_FIRMWARE.md), and operational configuration in
[DEPLOYMENT.md](DEPLOYMENT.md) and [MEDIA_TOOLCHAIN.md](MEDIA_TOOLCHAIN.md).

## Contents

- [HTTP conventions](#http-conventions)
- [Identity and authentication](#identity-and-authentication)
- [Capabilities](#capabilities)
- [Library](#library)
- [Media leases and downloads](#media-leases-and-downloads)
- [Radio](#radio)
- [Device queue](#device-queue)
- [Curator and Mood](#curator-and-mood)
- [Flight Recorder](#flight-recorder)
- [Browser compatibility API](#browser-compatibility-api)
- [Health](#health)
- [Client recipes](#client-recipes)
- [Source map and maintenance](#source-map-and-maintenance)

## HTTP conventions

All native paths below begin `/api/v1`. Browser paths explicitly begin `/api/`.
Use the configured WeazlTunes origin, not the Navidrome origin. JSON is UTF-8.
Send `Content-Type: application/json` for JSON bodies. Native timestamps are
RFC3339, durations/positions are integer milliseconds, sizes are bytes, hashes
are hexadecimal SHA-256, and identifiers are opaque strings. Browser saved tracks
use seconds instead. Treat unknown response fields as forward compatible; typed
request bodies generally reject unknown fields. Non-GET/HEAD bodies are limited
to 1 MiB. Length limits in handlers are UTF-8 byte lengths.

Native JSON success:

```json
{"data":{"id":"opaque-id"}}
```

Paged success:

```json
{"data":[],"nextCursor":null,"snapshotRevision":"opaque-revision"}
```

A 204 response has no body. Media and event streams do not use JSON envelopes.
API responses default to `Cache-Control: no-store`; immutable files and generated
covers override this with `private, max-age=0, must-revalidate`.

### Authentication and origin checks

Native requests use `Authorization: Bearer <accessToken>`. Most native endpoints
also accept a valid browser session, except device-only operations such as queue
storage. Media asset URLs contain their own scoped `lease` credential and do not
require an Authorization header.

Browser mutations, including login, require `X-Weazl-Request: 1`. Native bearer
mutations and native login/refresh are exempt from that header. All mutations
reject `Sec-Fetch-Site: cross-site`; when `Origin` is present its host must match
the request Host. This is a same-origin API, not a general cross-origin CORS API.
Do not forward credentials or proxy headers to arbitrary redirects/upstreams.

### Pagination

Paged routes accept `limit` (default 100, maximum 200) and an opaque `cursor`.
Keep all filters unchanged during a scan; `limit` may change. Snapshots are scoped
to account, library, route and filters, live for five minutes, and may be evicted
when the server's 32-snapshot bound is exceeded. A stale or mismatched cursor
returns 409 `snapshot_expired`. Restart the scan without discarding the previous
valid local library. Devices and saved stations are unpaged.

### Mutations, retries and versions

Unless explicitly exempt below, native POST/PUT/PATCH/DELETE requests require an
`Idempotency-Key` in UUID form. Reuse the same key, body and `If-Match` when retrying
one operation. Results are scoped by account, library, method and path; normalized
JSON and `If-Match` must match. Different content with the same key returns 409
`version_conflict`. Music mutation results last 30 days; recorder results are
retained long term (currently 100 years). An interrupted operation can return
`outcome_unknown`: inspect server/upstream state before attempting a new write.
A new UUID is a new operation, not a way to safely retry an ambiguous write.

Exemptions: login, media lease creation, preparation deletion, logout and device
revocation. Refresh uses its own required UUID key with a two-minute replay window.
GET/HEAD never need an idempotency key.

Versioned edits require the exact quoted ETag from a preceding read/create in
`If-Match`. Missing precondition returns 428; stale precondition returns 409.
Station/schedule/session tags look like `"v3"`; playlist tags are opaque `"p…"`
values. These checks coordinate WeazlTunes clients, but external Navidrome writes
can still race. Re-read after a conflict.

### Errors

```json
{"error":{"code":"validation_failed","message":"Human-readable explanation.","retryable":false,"details":{}}}
```

| HTTP status | Common code | Client action |
| --- | --- | --- |
| 401 | `unauthorized`, `session_revoked` | Refresh an expired access token; sign in again if refresh/revocation fails. |
| 403 | `forbidden` | Check ownership, role and origin headers. |
| 404 | `not_found` | Resource/route unavailable to this account. |
| 405 | `unsupported` | Use the documented method. Some unknown dispatch combinations instead return 404/422. |
| 409 | `version_conflict`, `snapshot_expired`, `capacity_conflict`, `storage_full`, `outcome_unknown` | Resolve the named conflict; do not blindly repeat ambiguous writes. |
| 410 | `asset_expired` | Prepare or lease again if the source still exists. |
| 422 | `validation_failed`, `unsupported` | Correct input or select a supported capability/profile. |
| 428 | `validation_failed` | Read the current ETag before editing. |
| 429 | `rate_limited` | Honor `Retry-After`. |
| 500/502/503/507 | `upstream_unavailable`, `upstream_rejected`, `storage_full`, `unsupported`, `outcome_unknown` | Check `retryable`, capability state and `Retry-After`. |

The common error helper sets `retryable` for 429 and 5xx, except
`outcome_unknown`. A preparation can instead return HTTP 200 with `state:"failed"`
and `errorCode`; inspect the body. Browser errors use `{"error":"message"}`.
The native radio SSE adapter also uses browser-style errors before streaming.
Errors after media/SSE headers are sent may only appear as stream termination.

## Identity and authentication

| Method and path | Input | Success |
| --- | --- | --- |
| GET `/info` | Public; no credentials. | 200 `{installationId,apiVersion:1,contractRevision,minimumClientVersion:"1.0"}` |
| POST `/auth/login` | `{username,password,deviceName,clientId}` | 200 credential object below. No idempotency key. |
| POST `/auth/refresh` | `{refreshToken}` and UUID `Idempotency-Key` | 200 rotated credential object. |
| POST `/auth/logout` | Authenticated; no body/key required. | 204; revokes current native device. |
| GET `/auth/devices` | Authenticated. | 200 array of `{id,name,lastSeen,current}` for nonrevoked owned devices. |
| DELETE `/auth/devices/{id}` | Owned device; no key required. | 204; revokes that device. |
| GET `/me` | Authenticated. | 200 `{accountId,username,libraryId,libraryAvailable,roles}` |
| GET `/capabilities` | Authenticated. | 200 capability object below. |

Credential object:

```json
{"data":{"accessToken":"<secret>","accessExpiresAt":"2026-10-24T17:15:00Z","refreshToken":"<secret>","refreshExpiresAt":"2026-11-23T17:00:00Z","deviceId":"device-id","accountId":"account-id","libraryId":"library-id"}}
```

`deviceName` must be nonblank and at most 100 bytes; `clientId` must be nonempty
and at most 100 bytes. Login follows the installation's normal Navidrome-user or
local-administrator rules. Usernames normalize to lowercase and must match
`[a-z0-9][a-z0-9._-]{2,63}`. Passwords are nonempty and at most 1,024 bytes.
Login attempts are limited by username and source IP (20/minute, then 429).
Browser login includes `Retry-After: 60`; the native login adapter returns
`rate_limited` but currently does not forward that header.

Access tokens last 15 minutes. Refresh tokens rotate and extend expiry by 30 days.
Serialize refresh calls and durably save both replacement tokens. Exact refresh
replay survives restart for two minutes; reuse after that is rejected. Revocation
always takes precedence over replay. Device `lastSeen` reflects credential
issuance/refresh, not every API request. Roles are `['admin']` or `['user']`.

Namespace caches by installation, account and library IDs. Verify `/info` before
reusing credentials at another base URL. Changing library connection invalidates
native sessions; changing only home/remote access URL does not change installation
identity. Native sessions persist across restarts; browser sessions do not.
Logout/revocation cancels the device's Mood generation and media authorization,
but account-owned recording jobs continue.

## Capabilities

Top-level fields:

| Field | Type / meaning |
| --- | --- |
| `contractRevision` | String, currently `2026-10-03.2`. |
| `library` | Boolean; a library connection is configured. |
| `originalDownloads`, `playlistReplace`, `scrobble`, `radio`, `mood` | Boolean feature flags; upstream/configuration can still prevent a particular request. |
| `favorites` | `['track','album','artist']`. |
| `maxPlaylistTracks` | 1000. |
| `radioDirectory` | `['somafm','icecast']`. |
| `coverProfiles` | `['rgb565-112']` when this thumbnail feature is installed. |
| `flightRecorder` | Recorder capabilities below. |
| `weazlpod` | MP3 extension capabilities below. |

`flightRecorder` contains `enabled`, `maxStations:6`, `defaultDurationMs:14400000`,
`maxDurationMs:43200000`, `maxConcurrentStreams:6`,
`recurrence:['once','daily','weekly']`, `inputFormats:['mp3','aac']`,
`outputProfiles:['aac-lc-m4a']`, and
`outputEncoding:{encoder:'fdkaac',profile:'AAC-LC',bitRateKbps:128,sampleRateHz:44100,channels:2}`.
`overnightValidated` and `appleValidated` are currently false; capability
availability is not an assertion of Apple-device or overnight acceptance.

`weazlpod` contains `extensionRevision`, `identificationHeader:'X-Weazl-Client'`,
`client:'weazlpod'`, and `profiles` (empty when encoding is unavailable, otherwise
`[{profile:'mp3-128',codec:'mp3',bitRateKbps:128,bitRateMode:'cbr',sampleRateHz:44100,channels:2}]`).
Extension `2026-10-25.1` also exposes `radioProfiles`: `mp3-64`, `mp3-96`, and
`mp3-128`, each with the encoding fields above and `channelMode:'joint_stereo'`.
All use 44100 Hz and two channels; the bitrate is 64, 96, or 128 respectively.
`radioProfiles` is empty when encoding is unavailable. `profiles` continues to
advertise finite media only. `radioQualityHeader` is `X-Weazl-Radio-Quality` and
`radioDefaultQuality` is `high`.
Boolean fields are `finiteLibrary`, `trackDownload`, `recordingSegments`,
`radioLive`, `preparations`, and `gaplessTrimmingRequired`.
Numeric limits are `maxWorkers`, `storageWorkerLimit`, `reservedApplicationCores`,
`transcodeCores`, `encoderThreadsPerJob`, `maxLiveWorkers`,
`maxLiveWorkersPerAccount`, `maxInputBytes`, `maxOutputBytes`, `maxDurationMs`,
and `budgetBytes`; `workerModel` is `parallel-processes`. Use returned limits,
particularly `maxWorkers`, instead of hardcoding download concurrency.

## Library

All library routes require a configured library, otherwise 503
`upstream_unavailable`. All list successes are paged unless noted.

| Method and path | Query/body | Success |
| --- | --- | --- |
| GET `/library/tracks` | `cursor`, `limit` | 200 Track list for full sync. |
| GET `/library/tracks/{id}` | — | 200 Track. |
| GET `/library/albums` | `cursor`, `limit`, `sort=newest` or default | 200 Album list. |
| GET `/library/albums/{id}` | — | 200 `{album,tracks:[Track]}`. |
| GET `/library/artists` | `cursor`, `limit` | 200 Artist list. |
| GET `/library/artists/{id}` | — | 200 `{artist,albums:[Album]}`. |
| GET `/library/search` | Required `scope=track|album|artist|playlist`; `query`, `cursor`, `limit` | 200 typed list. Playlist search is case-insensitive name substring matching. |
| GET `/library/favorites` | `cursor`, `limit` | 200 `{kind,id}` list. |
| PUT `/library/favorites/{kind}/{id}` | `{enabled:boolean}`; kind track/album/artist | 200 confirmed `{kind,id,enabled}`. |
| GET `/library/playlists` | `cursor`, `limit` | 200 playlist summaries without entries. |
| GET `/library/playlists/{id}` | — | 200 Playlist and ETag. |
| POST `/library/playlists` | `{name,trackIds:[]}` | 201 Playlist and ETag. |
| PUT `/library/playlists/{id}` | `{name,trackIds:[]}`, `If-Match` | 200 replaced Playlist and ETag. |
| DELETE `/library/playlists/{id}` | `If-Match` | 204. |
| POST `/library/scrobbles` | `{occurrenceId,trackId,playedAt}` | 200 `{occurrenceId,status:'accepted'}` after upstream confirmation. |

Every mutation above requires a UUID idempotency key. Playlist names must be
nonblank and at most 200 bytes; `trackIds` is required, may be empty and may contain
duplicates, with at most 1,000 entries. Replacement specifies the entire ordered
playlist. Editing/deleting is owner-only; an active Mood run can block it with 409.
`occurrenceId` is a nonempty string up to 200 bytes identifying one listening
occurrence; `playedAt` is a nonzero RFC3339 timestamp. Retrying an occurrence must
not create a new ID. Ambiguous scrobble outcomes are not safe to blindly repeat.

The upstream album request defaults to alphabeticalByName, but the server's
snapshot sort uses stable serialized DTO ordering except for `sort=newest`.
Clients must not assume the default output is strictly alphabetical. Album detail
tracks preserve upstream order. Full scans beyond the installation bound
(100,000 offset) fail rather than silently truncate.

### Library schemas

| Object | Fields |
| --- | --- |
| Track | `id`, `title`, `artist`, `artistId`, `album`, `albumId`, `durationMs`, `discNumber`, `trackNumber`, `coverId`, `contentType`, `bitRateKbps`, `starred`, `mediaRevision` |
| Album | `id`, `name`, `artist`, `artistId`, `year`, `genre`, `coverId`, `songCount`, `durationMs`, `addedAt`, `starred` |
| Artist | `id`, `name`, `coverId`, `albumCount`, `starred` |
| Playlist | `id`, `name`, `owner`, `canEdit`, `version`, `entries:[{entryId,track:Track}]` |

Names, IDs and content types are strings; counts/durations/track numbers are
numeric; `starred` and `canEdit` are booleans. Unavailable upstream fields may be
null. `mediaRevision` is currently null: do not fabricate revisions from request
time. Playlist `version` is the ETag value without surrounding quotes; `entryId`
distinguishes repeated occurrences of a track. Cover requests use `coverId`,
which need not equal the track ID. Missing cover IDs legitimately use fallback art.

## Media leases and downloads

### POST `/media/leases`

Authenticated; no idempotency key required. Body:

```json
{"kind":"cover","resourceId":"<coverId>","quality":"rgb565-112"}
```

`resourceId` is required, nonempty, at most 256 bytes.

| Kind | Resource ID | Quality |
| --- | --- | --- |
| `trackStream` | Library track ID | Omitted/`original`, or `mp3-128`. |
| `trackOriginal` | Library track ID | Omitted/`original`; original download bytes. |
| `trackDownload` | Library track ID | Requires `mp3-128`. |
| `cover` | Track/album/artist `coverId` | Omitted/`original`, or `rgb565-112`. |
| `radioLive` | Saved owned station ID | Omitted/`original`, or `mp3-64`, `mp3-96`, `mp3-128`. |
| `recordingSegment` | Manifest segment `assetId` | Omitted/`original` recording master, or `mp3-128`. |

An ordinary 200 lease has
`{url,expiresAt,contentType,byteLength,sha256,supportsRanges,profile,playbackId}`.
Unknown sizes/hashes are null; omitted quality is reflected as an empty `profile`.
`playbackId` is non-null only for live radio. Original cover type is `image/*` in
the lease; inspect the actual HTTP response for its concrete image type.
Leasing a cover does not prove its upstream bytes are available until GET succeeds.

URLs are relative same-origin paths containing scoped secrets, typically
`/api/v1/media/assets/{id}?lease=<secret>`. Resolve them against WeazlTunes, keep
them out of logs, and renew after expiry. Maximum lifetime is 12 hours, bounded
by browser-session expiry where applicable. Revocation and library binding are
checked at fetch time; possession of an asset hash is never authorization.

`X-Weazl-Client: weazlpod` defaults audio to `mp3-128`, leaves covers
unchanged, and rejects `trackOriginal` or incompatible explicit audio quality.
The value is case-insensitive after trimming and must occur in one header value.
Other clients retain ordinary defaults. Explicit `quality:'mp3-128'` also works
without that header. Live radio additionally accepts the lower bitrate profiles
below. Unsupported kind/quality combinations return 422.

### Radio quality selection

On `POST /media/leases` with `kind:'radioLive'`, identified Weazlpod clients may
send `X-Weazl-Radio-Quality: low|med|high` to select `mp3-64`, `mp3-96`, or
`mp3-128`. Header values are case-insensitive after trimming. Exactly one value
is required if the header is present: empty, duplicate, comma-separated and
unknown values return 422 `unsupported`. The quality header on another media
kind or without the valid `X-Weazl-Client: weazlpod` hint also returns 422.

Any authenticated client can instead supply JSON `quality:'mp3-64'`, `'mp3-96'`
or `'mp3-128'`. If JSON quality and the quality header are both present they must
match, otherwise 422. Weazlpod with neither selector keeps `mp3-128`; other
clients retain original relay behavior. Low/medium are radio-only and cannot
change existing finite derivatives or their cache keys.

All three profiles are MPEG-1 Layer III CBR, 44100 Hz, two-channel joint stereo.
The server normalizes the source even when it is already MP3; a source's bitrate
or codec can change during live playback. It strips ICY and ID3 metadata from the
audio; use the separate authenticated SSE route for station metadata.
The lease response reports the selected encoding including `channelMode`.
The media URL binds that profile; GET needs no identity/quality header and any
quality header there cannot override it. To change quality, close the stream,
create a new lease and open its URL. Transcoded responses include
`X-Weazl-Radio-Profile: mp3-64|mp3-96|mp3-128`, `Cache-Control: private, no-store`
and `X-Accel-Buffering: no`. Lease URLs remain secrets.

### GET / HEAD `/media/assets/{id}?lease=…`

Finite assets support GET/HEAD, ranges, 206/416 and validators as available.
Original library media proxies upstream validators and range requests, so its
behavior depends on upstream support. Recorded masters and MP3 derivatives are
immutable files with known byte lengths and SHA-256 ETags. Use `If-Range` while
resuming, verify final size/hash, then atomically publish the offline file.
Do not store a partial download as playable audio.

Live radio has no finite length/hash and does not support ordinary byte seeking.
HEAD is rejected. For original relays only, a GET with exactly `Range: bytes=0-1` is a format probe: 200,
at most 16 KiB, no static Content-Length/Content-Range/Accept-Ranges, and no claim
on playback ownership. Other ranges return 416. One ordinary audio GET owns a
playback ID at a time; duplicates return 409. Disconnect releases ownership.
Transcoded MP3 rejects **all** Range requests with 422 and supports GET only.
MP3 live leases also report encoding fields, with null representation/duration.
Live workers are bounded; exhaustion can return 429. A stream failure requires
reopening playback; do not interpret EOF as completion of a finite recording.

### MP3 preparation

Finite `mp3-128` requests can return 202 rather than a usable URL:

```json
{"data":{"preparationId":"prep-id","state":"preparing","kind":"trackDownload","resourceId":"track-id","createdAt":"2026-10-24T17:00:00Z","expiresAt":"2026-10-24T18:00:00Z"}}
```

Poll the `Location` header (`/api/v1/media/preparations/{id}`), honoring
`Retry-After: 2`. GET needs authentication, but no idempotency key. States are
`preparing`, `ready`, `failed`, `cancelled`. Preparing returns 202; failed/cancelled
return 200 with the preparation object and optional `errorCode`. Possible failure
codes include `storage_full`, `unsupported`, `upstream_unavailable`,
`upstream_rejected`, and `transcode_failed`.

GET when ready returns 200 with a newly authorized ready lease. DELETE on that
path returns 204 and cancels a preparing job; no idempotency key is required.
Preparation records expire after one hour and do not survive restart: a missing
record gives 404, an expired retained record 410. Create a new preparation if the
source remains available. Durable representation files and leases can survive.
Busy workers return 429/Retry-After 2; insufficient derivative capacity returns
507/Retry-After 30. Source authorization is checked again on polling and delivery.

Ready response extends the lease with:

| Field | Meaning |
| --- | --- |
| `state`, `preparationId` | `ready`, and preparation identifier. |
| `profile`, `codec`, `bitRateKbps`, `bitRateMode`, `sampleRateHz`, `channels` | `mp3-128`, `mp3`, 128, `cbr`, 44100, 2. |
| `representationId`, `sourceSha256` | Immutable derivative identity and input hash. |
| `byteLength`, `sha256`, `contentType` | Complete MP3 bytes/hash and `audio/mpeg`. |
| `durationMs`, `decodedSamples` | Presentation duration and sample count. |
| `encoderDelaySamples`, `encoderPaddingSamples` | Encoder delay/padding metadata. |
| `decoderSkipSamples`, `decoderDiscardSamples`, `referenceDecoderDelaySamples` | Reference decoder trimming information. |
| `decoderTrimBasis` | `ffmpeg`. |
| `mediaStartMs`, `seekOrigin`, `requiresGaplessTrimming` | 0, `gapless-decoded-sample-zero`, true. |

The MP3 timeline is defined after gapless trimming. Decoder-specific behavior must
be reconciled with these fields before mapping recorded session offsets. See the
native contract's Weazlpod extension for sample-level seek rules. Library MP3
preparation is bounded to four hours; capabilities expose byte and worker limits.

### RGB565 artwork

Only request this profile when `coverProfiles` includes `rgb565-112`. Lease
metadata is `profile:'rgb565-112'`,
`contentType:'application/vnd.weazlpod.rgb565'`, `byteLength:25088`, `sha256:null`,
`supportsRanges:true`, `playbackId:null`.

GET returns exactly 112 × 112 pixels, row-major, two bytes per pixel, little-endian
RGB565, with **no header**. Red occupies bits 15–11, green 10–5 and blue 4–0.
The server requests a small upstream cover, decodes JPEG (including progressive),
PNG or GIF, center-crops a square, scales it and composites transparency on black.
`X-Weazl-Cover-SHA256` and the quoted ETag identify the returned pixel bytes.
Validate both length and hash before saving. Any firmware-specific cache header
is local storage metadata and is not part of this HTTP payload.

HEAD, Range and conditional GET operate on the generated bytes. The source is
re-read each request and may change upstream: use one full 25,088-byte transfer
instead of combining partial requests across versions. A lease does not freeze a
cover. Original/default covers retain their original representation.

Missing upstream art returns 404; transfer/decode failure returns retryable 502;
two occupied conversion slots return 429 with `Retry-After: 1`. Work is bounded by
20 seconds, 4 MiB encoded input, 16 MiPixels decoded input and 8,192 pixels per
source dimension. Retry transient failures; do not permanently classify them as
missing artwork. Existing audio downloads need not be deleted to add cover files.

## Radio

All paths below require authentication. Writes require UUID idempotency keys.

| Method and path | Input | Success |
| --- | --- | --- |
| GET `/radio/stations` | — | 200 unpaged Station array + collection ETag. |
| POST `/radio/stations` | `{name,streamURL,preset}` | 201 Station + item ETag. |
| PATCH `/radio/stations/{id}` | Changed `name`, `streamURL`, `preset`; item `If-Match` | 200 Station + item ETag. |
| DELETE `/radio/stations/{id}` | Item `If-Match` | 204; recording snapshots survive. |
| PUT `/radio/presets` | `{stationIds:[]}`; collection `If-Match` | 200 ordered Station array + collection ETag. |
| GET `/radio/directory` | Required `provider=somafm|icecast`; `query`, `cursor`, `limit` | 200 paged `{name,url,preset}` rows. |
| GET `/radio/events` | `playbackId` from a live lease | 200 SSE metadata. |

Station is `{id,name,streamURL,preset,order,version}`. Names are nonblank and at
most 200 bytes, URLs at most 2,048 bytes and validated HTTP(S) destinations.
There are at most 200 saved stations and eight presets. Duplicate URLs conflict;
preset IDs must be unique and owned. PUT presets orders selected stations first
and clears the preset flag on unselected stations. IDs survive name/URL edits.
Public destination policy is enforced again during resolution/dial; configured
private-origin exceptions are described in [RADIO_PRIVATE_ORIGINS.md](RADIO_PRIVATE_ORIGINS.md).

Radio SSE sends an initial `: ready`, heartbeat comments every 25 seconds, and
`data: {"title":"…","artist":"…","station":"…","ended":true}` records.
`station` and `ended` are optional. The terminal `ended` record closes the stream.
This is latest-state metadata, not a durable event log; Last-Event-ID replay is
not supported. Ownership follows the device/browser media hub. Playback IDs match
`[a-zA-Z0-9-]{16,64}`; each hub permits 16 feeds and two metadata listeners per feed.
Excess listeners return 429. Metadata subscription does not start audio capture.

## Device queue

GET `/queue` returns 200 `{data:{queue:[savedTrack],current:savedTrack|null}}`.
PUT `/queue` accepts the same inner shape, requires an idempotency key and returns
200 with the saved shape. A native device session is required; a browser-only
session returns 422. Storage is scoped to account, library and native device.

At most 1,000 queued entries. `savedTrack` is a legacy-shaped object with required
nonempty `id` (maximum 1,024 bytes), optional `title`, `artist`, `album` (each at
most 2,000 bytes), `coverArt` (at most 1,024), `url`, `duration` and `position`.
Duration and position are finite nonnegative **seconds**; URL, when present, must
pass radio URL validation. Extra track metadata such as `albumId` and `radio` can
be preserved. `current` may be null; individual queue entries may not be null.
Use this route for native queues rather than overwriting shared browser state.

## Curator and Mood

| Method and path | Input | Success |
| --- | --- | --- |
| GET `/preferences/curator` | — | 200 `{provider,url,model,hasKey}`. |
| PUT `/preferences/curator` | CuratorInput below; idempotency key | 200 visible saved settings. |
| POST `/preferences/curator/models` | CuratorInput; idempotency key | 200 `{models:[string]}`. |
| POST `/mood/jobs` | `{seedTrackId}`; idempotency key | 201 `{jobId,state:'running'}`. |
| GET `/mood/jobs/{id}` | Owned job | 200 `{jobId,state,username,deviceId,events}`. |
| GET `/mood/jobs/{id}/events` | Owned job; optional `Last-Event-ID` | 200 replayable SSE. |
| DELETE `/mood/jobs/{id}` | Idempotency key | 204; requests cancellation. |

CuratorInput is `{provider:'off'|'ollama'|'vllm',url,model,apiKey:null|string}`.
URLs are HTTP(S), at most 2,048 bytes, without credentials/query/fragment; models
are at most 200 bytes, API keys at most 4,096. Save requires an available model;
discovery can omit model selection. Omitted/null key preserves the account's saved
key only for the same endpoint/provider; empty string clears it. Secrets are
never returned. `off` clears the account override; effective GET may then inherit
installation defaults. Only browser administrators edit installation defaults.
Validation/discovery errors are adapted from browser handlers and may use 400/422
with native `upstream_rejected` codes.

Mood requires a configured library and curator, and a nonempty `seedTrackId` up to
1,024 bytes. A conflicting active run returns 409. Jobs target 20 valid unique
library tracks, including the seed, and save confirmed selections incrementally
in the upstream Mood playlist. States are `running`, `completed`, `failed`.
Cancellation ultimately records failure/cancellation information, not a fourth
job state; already saved tracks remain.

SSE records have `id: <sequence>`, `event: <type>`, and JSON data
`{sequence,type,playlist?,track?,count,target,message?}`. Types are `progress`,
`selection`, `completed`, `failed`; playlist is `{id,name,owner,songCount}`, track is the
native Track schema. Reconnect with Last-Event-ID to replay later durable events.
The stream closes at terminal state. Disconnecting SSE does not cancel a native
job. DELETE, device logout/revocation or the ten-minute run timeout cancels it;
restart marks interrupted jobs failed and preserves their events.

## Flight Recorder

Recordings belong to the account, independently of the requesting device. They
continue after logout. Recording creation needs enabled media tools; disabled
capture returns 503 `unsupported`, while reads/stop/delete remain available.
All recorder mutations require UUID idempotency keys.

| Method and path | Input | Success |
| --- | --- | --- |
| GET `/flight-recorder/storage` | — | 200 Storage. |
| GET `/flight-recorder/schedules` | `cursor`, `limit` | 200 paged Schedule list. |
| GET `/flight-recorder/schedules/{id}` | — | 200 Schedule + ETag. |
| POST `/flight-recorder/schedules` | ScheduleInput | 201 Schedule + ETag. |
| PATCH `/flight-recorder/schedules/{id}` | Changed ScheduleInput fields; `If-Match` | 200 Schedule + ETag. |
| DELETE `/flight-recorder/schedules/{id}` | `If-Match` | 204; cancels future occurrences, preserves recordings. |
| POST `/flight-recorder/sessions` | `{name?,stationIds,durationMs?}` | 201 Session + ETag. |
| GET `/flight-recorder/sessions` | `cursor`, `limit`, optional `state` | 200 paged Session list. |
| GET `/flight-recorder/sessions/{id}` | — | 200 Session + ETag. |
| POST `/flight-recorder/sessions/{id}/stop` | No If-Match required | 202 Session; starts finalization or returns existing final state. |
| DELETE `/flight-recorder/sessions/{id}` | `If-Match`; must be terminal | 204 tombstone and master asset removal. |
| GET `/flight-recorder/sessions/{id}/manifest` | Optional `If-None-Match` | 200 immutable Manifest + ETag, 304 if unchanged, 409 until finalized. |

### ScheduleInput and admission

```json
{"name":"Evening recording","stationIds":["station-id"],"startsAt":"2030-10-25T02:00:00Z","endsAt":"2030-10-25T04:00:00Z","timeZone":"America/New_York","recurrence":{"kind":"once","endDate":null}}
```

Supply future timestamps appropriate to the installation clock. Names are nonblank
and at most 200 bytes. Select one to six distinct saved **preset** station IDs.
One-off duration must be between one second and 12 hours. `timeZone` must be an
IANA zone even for one-off schedules. Omitted recurrence kind defaults to `once`.

For `daily`/`weekly`, recurrence also contains `localStart:'HH:MM'`,
`localEnd:'HH:MM'`, optional `endDate:null|'YYYY-MM-DD'` (inclusive), and for weekly
`weekdays:[1..7]` (unique ISO weekdays, Monday = 1). An earlier local end means
next day; equal start/end is invalid. DST gaps shift forward; ambiguous boundaries
use the first occurrence. Returned `timeCorrections` explains adjustments.
Windows exceeding duration/capacity limits are not silently stretched.

Immediate sessions default to name `Flight recording` and duration 14,400,000 ms
(four hours); valid duration is 1,000–43,200,000 ms. At most six simultaneous
station streams globally; overlapping demand returns 409 `capacity_conflict`.
Budget/reserve exhaustion returns `storage_full`. Admission reserves twice nominal
encoded size (32,000 bytes per second per station at the current 128 kbps profile).

Each occurrence snapshots station identity/name/URL. Later preset edits do not
rewrite captured history. Schedule and session IDs differ. Editing a schedule
changes future occurrences; stop a session explicitly to end an active capture.

### Recorder response schemas

Storage contains integer `usedBytes`, `budgetBytes`, `availableBytes`,
`reserveBytes`, `reservedBytes`, `accountUsedBytes`, `accountBudgetBytes`,
`accountAvailableBytes`, and `retention:{enabled:boolean,days:number}`.
Available space accounts for both logical budgets and filesystem commitments;
do not infer it from budget minus recorded bytes alone.

Schedule contains ScheduleInput plus `id`, integer `version`, `state`
(`active`, `cancelled`, `finished`), `username`, `libraryId`, opaque `stateKey`,
`nextStartsAt`, `nextEndsAt`, `stationSnapshots:[Station]`,
`timeCorrections:[string]`, and optional `error`.

Session contains `id`, optional `scheduleId`, `name`, `state`, `username`,
`libraryId`, `startsAt`, `endsAt`, `actualStartedAt` (timestamp or null),
`durationMs`, `stationCount`, `capturedBytes`, `stations:[StationProgress]`,
optional `stopReason`/`manifestRevision`, integer `version`, boolean `deleted`,
and `reservedBytes`.

States progress `scheduled → recording → finalizing → complete|partial|failed`,
or `scheduled → cancelled|missed`. These five terminal states permit deletion.
Stopping a recording retains captured audio and generally produces `partial` with
`stopReason:'user'`. A restart can resume capture with an explicit timeline gap.

StationProgress is `{station:Station,stationId,state,capturedDurationMs,bytes,
error?,segments:[Segment],metadata:[Metadata]}`. Progress arrays can be null before
capture; finalized manifest arrays are arrays.

Manifest is `{schemaVersion:1,sessionId,revision,name,startsAt,durationMs,state,
totalBytes,tracks:[RecordingTrack]}`. RecordingTrack is
`{stationId,name,order,segments:[Segment],gaps:[Gap],metadata:[Metadata]}`.

| Nested object | Fields |
| --- | --- |
| Segment | `id`, `assetId`, `startMs`, `durationMs`, `mediaStartMs`, `byteLength`, `sha256`, `contentType`, `codec` |
| Gap | `startMs`, `durationMs`, `reason` |
| Metadata | `atMs`, `title`, `artist` |

A recording master uses `contentType:'audio/mp4'`, `codec:'aac-lc'` (FDK AAC-LC,
44.1 kHz stereo, currently 128 kbps). Existing older-rate assets remain valid and
immutable. Use manifest byte counts and hashes rather than bitrate estimates.
Lease each segment using `kind:'recordingSegment',resourceId:segment.assetId`.
At session offset T within a segment, media time is
`segment.mediaStartMs + T - segment.startMs`. Gaps are real missing intervals,
not instructions to concatenate audio and shrink the shared station timeline.
MP3 derivatives have a separate gapless presentation origin; use their lease
metadata when seeking. Deleting a server recording does not remotely erase a
client's already downloaded offline copy.

## Browser compatibility API

These endpoints are the existing web application's API. They use a
`weazl_session` cookie, not native bearer tokens. Success values are **not**
wrapped in `data`. Mutations require `X-Weazl-Request: 1`, but do not implement
native UUID mutation replay. Errors are `{"error":"human-readable message"}`.

The cookie is HttpOnly, SameSite=Strict, Path=/, lasts 24 hours and is Secure when
`COOKIE_SECURE` is enabled. Sessions live in process memory and expire on restart.
Missing/expired cookies return 401. Connected-only routes return 409 if no
Navidrome connection is configured; admin-only routes return 403 for other users.

### Accounts and configuration

BrowserIdentity is `{username,admin,connection:null|{url,username}}`.

| Method and path | Request | Success |
| --- | --- | --- |
| POST `/api/login` | Public `{username,password}` | 200 BrowserIdentity and cookie. |
| POST `/api/register` | Public `{username,password}` | 200 BrowserIdentity and cookie; legacy local-account registration. |
| POST `/api/logout` | Authenticated | 200 `{ok:true}` and expired cookie. |
| GET `/api/me` | Authenticated | 200 BrowserIdentity. |
| PUT `/api/password` | `{current,next}` | 200 BrowserIdentity, replacement cookie; revokes other browser/native sessions. |
| PUT `/api/connection` | `{url,username,password}` | 200 BrowserIdentity, replacement cookie; changes library connection and revokes native devices. |
| GET `/api/admin/config` | Admin | 200 `{adminUsername,navidromeURL,llm:VisibleCurator}`. |
| PUT `/api/admin/config` | Admin `{navidromeURL,provider,llmURL,model,apiKey:null|string}` | 200 `{navidromeURL,llm:VisibleCurator}`. |
| GET `/api/llm` | Authenticated | 200 effective VisibleCurator. |
| PUT `/api/llm` | CuratorInput | 200 saved VisibleCurator. |
| POST `/api/llm/models` | CuratorInput | 200 `{models:[string]}`. |

VisibleCurator is `{provider,url,model,hasKey}`; CuratorInput and endpoint/key rules
are described under Curator above. Admin configuration permits off/empty provider
and validates endpoint/model syntax; account save also verifies model discovery.
Normal users sign in with Navidrome credentials against the globally configured
server. Local administrators authenticate locally. Existing local accounts have
a migration fallback if Navidrome authentication fails. Registration is a legacy
route, not a Navidrome-user creation API. New normal users are provisioned locally
on successful Navidrome login.

Registration/new local password minimum is 10 bytes, maximum 1,024. Username rules
match native login. Connection input caps URL at 2,048, username at 100 and password
at 1,024 bytes, requires nonempty credentials and a valid HTTP(S) URL with no
embedded credentials/query/fragment. Failed connection validation returns 422.
Password mutation affects the local account password, not a Navidrome password.

### Browser library and playlists

GET `/api/library/{method}` requires a connection and returns the upstream
**inner** `subsonic-response` object, including its status/version and resource
field (not native Track/Album DTOs). Only these operations/query keys are accepted:

| Method | Forwarded query keys |
| --- | --- |
| `getAlbumList2` | `type`, `size`, `offset` (defaults newest, 40). |
| `getAlbum` | `id`. |
| `search3` | `query`, `songCount`, `albumCount`, `artistCount`. |
| `getRandomSongs` | `size`. |
| `getPlaylists` | None. |
| `getPlaylist` | `id`. |
| `getStarred2` | None. |
| `getSong` | `id`. |

Size/count parameters must be integers from 0 to 100. Unknown methods return 404;
upstream failures return 502. This is a bounded compatibility proxy, not arbitrary
access to the entire Subsonic API. Credentials are supplied server-side.

| Method and path | Request | Success |
| --- | --- | --- |
| POST `/api/favorite` | `{id,star:boolean}` (track ID) | 200 `{ok:true}`. |
| POST `/api/playlists` | `{id?,name?,songIds:[],remove:[]}` | 200 upstream inner response. |
| POST `/api/playlist-delete` | `{id}` | 200 `{ok:true}`. |
| GET `/api/media/{kind}` | `kind=stream|cover|download`, required query `id` | 200/206 media, or 416; download is an original-file attachment. |
| GET `/api/download/{kind}` | `kind=album|playlist`, required query `id` | 200 streamed ZIP of original tracks, in upstream order. |

Playlist creation omits `id` and requires a nonblank name; removal indices are
invalid during create. An existing ID appends `songIds`, optionally renames, and
removes zero-based positions in `remove`; this is **not** native full replacement.
Names are at most 200 bytes, arrays at most 1,000 entries each. Only the upstream
owner may edit/delete. Invalid positions return 400; an active Mood run returns
409. Browser writes do not use native If-Match protection.

Media streams forward Range and selected media headers. Browser covers request
upstream size 400; there is no RGB565 option on this compatibility route. JSON/XML/
HTML playback responses and non-image cover responses are rejected as 502.

Browser downloads use the signed-in account's upstream `download` endpoint,
never the player-default `stream` representation. Individual file attachments
forward Range/If-Range, actual lengths and upstream validators when available;
HEAD returns headers only. Filenames are sanitized from original track metadata.
Collection ZIPs preserve track order and duplicate occurrences with numbered
entries; audio is streamed sequentially with no recompression or disk staging.
At most two ZIP requests run globally (429, Retry-After: 5); collections must have
1–1,000 tracks (422 otherwise). Missing/unavailable resources return 400/404;
source failures before output return 502. Later failures abort the transfer
without completing the ZIP; disconnect/logout/session expiry cancel upstream
work. ZIP HEAD returns type/disposition without generating audio; ZIP lengths,
validators and resumable ranges are not available. Native lease downloads are
unchanged. See [UI_QUICK_ACTIONS.md](UI_QUICK_ACTIONS.md) for UI coverage.

### Browser state

GET `/api/state` returns `{stationsVersion,stations,queue,current}`. Station entries
are `{id?,version?,name,url,preset}` (browser field `url`, native field `streamURL`).
PUT `/api/state` accepts that same shape and returns 200
`{ok:true,stationsVersion,stations}`. Maximum 200 stations, eight presets and 1,000
queue entries; saved-track validation matches the device queue section.

Send the version from the latest GET. Stale station versions return 409 so browser
writes cannot overwrite newer native presets. Optional `X-Weazl-Device: <UUID>`
on GET/PUT scopes queue/current to a browser device while stations remain shared.
Without it, legacy shared queue storage is used. PUT is a complete state write;
do not send only the queue and accidentally clear station state.

### Browser radio and Mood

| Method and path | Input | Success |
| --- | --- | --- |
| GET `/api/radio/directory` | Required `source=somafm|icecast`, optional `q` | 200 unwrapped array `{name,url,preset}`; browser Icecast results capped at 100. |
| GET `/api/radio/stream` | `url` and optional `playback` | 200 audio relay; public URL policy and playlist resolution apply. |
| GET `/api/radio/events` | `playback` | 200 radio SSE described above. |
| POST `/api/mood` | Connected session; `{seedId}` | 200 NDJSON stream. |

Use the same playback ID for browser stream/events. Native clients use saved
station leases and `playbackId` instead of arbitrary stream URLs. The browser
radio relay also supports the bounded `bytes=0-1` probe and rejects other ranges.

Browser Mood emits newline-delimited JSON, not SSE. Event fields are
`{type,playlist?,track?,count?,target?,error?}`. Types are `started`, `track`,
`done`, `error`, plus `working` heartbeats every 25 seconds. Playlist contains
`{id,name,owner,songCount}`; track contains
`{id,title,artist,album,albumId,coverArt,duration,genre?,year?}` with duration in
seconds. The seed ID is nonempty and at most 1,024 bytes. Missing curator or an
already active run returns 409 before streaming; later failures emit an error
record. Disconnecting the browser request cancels generation (unlike native SSE).
Confirmed upstream selections remain saved. The run timeout is ten minutes.

## Health

GET `/healthz` is public and returns 200 `{"status":"ok"}`. It confirms the HTTP
process is serving; it does not prove Navidrome, directories, curator, encoders or
external radio stations are healthy. `/` serves the web UI/static assets and is
not an API endpoint.

## Client recipes

### Native sign-in and one cover download

1. GET `/api/v1/info`; verify installation identity for the configured origin.
2. POST `/api/v1/auth/login` with user credentials and device/client identifiers.
3. Securely store the credential object; GET `/api/v1/me` and `/api/v1/capabilities`
   with the bearer token.
4. Read a track/playlist and use its nonempty `coverId`.
5. POST `/api/v1/media/leases` with kind cover and quality rgb565-112.
6. Resolve `data.url` against the same origin; GET all bytes, check length 25088
   and X-Weazl-Cover-SHA256, then atomically save the art keyed by library/cover/profile.
7. Reuse that cached cover for other tracks from the same album. A transient
   failure can temporarily display fallback art and be retried without audio deletion.

Example request (placeholder credentials/IDs only):

```http
POST /api/v1/media/leases HTTP/1.1
Host: music.example.invalid
Authorization: Bearer <access-token>
Content-Type: application/json

{"kind":"cover","resourceId":"<cover-id>","quality":"rgb565-112"}
```

### Offline audio and recorder sync

For library audio, request trackDownload/mp3-128, poll preparation when 202,
then download/verify/promote the ready file. Keep the preparation and download
states distinct. For recordings, wait for a terminal session and fetch its manifest;
lease every required asset, verify its own size/hash, and retain gaps/metadata.
Renew expired leases without replacing a valid local manifest or complete file.
Use advertised worker limits for bounded parallel preparation, honor Retry-After,
and preserve the same idempotency key when retrying mutations.

## Source map and maintenance

| Area | Implementation |
| --- | --- |
| Route registration, body/origin policy | `internal/server/server.go`, `v1_http.go` |
| Login, devices, browser accounts | `v1_auth.go`, `auth.go`, `accounts.go` |
| Library DTOs, pagination, mutations | `v1_library.go`, `v1_library_routes.go`, `v1_mutations.go` |
| Leases/capabilities | `v1_media.go`, `v1_media_helpers.go` |
| MP3 preparation, files, live stream | `weazlpod_prepare.go`, `weazlpod_delivery.go`, `weazlpod_live.go` |
| RGB565 artwork | `weazlpod_cover.go` |
| Stations/queue/SSE | `v1_stations.go`, `radio_events.go`, `state_validation.go` |
| Curator/Mood | `llm_settings.go`, `v1_mood.go`, `mood.go` |
| Recorder | `recorder_api.go`, `recorder_types.go`, `recorder_read.go`, `recorder_time.go`, `recorder_quota.go` |
| Browser compatibility | `navidrome.go`, `playlists.go`, `store.go`, `directory.go`, `radio.go` |

When adding/changing a route, update this reference and the relevant contract,
capability advertisement and fixtures together. Keep browser seconds and native
milliseconds explicit. Validate request/response claims against handlers and tests;
do not infer endpoint support solely from a feature flag or a route wildcard.
