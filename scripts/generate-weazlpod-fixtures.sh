#!/usr/bin/env bash
# Regenerate only synthetic fixture artifacts with the packaged production encoder.
set -euo pipefail
cd "$(dirname "$0")/.."
image=${1:-weazltunes:weazlpod-review}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
CGO_ENABLED=0 go test -c -o "$work/server.test" ./internal/server
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,nosuid,size=8m --env TMPDIR=/work \
  --env WEAZLPOD_EXPORT_FIXTURE=/out \
  --mount "type=bind,src=$work,dst=/work" \
  --mount "type=bind,src=$PWD,dst=/src,readonly" \
  --mount "type=bind,src=$PWD/fixtures/v1,dst=/out" \
  --workdir /src/internal/server --entrypoint /work/server.test \
  "$image" -test.run '^TestPodContractFixture$' -test.v
(cd fixtures/v1/media && sha256sum aac-lc-160k-stereo.m4a mp3-128-stereo.mp3 > SHA256SUMS)
