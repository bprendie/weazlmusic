#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
WEAZL_VENDOR_DIR=$(mktemp -d)
trap 'rm -rf "$WEAZL_VENDOR_DIR"' EXIT
npm install --prefix "$WEAZL_VENDOR_DIR" --no-audit --no-fund mp4box@2.4.1 esbuild@0.28.2
"$WEAZL_VENDOR_DIR/node_modules/.bin/esbuild" \
  "$WEAZL_VENDOR_DIR/node_modules/mp4box/dist/mp4box.all.mjs" \
  --bundle --minify --format=esm --outfile="$WEAZL_VENDOR_DIR/bundle.js"
python3 - "$WEAZL_VENDOR_DIR" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
license = (root / 'node_modules/mp4box/LICENSE').read_text()
pathlib.Path('web/mp4box.js').write_text('/* Vendored mp4box 2.4.1 (BSD-3-Clause).\n' + license + '*/\n' + (root / 'bundle.js').read_text())
PY
