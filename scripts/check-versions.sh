#!/usr/bin/env bash
# The SDK, the web app and the CHANGELOG must agree on the version.
#   scripts/check-versions.sh            # during development ("## Unreleased" may come first)
#   scripts/check-versions.sh 1.0.0      # for a release tag: everything must say 1.0.0
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
tag="${1:-}"
tag="${tag#v}"
sdk=$(sed -n 's/^__version__ = "\(.*\)"/\1/p' python-sdk/pyloggle/_version.py)
web=$(sed -n 's/^  "version": "\(.*\)",/\1/p' web/package.json | head -1)
first=$(grep -m1 -E '^## ' CHANGELOG.md | sed 's/^## //; s/ .*//')
fail=0
[ "$sdk" = "$web" ] || { echo "check-versions: SDK $sdk but web $web" >&2; fail=1; }
if [ -n "$tag" ]; then
  [ "$sdk" = "$tag" ] || { echo "check-versions: tag $tag but SDK $sdk" >&2; fail=1; }
  [ "$first" = "$tag" ] || { echo "check-versions: tag $tag but the CHANGELOG starts with \"$first\"" >&2; fail=1; }
else
  [ "$first" = "$sdk" ] || [ "$first" = "Unreleased" ] || { echo "check-versions: CHANGELOG starts with \"$first\", expected $sdk or Unreleased" >&2; fail=1; }
fi
[ "$fail" -eq 0 ] && echo "check-versions: ok ($sdk)"
exit "$fail"
