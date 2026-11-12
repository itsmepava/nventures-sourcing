#!/usr/bin/env sh
# Start the nVentures Go app (one local process, embedded UI, SQLite).
# Uses ./bin/nventures if present; otherwise builds it with the local Go
# toolchain in .tools/go (no system packages are changed).
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BIN=./bin/nventures
if [ ! -x "$BIN" ]; then
  GO=go
  if [ -x ./.tools/go/bin/go ]; then GO=./.tools/go/bin/go; fi
  if ! command -v "$GO" >/dev/null 2>&1; then
    echo "nventures binary not found and Go is not installed. Run: python3 scripts/bootstrap_go.py" >&2
    exit 1
  fi
  echo "Building nventures (first run)..."
  CGO_ENABLED=0 "$GO" build -trimpath -o "$BIN" ./cmd/nventures
fi
ENV_FILE=""
[ -f ./.env ] && ENV_FILE=./.env
exec "$BIN" serve --env "$ENV_FILE" "$@"
