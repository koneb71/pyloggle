#!/usr/bin/env bash
# Fails when a commit in the range credits an AI assistant: a Co-Authored-By trailer, a session
# link or a "Generated with" footer in its message, or an assistant as its author or committer.
# Commits here are the maintainer's alone (CLAUDE.md, CONTRIBUTING.md).
#
#   scripts/check-commits.sh                    # what this branch adds to origin/main
#   scripts/check-commits.sh origin/main        # every commit on main
#   scripts/check-commits.sh BASE..HEAD         # any range git rev-list accepts
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
range="${1:-origin/main..HEAD}"
message='^co-authored-by:.*(claude|anthropic)|^claude-session:|generated with \[?claude code|claude\.ai/code/session_|noreply@anthropic\.com'
person='(^|[^a-z])claude([^a-z]|$)|anthropic'
fail=0
count=0
for c in $(git rev-list "$range"); do
  count=$((count + 1))
  if git log -1 --format=%B "$c" | grep -qiE "$message" ||
    git log -1 --format='%an <%ae>%n%cn <%ce>' "$c" | grep -qiE "$person"; then
    echo "check-commits: $(git log -1 --format='%h %s' "$c") credits an AI assistant" >&2
    fail=1
  fi
done
[ "$fail" -eq 0 ] && echo "check-commits: ok ($count commits in $range)"
exit "$fail"
