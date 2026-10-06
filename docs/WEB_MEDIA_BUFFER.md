# Flight Recorder web buffering — 2026-10-06

The web client defaults to five minutes of compressed AAC ahead of the playhead.
It refills to five minutes when less than four minutes remain. The buffer selector
persists 1, 3, 5 or 10 minutes per browser; other choices use the same 80% refill
threshold. Only the selected station is prefetched. Five minutes at 160 kbps is
about 6 MB of audio, plus MP4/browser overhead. Old audio is evicted behind the
playhead; recordings are not decoded into a five-minute PCM array or downloaded
in full. Stop, station changes and seeking cancel obsolete requests and release
media URLs. Network failures retry with a three-second delay.

Independent M4A files are fragmented in the browser and appended to one Media
Source Extensions AAC source buffer. Segment boundaries do not change the audio
URL, pause the element, or seek forward. The player reads the actual audio clock,
which stops during buffering. Genuine capture gaps retain their original shared
session timing. Existing recordings and the server's AAC profile need no changes.
The server CSP permits `blob:` only for media to support the browser-owned buffer.

Browsers with AAC MediaSource (or ManagedMediaSource) use the continuous path.
Browsers without it prefetch the same larger window into bounded Blob URLs and
use a compatibility path that can still have a short handoff between files. No
physical Apple-device or locked-screen acceptance is claimed by these desktop
Chromium tests. The buffer selector and buffered-duration display are in the
recorder playback controls.

## Dependency

`web/mp4box.js` vendors [GPAC MP4Box.js](https://github.com/gpac/mp4box.js) version
2.4.1 under BSD-3-Clause. Its full license is retained in the file header. It
repackages the existing AAC without re-encoding. Rebuild the checked-in ES module
with `scripts/vendor-mp4box.sh` (mp4box 2.4.1, esbuild 0.28.2). Normal Go/container
builds need neither npm nor a CDN. The module loads only for recorder playback.

## Verification

- Existing production Freestyle recording inspected read-only. Its four-hour
  manifest has 482 segments, two substantive gaps (343 ms and 3,263 ms), and 87
  one-millisecond rounding gaps. Four consecutive initial M4A files were copied
  locally, hash-verified and decoded. No recurring silence of at least 50 ms was
  found in that sample, apart from the initial 91 ms of startup silence.
- Before this change, real Chromium playback of those files skipped 214–256 ms
  into each new segment; injecting 150 ms of lease latency increased that to
  363–367 ms. The old wall clock continued through the file reload. The new
  continuous player crosses the same boundaries without pause, reload or seek.
  Local diagnostic artifacts are ignored under
  `test-results/freestyle-investigation/`; no real station audio is committed.
- `tests/recorder-buffer.cjs` uses checked-in synthetic AAC with the real browser
  decoder. It verifies five-minute initial fill, no refill above four minutes,
  refill below that threshold, uninterrupted buffered playback during network
  failure, clock freeze and recovery after starvation, bounded buffering,
  pause/seek/station selection, genuine gap traversal, compatibility playback,
  saved buffer preference and cancellation of pending requests on stop.
- The existing recorder browser flow covers real capture, playback, footer
  controls, station changes, navigation, multiple recordings, deletion and mobile
  layout. The library browser suite and Go tests also pass.

The earlier six-hour soak validated server capture and media integrity. It did
not test continuous browser playback across segment boundaries. This follow-up
adds that missing browser coverage; it is not a new six-hour browser soak.

## Production deployment

Deployed source `37899427d39de9b8e70a10e152cb0876b6972961` through the jumpbox on
2026-10-06. No production capture was active. The new image built before stopping
the service for a full-volume backup and container replacement.

- Runtime image:
  `sha256:28086a0f538d3ae0e08858fb20ee3597aeb219866cccd9e9a6387a058f67fd58`.
  Container healthy, zero restarts; health endpoint passed and anonymous native
  identity access remained 401. Served buffering code, the exact vendored module
  and the media CSP were verified over HTTP.
- Installation info and encryption-key digest matched before/after. The existing
  recording volume was retained; no production recording or library mutation was
  used as a playback test.
- Backup directory: `/home/bobp/weazlmusic-backups/20261006T130556Z` (mode0700).
  Full archive `data.tar.gz` is operator-owned mode0600, with verified SHA-256
  `951dc3b900a7fc0287e0d44d75c88cc7bffd14c89b91258683b770ce85e6f9eb`.
- Rollback image: `weazltunes-web:rollback-20261006T130556Z`. Its Compose override,
  prior image/source, key digests and deployment receipts are in that directory.

Documentation-only receipt commits advance the production checkout without
rebuilding or restarting this tested runtime image.
