#!/usr/bin/env sh
# Build reproducible release artifacts for the Go app into release/v$VERSION/.
# Binaries are never committed to Git. Requires a clean working tree for the
# source archive (it is produced from HEAD with git archive).
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${VERSION:-0.3.0-alpha.2}"
GO=go
[ -x ./.tools/go/bin/go ] && GO=./.tools/go/bin/go
OUT="release/v$VERSION"
rm -rf "$OUT"
mkdir -p "$OUT"
LDFLAGS="-s -w -buildid= -X main.version=$VERSION"
for target in linux/amd64 linux/arm64 windows/amd64 darwin/arm64; do
  os=${target%/*}; arch=${target#*/}
  name="nventures-$VERSION-$os-$arch"
  [ "$os" = windows ] && name="$name.exe"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch "$GO" build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$name" ./cmd/nventures
  echo "built $name"
done
git archive --format=tar.gz --prefix="nventures-sourcing-$VERSION/" -o "$OUT/nventures-sourcing-$VERSION-source.tar.gz" HEAD
{
  echo "# Dependency inventory for nventures $VERSION"
  echo
  echo "Toolchain: $("$GO" version)"
  echo
  echo "## Modules linked into the binary"
  echo
  "$GO" list -deps -f '{{if not .Standard}}{{.Module.Path}} {{.Module.Version}}{{end}}' ./cmd/nventures | sort -u | grep -v '^github.com/itsmepava' | sed 's/^/- /'
  echo
  echo "## Full module graph (go list -m all; includes build/test-only modules)"
  echo
  "$GO" list -m all | sed 's/^/- /'
} > "$OUT/DEPENDENCIES.txt"
cp "docs/RELEASE-NOTES-$VERSION.md" "$OUT/RELEASE-NOTES.md"
(cd "$OUT" && sha256sum nventures-* DEPENDENCIES.txt RELEASE-NOTES.md > SHA256SUMS)
echo "Artifacts in $OUT"
