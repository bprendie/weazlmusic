#!/usr/bin/env bash
# Isolated tests using the exact runtime image's packaged encoder. No account traffic.
set -euo pipefail
cd "$(dirname "$0")/.."
image=${1:-weazltunes:weazlpod-review}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
CGO_ENABLED=0 go test -c -o "$work/server.test" ./internal/server
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,nosuid,size=8m --env TMPDIR=/work \
  --mount "type=bind,src=$work,dst=/work" \
  --mount "type=bind,src=$PWD,dst=/src,readonly" \
  --workdir /src/internal/server --entrypoint /work/server.test \
  "$image" -test.run 'TestPod|TestCheckedInV1ContractFixtures' -test.v
