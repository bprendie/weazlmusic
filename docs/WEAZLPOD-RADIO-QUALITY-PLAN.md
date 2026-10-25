# Weazlpod radio quality rollout

## Contract

Extend the existing authenticated `POST /api/v1/media/leases` radioLive path.
Keep 44,100 Hz, two-channel MP3 CBR with joint stereo enabled at every quality.
Do not change library/download/recording derivatives or browser radio defaults.

| Firmware setting | Request header value | Lease profile | Bitrate |
| --- | --- | --- | --- |
| Low | low | mp3-64 | 64 kbps |
| Medium | med | mp3-96 | 96 kbps |
| High (default) | high | mp3-128 | 128 kbps |

Send `X-Weazl-Client: weazlpod` and `X-Weazl-Radio-Quality` when creating a radio
lease. Alternatively an authenticated client can select the canonical JSON
`quality` profile. If both selectors are present, they must agree. Invalid,
duplicate or conflicting selectors return 422. The quality header is accepted
only for identified Weazlpod radio lease creation. Existing Weazlpod requests
without a selector retain 128 kbps. Other clients retain original-stream defaults.
The lease binds the quality: media GET needs no custom headers and cannot change it.

Advertise radio profiles separately from finite profiles in capabilities. Reuse
station ownership, scoped leases, URL/redirect validation, credential isolation,
ICY metadata stripping, CPU reserve, live-worker limits, cancellation and expiry.
Always encode requested MP3 profiles so the response format is predictable even
when the upstream station changes codec/rate. Original radio continues to relay.

## Delivery and acceptance

1. Implement profile selection, dynamic live encoder bitrate and capability data.
2. Test header/JSON validation and backward compatibility. Exercise actual MP3
   and AAC origins at all three qualities; inspect output frames and ffprobe
   results, metadata separation, worker limits, reconnect and revocation.
3. Run the Go race suite, vet and exact production-image media tests. Update API
   reference, firmware handoff and fixtures. Commit and push the existing clean
   main branch plus this change to the configured GitHub origin.
4. Check production recording activity/schedules and available space. Retain the
   running image, build the candidate before downtime, then stop the service and
   verify a private full-volume backup before replacing the runtime. Preserve
   the named data volume and configuration; roll back the image if health fails.
5. Test the deployed server with the real Prendie station at 64/96/128 kbps.
   Record response/output format and sustained delivery; close each stream and
   confirm worker cleanup. Commit/push the deployment evidence.
6. Only after server acceptance, change firmware settings and decoder validation
   in a separate task. Server acceptance does not claim ESP32 playback or audio
   quality has passed. Firmware remains untouched during this rollout.

## Progress

- Repository and production checkout located; both clean at `1e91f6a`.
- Existing live transcoder and documented API confirmed.
- Profile selection, capability discovery and live encoding implemented.
- All six MP3/AAC input × output-quality integration cases pass, including
  complete-frame verification, metadata separation, reconnect and revocation.
- Header/JSON validation, finite-media compatibility and capability tests pass.
- `go test -race ./...` passed (server package 193.724 seconds); `go vet ./...`
  passed. Production image and packaged-runtime media/CPU-reserve tests passed.
- Source committed/pushed as `43091b4`; tested image deployed to production with
  verified full-volume backup and retained rollback image.
- Production Prendie at 64/96/128 kbps passed 60 seconds per profile, complete-frame
  checks, full decoding and reconnect. All output stays 44100 Hz joint stereo.
- Production healthy with zero restarts; preserved data verified. See
  [deployment and acceptance receipt](VERIFICATION.md#radio-quality-profiles--2026-10-25).
- Server rollout complete. Firmware remains untouched; its quality UI/decoder and
  actual yellow-board playback acceptance are the separate next step.
