#!/usr/bin/env bash
# Isolated tests using the exact runtime image's packaged encoder. No account traffic.
set -euo pipefail
cd "$(dirname "$0")/.."
image=${1:-weazltunes:weazlpod-review}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
CGO_ENABLED=0 go test -c -o "$work/server.test" ./internal/server
common=(--rm --network none --read-only --cap-drop ALL
  --security-opt no-new-privileges --user "$(id -u):$(id -g)"
  --tmpfs /tmp:rw,nosuid,size=8m --env TMPDIR=/work
  --mount "type=bind,src=$work,dst=/work"
  --mount "type=bind,src=$PWD,dst=/src,readonly"
  --workdir /src/internal/server --entrypoint /work/server.test)
docker run "${common[@]}" "$image" \
  -test.run 'TestMediaCPU|TestMediaSubprocess|TestMediaWorker|TestMediaAutomatic|TestPod|TestCheckedInV1ContractFixtures' -test.v
# Select four distinct physical cores from the caller's actual affinity mask.
four=$(python3 - <<'PY'
import os
from pathlib import Path
seen=set();cpus=[]
for cpu in sorted(os.sched_getaffinity(0)):
    p=Path(f'/sys/devices/system/cpu/cpu{cpu}/topology')
    try: key=((p/'physical_package_id').read_text(),(p/'core_id').read_text())
    except OSError: key=cpu
    if key not in seen: seen.add(key); cpus.append(cpu)
    if len(cpus)==4: break
if len(cpus)==4: print(','.join(map(str,cpus)))
PY
)
if [[ -n "$four" ]]; then
  docker run "${common[@]}" --cpuset-cpus "$four" --cpus 4 "$image" \
    -test.run '^TestMediaSubprocessAffinityAndCancellation$' -test.v
  docker run "${common[@]}" --cpuset-cpus "$four" --cpus 2.9 \
    --env WEAZL_TEST_MEDIA_DISABLED=1 "$image" -test.run '^TestMediaNoCPUHeadroom$' -test.v
  two=$(cut -d, -f1,2 <<<"$four")
  docker run "${common[@]}" --cpuset-cpus "$two" --cpus 2 \
    --env WEAZL_TEST_MEDIA_DISABLED=1 "$image" -test.run '^TestMediaNoCPUHeadroom$' -test.v
else
  echo 'Quota matrix skipped: requires four distinct available cores.'
fi
