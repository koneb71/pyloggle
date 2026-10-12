#!/usr/bin/env bash
# Fails when the tree holds what a public repository must not: caches or dependencies,
# machine-specific paths, live-looking keys, oversized files, two different LICENSE copies, or
# broken relative links in the top-level Markdown files.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
fail=0
bad() { echo "check-tree: $*" >&2; fail=1; }

# Caches, virtualenvs and build output.
if git ls-files | grep -E '(^|/)(node_modules|\.venv|venv|__pycache__|\.gocache|\.gomodcache|\.gopath)/|(^|/)go/pkg/mod/'; then
  bad "caches, virtualenvs or dependencies are tracked"
fi
if git ls-files web/dist | grep -v '^web/dist/\.gitkeep$'; then
  bad "web/dist build output is tracked"
fi

# Machine-specific paths and live-looking keys (the lists that define them are excluded).
pattern='/home/user|/tmp/claude|/Users/[A-Za-z]|/opt/pw-browsers|sk_live_|fc-v114'
if git grep -nIE "$pattern" -- . ':!scripts/check-tree.sh' ':!python-sdk/tests/gen_golden.py'; then
  bad "machine-specific paths or live-looking keys (above)"
fi

# Size budget: 1 MB per file, 400 KB per image.
while IFS= read -r f; do
  [ -f "$f" ] || continue
  size=$(wc -c <"$f")
  case "$f" in *.png | *.jpg | *.jpeg | *.gif) limit=409600 ;; *) limit=1048576 ;; esac
  [ "$size" -le "$limit" ] || bad "$f is $size bytes (limit $limit)"
done < <(git ls-files)

# The SDK ships its own copy of the license.
cmp -s LICENSE python-sdk/LICENSE || bad "python-sdk/LICENSE differs from LICENSE"

# Relative links in the top-level Markdown files must point at files that exist.
python3 - <<'PY' || fail=1
import os, re, sys
broken = []
for md in sorted(f for f in os.listdir(".") if f.endswith(".md")):
    text = open(md, encoding="utf-8").read()
    text = re.sub(r"```.*?```", "", text, flags=re.S)
    for target in re.findall(r"\]\(([^)\s]+)\)", text):
        if re.match(r"^(https?:|mailto:|#)", target):
            continue
        path = target.split("#", 1)[0]
        if path and not os.path.exists(path):
            broken.append(f"{md}: {target}")
for b in broken:
    print("check-tree: broken link " + b, file=sys.stderr)
sys.exit(1 if broken else 0)
PY

[ "$fail" -eq 0 ] && echo "check-tree: ok"
exit "$fail"
