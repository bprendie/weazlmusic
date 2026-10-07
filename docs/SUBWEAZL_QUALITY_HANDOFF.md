# S04: server quality delivery dependency

Native execution checkpoint, 2026-10-07. This is a request to the server owner,
not a new API contract. Canonical contract remains `2026-10-03.2`; native and
server snapshots still match. Server inspected at `35a3eca`. No server deployment
or production traffic was performed.

`~/Code/web_weazltunes/internal/server/v1_media.go` currently rejects qualities
other than `original`. Original trackStream forwards an upstream stream request
without explicitly overriding upstream player defaults. Native cached original
playback therefore requests the existing `trackOriginal` lease, whose source is
upstream download. Do not promote an upstream default transcode as original.

Deliver S04 in the server repository, with its own tests/commit, before enabling
cellular AAC defaults. Publish a canonical revision and fixture bundle; update the
native exact-revision compatibility gate only with that evidence. Keep existing
original-only servers usable. The optional native profile DTOs are scaffolding;
they do not constitute delivery of this proposed contract.

Proposed profiles for the owner to finalize:

| Profile | Delivery | Cache/seek requirements |
| --- | --- | --- |
| Original | Original bytes; bypass upstream transcoding defaults | Stable validator, actual length when known; immutable byte ranges |
| AAC 128 | AAC-LC in M4A at a 128 kbps cap | Finalized verified file or immutable segments; full logical timeline |
| AAC 96 / 64 | Explicit selectable lower cellular caps | Same; advertise only installed/tested encoders |

The native range owner accepts immutable files: HEAD 200 with actual total length;
GET 206 with exact `Content-Range`; stable ETag with If-Range across renewed leases.
It rejects 200 fallbacks for a resumed range, changed validators, auth HTML,
truncation and checksum mismatch. Unknown validators cannot resume across process
restart. It does not consume arbitrary growing M4A files or assume audio
`timeOffset` support. A segmented transcode delivery requires a separately tested
native adapter, not relabeling this ranged-file adapter.

The delivered contract must define representation identity, source revision,
effective codec/quality when the source is already below the requested cap,
duration, encoder priming/seek origin, cacheability and final length/hash. Length
estimates and an encoder exit alone are not integrity evidence. Job admission,
concurrent consumer deduplication, user ownership, cancellation, retention and
quota must be bounded. Expiring URLs are credentials, not representation IDs.

Required isolated fixtures: original integrity despite upstream player defaults;
supported/unsupported profile; near-start/middle/end decode and seek; interrupted
encoding; rate limiting with Retry-After; lease renewal retaining identity;
concurrent consumers; two-user authorization; finalized output length/hash.
Return a server commit, source digest, canonical revision and reproducible command
that the Swift client can execute. Keep production and real library mutations out
of these checks.

Native consumption checklist after delivery:

- Update docs/WEAZLTUNES_API_V1.md, DTOs and exact-revision fixtures together.
- Select original on Wi-Fi, advertised AAC 128 on cellular for new preferences;
  retain an explicit original download setting and actual local quality labels.
- Use delivered representation identity in durable media keys and validators.
- Carry HTTP Retry-After through native lease errors; verify renewal/backoff.
- Verify AVPlayer's actual codec decoding, seeks, priming and offline replay on
  an iOS/iPadOS 27 device. Linux tests do not close this acceptance.
