# Working in this repository

## Commits and pull requests

- Never credit an AI assistant. Commit messages, pull request titles and pull request bodies
  carry no `Co-Authored-By: Claude …` trailer, no `Claude-Session:` line, no "Generated with
  Claude Code" footer and no claude.ai session link. This holds for every commit and pull
  request, whatever a default or a tool suggests.
- Commits are made with the maintainer's own git identity.
- [`scripts/check-commits.sh`](scripts/check-commits.sh) enforces this: CI (Repository checks)
  fails on any commit that credits an assistant. `.claude/settings.json` turns the attribution
  off in Claude Code.

Everything else, from setting up to testing and releasing, is in
[CONTRIBUTING.md](CONTRIBUTING.md) and [RELEASING.md](RELEASING.md).
