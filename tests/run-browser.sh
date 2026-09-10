#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
WEAZL_TEST_DIR=$(mktemp -d)
fixture_pid=
app_pid=
cleanup() {
  [[ -z "$app_pid" ]] || kill "$app_pid" 2>/dev/null || true
  [[ -z "$fixture_pid" ]] || kill "$fixture_pid" 2>/dev/null || true
  rm -rf "$WEAZL_TEST_DIR"
}
trap cleanup EXIT
go build -o "$WEAZL_TEST_DIR/testnav" ./cmd/testnav
go build -o "$WEAZL_TEST_DIR/weazltunes" ./cmd/weazltunes
"$WEAZL_TEST_DIR/testnav" >"$WEAZL_TEST_DIR/nav.log" 2>&1 &
fixture_pid=$!
LISTEN_ADDR=127.0.0.1:4002 DATA_DIR="$WEAZL_TEST_DIR/data" "$WEAZL_TEST_DIR/weazltunes" >"$WEAZL_TEST_DIR/app.log" 2>&1 &
app_pid=$!
for attempt in $(seq 1 40); do
  kill -0 "$fixture_pid" "$app_pid"
  if curl -fsS http://127.0.0.1:4002/healthz >/dev/null; then break; fi
  sleep 0.1
done
node tests/smoke.cjs
