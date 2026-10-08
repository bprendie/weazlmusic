# Flight Recorder iPhone sync — 2026-10-08

New server captures use AAC-LC at 128 kbps, 44.1 kHz stereo. This reduces nominal
audio bytes by 20% from 160 kbps: six stations over six hours are about 2.07 GB
before packaging. Existing files, manifests and checksums remain unchanged.
Reservations now use 32,000 bytes/second/station (twice nominal); the independent
upstream input guard remains 80,000 bytes/second so lowering the output bitrate
does not reject higher-bitrate radio sources. The capability advertises 128.
The API schema and revision remain compatible; current encoding describes new
captures, not previously finalized assets. The existing 160 kbps fixture remains
intentional backward-compatibility evidence.

`go test ./...` passes, including real AAC and VBR MP3 input conversion. The encoder
test now checks ffprobe's measured output bitrate (115–141 kbps tolerance around
128), codec/rate/channels, seeking and decode, plus rejection of incomplete media.
The 2026-10-03 six-hour soak used 160 kbps; it is historical evidence, not a claim
that another six-hour run has been performed at 128 kbps.

## Findings from the native checkout

Read-only review of `~/Code/iOS/subweazl_app` found these optimization candidates.
These are source-level findings; phone throughput has not been benchmarked in
this pass, and native code has not been changed.

1. `Subweazl/App/AppModel.swift:downloadFlight` awaits a separate lease request for
   every segment. Six hours × six stations at 30-second segmentation is roughly
   4,320 files. At an illustrative 100 ms per lease round trip, serial lease
   acquisition alone represents about 7.2 minutes, although background transfers
   can overlap that work. The custom-header fallback additionally awaits each
   full `pipeline.save` before advancing.
2. `Subweazl/Platform/BackgroundDownloads.swift` explicitly permits two active
   tasks and configures two connections per host. This is the application's
   policy, not a claim about an iOS limit. Benchmark a bounded pool of four to
   six concurrent transfers on Wi-Fi, including the custom-header path, and
   retain cancellation, network policy and bounded memory.
3. Completed downloads are counted when calculating missing bytes, but the queue
   loop still resolves a lease for every segment before the download manager
   skips completed files. Filter verified complete files before requesting URLs;
   reuse immutable identity/hash and resume partial coverage.
4. Native `DownloadManager` repeatedly enumerates local files during capacity
   checks and promotion. Progress callbacks also persist and refresh UI for
   each update. With thousands of assets, profile these costs and use maintained
   byte totals/indexes plus coalesced progress, without weakening quota or hash
   validation. The server also decrypts/deserializes the full job under its lock
   for individual lease and media authorization; profile this before increasing
   concurrency, and retain authoritative ownership/deletion checks in any index.

## Recommended order

First add a bounded batch-lease endpoint and a bounded native transfer pool;
request URLs only for missing assets. Keep each URL scoped and revocable and
verify every downloaded asset against its manifest. Existing Range/If-Range
support already provides the server side of resumable transfers.

Then consider immutable five-minute **download bundles**. This could reduce
about 4,320 transfers to 432 without changing the 30-second capture format or
AAC encoding. Packages should be prepared/reused server-side, downloaded with
background URLSession and Range support, and unpacked into the same individually
verified assets. AAC is already compressed; ZIP compression is not the intended
saving. Bundles reduce request, task, filesystem and bookkeeping overhead. A new
bundle endpoint/manifest requires a coordinated versioned native contract.

Use the verified home endpoint when available to avoid an unnecessary remote
route. Benchmark on a real iPhone with the same recording: elapsed time to full
verified offline readiness, bytes/sec, lease time, active task count, CPU,
checksum time, disk work, retry/relaunch behavior and network conditions. Do not
claim a particular speedup from concurrency or bundling before this measurement.

## Production receipt

Deployed source `ca8ddab` on 2026-10-08, after checking that no capture was active
both before the image build and before stopping the service. Access used the
jumpbox's LAN name `jumpbox.teralab.local` with its previously trusted
`jumpbox.prendie.io` host-key alias, then the usual nested production SSH hop.

Runtime image:
`sha256:0ec89af7ed4aabfb577ca504bccd51ccb06e371bb6f5b3aefa15464cd841facc`.
Container healthy with zero restarts; health/root HTTP checks passed, anonymous
native identity remained 401, and installation info/key digest matched before
and after. The original data volume and recordings were retained.

Full-volume backup and rollback receipts:
`/home/bobp/weazlmusic-backups/20261008T130843Z` (directory0700, archive0600,
operator-owned). Verified `data.tar.gz` SHA-256:
`91e0c0c2ba8a3826c1d83cd2df176ea166c27587586183173a2514bb453cc51e`.
Rollback image: `weazltunes-web:rollback-20261008T130843Z`; override YAML and old
source/image receipts are in the backup directory. Documentation-only follow-up
commits advance the checkout without another runtime restart.
