# Ten-minute recording assets — 2026-10-08

Capture continues to checkpoint approximately every 30 seconds. After capture
workers stop, finalization stream-copies contiguous AAC into approximately
ten-minute fast-start M4A files. Encoding stays FDK AAC-LC, 128 kbps, 44.1 kHz,
stereo. There is no second lossy encode. Ten minutes is about 9.6 MB; six stations
for six hours normally yield about 216 assets instead of 4,320. Request overhead
falls, but actual iPhone sync throughput has not been benchmarked.

Groups allow one-millisecond capture rounding seams and up to 25 milliseconds
past the ten-minute target for AAC packet alignment. Real gaps, overlaps and
codec changes split groups; final tails are shorter. Shared timeline positions,
station switching, pause and seeking use the existing manifest fields. Existing
finalized recordings are never rewritten. API revision remains 2026-10-03.2.

## Publication and recovery

Packaging runs outside the API lock after capture workers release stream slots.
Source checkpoints remain available until a single transaction publishes the
replacement assets, final manifest, job state and durable cleanup journal.
Old files are unlinked in bounded batches after that commit. Interrupted cleanup
retries safely; interrupted finalization resumes from durable capture state.

Each merge checks the disk-free reserve and validates output duration, size and
checksum before durable publication. A failed merge retains its original short
assets with a station warning, preserving usable audio. Shutdown cancellation
leaves the job available for recovery. Capture data is never deleted before the
replacement manifest commits. Temporary space includes replacement files while
source files still exist; insufficient reserve can leave some groups unmerged.

The web client reuses cached audio when seeking within a chunk. Buffer targets
still default to five minutes, refilling below four, but whole-file downloads
can overshoot the target by a chunk. Actual capture gaps retain their timeline;
network starvation freezes the audio clock.

## Verification

Passed locally:

- `go test -race ./...` and `go vet ./...`.
- Real AAC compaction: twenty capture files become a ten-minute file plus a
  separate tail; packet hashes match every source AAC packet. Decode and seeking
  work around former boundaries and near the end.
- Injected manifest transaction failure retains original assets and files;
  retry publishes deterministic replacement IDs; journal cleanup is bounded.
  Real gaps, deletion and reserve-pressure fallback are covered.
- `tests/recorder-chunks.cjs`: real ten-minute AAC playback across old and new
  boundaries, cached seeks without another lease, pause, station timeline,
  stop, mobile layout and no browser errors.
- `tests/recorder-buffer.cjs`: refill thresholds, outage playback, starvation
  recovery, real gaps, fallback playback, preferences and cancellation.
- `tests/recorder.cjs` against the local six-stream fixture, and
  `tests/run-browser.sh` for general browser regression.

Generate the ignored real-media browser fixture with:

```sh
RECORDER_COMPACT_ARTIFACT_DIR="$PWD/test-results/ten-minute" go test ./internal/server -run 'TestRecorder.*Compaction' -count=1
PLAYWRIGHT_MODULE=/path/to/node_modules/playwright node tests/recorder-chunks.cjs
```

The synthetic fixture retains the previous 160 kbps profile to exercise existing
media compatibility. Native source inspection confirms manifest timing is used;
a real iPhone sync benchmark and another six-hour soak were not performed for
this change. Production checks are read-only and do not create/delete recordings.

## Production receipt

Source `01630d1` deployed on 2026-10-08 to `weazlmusic.teralab.local` through
`jumpbox.teralab.local`, verifying the trusted `jumpbox.prendie.io` host-key alias.
The misspelled `weazlmuisic` name did not resolve. No capture was active before
build or immediately before stopping the service.

Runtime image:
`sha256:f8d5e1c09a782c0c59f27fe2b23746bacced722559a2cb18a44ce07e42a88260`.
Container healthy, zero restarts; health and root HTTP checks passed, anonymous
native identity returned 401, and installation info and key digest were unchanged.
Existing data and recordings were retained.

Verified full-volume backup: `/home/bobp/weazlmusic-backups/20261008T135035Z`
(directory0700, archive0600, operator-owned). Archive SHA-256:
`2065ebce1f3573fa2f85fc7ca2e6ba2c65f2cc8b47fab4d3dc648ac9fc7850da`.
Rollback image: `weazltunes-web:rollback-20261008T135035Z`; the directory contains
the compose override, old image/source receipts, build log and health evidence.
Documentation-only receipt commits do not require another runtime restart.
