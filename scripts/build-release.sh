#!/usr/bin/env bash
# Builds the release archives: the web UI once, then ingestd (UI embedded) for Linux and macOS
# on amd64 and arm64, each archive with the license files, plus SHA256SUMS.
#   scripts/build-release.sh 1.0.0 [outdir]      # SKIP_WEB_BUILD=1 reuses web/dist
set -euo pipefail
version="${1:?usage: scripts/build-release.sh VERSION [outdir]}"
version="${version#v}"
out="${2:-dist}"
cd "$(git rev-parse --show-toplevel)"
commit=$(git rev-parse --short HEAD 2>/dev/null || true)

if [ "${SKIP_WEB_BUILD:-}" != "1" ]; then
  (cd web && npm ci --no-audit --no-fund && npm run build)
fi
test -f web/dist/index.html || { echo "web/dist is empty: build the UI first" >&2; exit 1; }

rm -rf "$out"
mkdir -p "$out"
ldflags="-s -w -X github.com/koneb71/pyloggle/internal/version.Version=${version} -X github.com/koneb71/pyloggle/internal/version.Commit=${commit}"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  name="pyloggle_${version}_${os}_${arch}"
  mkdir -p "$out/$name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$out/$name/ingestd" ./cmd/ingestd
  cp LICENSE NOTICE THIRD_PARTY_LICENSES.md README.md "$out/$name/"
  tar -C "$out" -czf "$out/$name.tar.gz" "$name"
  rm -rf "${out:?}/$name"
done
(cd "$out" && if command -v sha256sum >/dev/null; then sha256sum ./*.tar.gz; else shasum -a 256 ./*.tar.gz; fi | sed 's# \./# #' >SHA256SUMS)
ls -l "$out"
