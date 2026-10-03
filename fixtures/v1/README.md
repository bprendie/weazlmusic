# Native contract fixtures — 2026-10-03.2

JSON uses actual fixture-server DTOs except inert credential/lease values and
illustrative error/Mood events. Files are synthetic; no production library or
credentials are included. Arrays and null optionals match the server contract.
`manifest-partial.json` is a real 60-second six-stream capture with initial,
disconnect and stall gaps. Its full assets can be reproduced with the client;
only one short sample is checked in. `manifest-complete.json` wraps that sample
in a synthetic one-track timeline with exact duration and no gaps. This complete
sample is a DTO/player fixture, not evidence of a gap-free overnight recording.

`media/aac-lc-160k-stereo.m4a` is FDK AAC-LC 160 kbps, 44.1 kHz, stereo.
Its size and SHA-256 match the complete manifest and `media/SHA256SUMS`.
Verify with `cd fixtures/v1/media && sha256sum -c SHA256SUMS`.
The checked-in manifests intentionally contain no expiring URLs; obtain runtime
leases for runtime asset IDs. Static sample-asset is not a runtime server ID.

Equivalent request/response tests are in `internal/server/v1*_test.go` and
`recorder*_test.go`, including fixture shape/coverage/sample integrity validation.
These tests replace a separate OpenAPI generator for this release. Run
`go test -race ./...` and follow `docs/NATIVE_HANDOFF.md` for a runnable server.
The live exporter `scripts/generate-v1-fixtures.py` refuses non-fixture stations.
It changes only isolated fixture playlists/favorites/schedules. Use a fresh
fixture volume when regenerating. Apple media acceptance remains pending.
