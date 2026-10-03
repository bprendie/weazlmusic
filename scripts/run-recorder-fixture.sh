#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
WEAZL_FIXTURE_DIR=$(mktemp -d)
WEAZL_FIXTURE_PID=
cleanup() {
  if [[ -n "$WEAZL_FIXTURE_PID" ]]; then
    kill "$WEAZL_FIXTURE_PID" 2>/dev/null || true
    wait "$WEAZL_FIXTURE_PID" 2>/dev/null || true
  fi
  rm -rf "$WEAZL_FIXTURE_DIR"
}
trap cleanup EXIT
go build -o "$WEAZL_FIXTURE_DIR/weazlfixture" ./cmd/weazlfixture
DATA_DIR="$WEAZL_FIXTURE_DIR/data" "$WEAZL_FIXTURE_DIR/weazlfixture" >"$WEAZL_FIXTURE_DIR/fixture.log" 2>&1 &
WEAZL_FIXTURE_PID=$!
for attempt in $(seq 1 100); do
  kill -0 "$WEAZL_FIXTURE_PID"
  if curl -fsS http://127.0.0.1:4003/api/v1/info >/dev/null 2>&1 && rg -q 'Fixture ready' "$WEAZL_FIXTURE_DIR/fixture.log"; then break; fi
  sleep 0.2
done
python3 scripts/recorder-client.py --duration "${1:-60}" --output "${2:-test-results/recorder}"
