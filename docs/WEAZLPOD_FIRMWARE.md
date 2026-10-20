# Weazlpod firmware handoff

Server extension `2026-10-20.1`; base API revision stays `2026-10-03.2` for existing
iOS clients. No firmware changes or ESP32 playback have been validated here.
All examples use inert credentials/IDs. Substitute your authenticated device's
resource IDs; the checked-in IDs are not runtime server resources.

## Identify, discover, prepare

Use the existing `/api/v1/auth/login` and refresh flow; the hint is not identity
or authentication. GET `/api/v1/capabilities` with bearer authentication and check
`data.weazlpod.profiles` includes `mp3-128` and the desired operation flag is true.
See [capability fixture](../fixtures/v1/weazlpod-capabilities.json).

```http
POST /api/v1/media/leases HTTP/1.1
Authorization: Bearer <device-access-token>
X-Weazl-Client: weazlpod
Content-Type: application/json

{"kind":"trackStream","resourceId":"<track-id>","quality":"mp3-128"}
```

For a pinned/offline file, use `kind:"trackDownload"` with the same profile. Both
operations share identical immutable bytes. Send the hint on each new audio JSON
request, or explicitly select quality without it. Do not request `trackOriginal`
with the hint: that combination fails explicitly. Normal clients can still use
`trackOriginal` for original bytes.

```http
HTTP/1.1 202 Accepted
Retry-After: 2
Location: /api/v1/media/preparations/<preparation-id>
Content-Type: application/json

{"data":{"preparationId":"<preparation-id>","state":"preparing","kind":"trackStream","resourceId":"<track-id>","createdAt":"2026-10-20T12:00:00Z","expiresAt":"2026-10-20T13:00:00Z"}}
```

Poll the Location with bearer authentication, waiting at least Retry-After. No
identification header is needed for polling or media transfer. 202 is not a
playable lease. Stop polling at 200 ready; every subsequent ready poll renews the
lease. The exact checked-in [ready response](../fixtures/v1/weazlpod-lease.json)
uses the packaged encoder sample below. A representative abbreviated response:

```json
{"data":{"state":"ready","profile":"mp3-128","codec":"mp3","bitRateKbps":128,"bitRateMode":"cbr","sampleRateHz":44100,"channels":2,"contentType":"audio/mpeg","representationId":"mp3-<stable-id>","url":"/api/v1/media/assets/<lease-id>?lease=<secret>","expiresAt":"2026-10-21T00:00:00Z","byteLength":81083,"sha256":"21491ed489b84cd40167a8125b119651fdb01f0e81ffe26459ccd7958ccc6a57","durationMs":5016,"decodedSamples":221184,"encoderDelaySamples":576,"encoderPaddingSamples":576,"decoderSkipSamples":1105,"decoderDiscardSamples":47,"referenceDecoderDelaySamples":529,"decoderTrimBasis":"ffmpeg","supportsRanges":true,"mediaStartMs":0,"seekOrigin":"gapless-decoded-sample-zero","requiresGaplessTrimming":true}}
```

## Transfer and resume

Resolve relative URLs against the configured WeazlTunes origin. Lease query
strings are credentials: keep them out of logs. Media GET/HEAD uses that URL
alone, without bearer or custom headers. Do not send server credentials to radio
origins or Navidrome.

```http
HEAD /api/v1/media/assets/<lease-id>?lease=<secret> HTTP/1.1

HTTP/1.1 200 OK
Content-Type: audio/mpeg
Content-Length: 81083
Accept-Ranges: bytes
ETag: "21491ed489b84cd40167a8125b119651fdb01f0e81ffe26459ccd7958ccc6a57"
```

```http
GET /api/v1/media/assets/<lease-id>?lease=<secret> HTTP/1.1
Range: bytes=4096-8191
If-Range: "21491ed489b84cd40167a8125b119651fdb01f0e81ffe26459ccd7958ccc6a57"

HTTP/1.1 206 Partial Content
Content-Type: audio/mpeg
Content-Length: 4096
Content-Range: bytes 4096-8191/81083
ETag: "21491ed489b84cd40167a8125b119651fdb01f0e81ffe26459ccd7958ccc6a57"
```

A validator mismatch gives full **200**: replace the partial file; never append.
Out-of-bounds requests give 416 and `Content-Range: bytes */81083`. Check final
length and SHA-256 before moving a download into the verified offline cache.
Key that cache by installation/account/library and representationId. Renewing a
ready preparation (GET, within one hour) preserves ID/hash/ETag/bytes. A new POST
rehashes original source bytes and may select a new representation if changed.
Preparations disappear on restart; durable leases survive. On 410/404, create a
new preparation, then compare the returned representation before resuming.

| Result | Firmware action |
| --- | --- |
| 202 preparing | Poll with Retry-After; do not open a media decoder |
| 200 ready | Transfer or play finalized MP3; stop preparation polling |
| 200 state failed + errorCode | Show preparation failure; retry a new POST if appropriate |
| 200 state cancelled | A consumer cancelled the shared preparation; POST to try again |
| 422 unsupported | Fix kind/quality or choose a server advertising this profile; no codec fallback |
| 429 rate_limited | Retry after Retry-After, increasing backoff if repeatedly busy |
| 507 storage_full | Stop automatic rapid retries; wait at least Retry-After and surface storage status |
| 401 revoked/unauthorized | Authenticate/review device authorization; do not loop downloads |
| 404/410 source or preparation missing | Refresh resource/session state and prepare again if it still exists |

DELETE the preparation Location with bearer authentication cancels pending work,
returning 204. Deduplication is account/resource scoped while preparing, so this
cancels the shared job for its other consumers too. No Idempotency-Key is needed
for these reversible preparation operations.

## Recorded radio and precise seeking

Download the unchanged Flight Recorder manifest. Each segment still names its
AAC master asset, logical start/duration and gaps. Request its resource ID using:

```json
{"kind":"recordingSegment","resourceId":"<manifest-asset-id>","quality":"mp3-128"}
```

Use the returned MP3 representation length/hash for the cached MP3, retaining the
master manifest for logical timing. MP3 representation IDs and hashes must not
replace AAC master IDs/checksums. At logical T in a segment starting at S, seek to
`manifest.mediaStartMs + T - S` in the **trimmed decoded presentation**. Keep gaps
and station-switch positions intact. A segment's logical duration can clip its
source tail; use the manifest duration for the session interval.

Finite files contain Info/Xing/LAME metadata. Firmware must skip the Info frame
and honor encoder delay/end padding using its own decoder-delay convention. The
LAME tag stores 576/576 encoder delay/padding in this sample. FFmpeg adds its
529-sample decoder delay, giving `decoderSkipSamples:1105` and
`decoderDiscardSamples:47` (`decoderTrimBasis:"ffmpeg"`). These reference counts
are not a claim that every embedded decoder has the same internal delay. Do not
trim twice if the decoder already honors gapless metadata. Ignoring these fields
introduces leading delay and drift at transitions. Accurate random access also needs reservoir preroll:
seek earlier, decode and discard to the requested sample, or decode forward from
the start. Verify these on the actual ESP decoder before claiming timeline parity.

## Live radio

Request `kind:"radioLive"`, an owned station ID, and the hint/profile. Response
is 200 with MP3 encoding fields, playbackId, null length/hash/representationId and
supportsRanges false. Open without Range. HEAD is 405; Range is 422. Live MP3
contains no ID3 or ICY metadata; consume metadata separately from authenticated
`/api/v1/radio/events?playback=<playback-id>`. Existing station URL policy applies.

Two live transcodes globally, one per account; finite preparation has separate
workers. Disconnect frees the slot; duplicate actual GET is 409 and account/global
capacity exhaustion is 429. On EOF/error reconnect using the same unexpired lease
with 1–30 second exponential backoff; there is no byte resume or seamless server
reconnect. A 15-second origin idle/write stall ends the attempt. Revocation is
checked every second. Use recorded segments for offline radio.

## Reproduce the packaged evidence

```sh
docker build --target production -t weazltunes:weazlpod-review .
scripts/verify-weazlpod.sh weazltunes:weazlpod-review
# Optional intentional regeneration of synthetic fixture media and DTOs:
scripts/generate-weazlpod-fixtures.sh weazltunes:weazlpod-review
(cd fixtures/v1/media && sha256sum -c SHA256SUMS)
```

The verifier compiles repository tests and executes them against the runtime
image's actual ffmpeg/ffprobe as a nonroot user, offline, read-only root, 8 MiB
`/tmp`, and disposable host-backed data. It never uses production accounts.

[Sample MP3](../fixtures/v1/media/mp3-128-stereo.mp3): **81,083 bytes**, SHA-256
`21491ed489b84cd40167a8125b119651fdb01f0e81ffe26459ccd7958ccc6a57`;
5,016 ms, 221,184 decoded stereo frames at 44,100 Hz. The file stores 576/576
encoder delay/padding; reference FFmpeg trimming removes 1,105 leading samples
and 47 trailing samples, including its 529-sample decoder delay. Source is the existing
synthetic AAC fixture; source hash and packaged encoder fingerprint are in
[representation receipt](../fixtures/v1/weazlpod-representation.json). Fixture
IDs/URLs are inert; regeneration is deterministic for the same packaged toolchain.

Pending physical checks: ESP32 decoder format detection, audible playback, SD
throughput/storage, trimming/preroll, start/middle/end seeking, uninterrupted
segment transitions, radio reconnect/UI and power/resource behavior. Linux tests
are not hardware acceptance. No production deployment was performed.
