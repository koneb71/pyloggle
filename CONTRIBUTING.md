# Contributing to Pyloggle

Thank you for helping. Bug reports, fixes, docs and ideas are all welcome. This page explains
how the code is laid out, how to run it, and what a change needs before it can be merged.

- **Found a bug?** Open an issue with the [bug report form](https://github.com/koneb71/pyloggle/issues/new/choose).
- **Have an idea?** Open a feature request first if the change is large, so we can agree on
  the shape before you write it.
- **Found a security problem?** Do not open an issue. Follow [SECURITY.md](SECURITY.md).

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).

## What is where

| Folder | What it holds |
|---|---|
| `cmd/ingestd` | The server's entry point: one binary that receives data, stores it and serves the app |
| `internal/` | The server code (Go). `ingest` receives data, `storage/clickhouse` stores and queries it, `alerting` checks alert rules, `webui` is the app's API |
| `internal/storage/clickhouse/migrations` | The database schema, applied in order at startup |
| `web/` | The app (React, TypeScript, Tailwind), built into `web/dist` and embedded in the binary |
| `python-sdk/` | The Python package that sends errors, logs and requests to the server |
| `docs/` | The documentation site (VitePress), published at <https://koneb71.github.io/pyloggle/> |
| `deploy/` | The Dockerfile, the Compose files and the ClickHouse settings |
| `examples/` | Small apps that send data, for trying things out |
| `scripts/` | Release, license and repository checks |

## Set up

You need:

- Go 1.26
- Node.js 22
- Python 3.9 or newer
- Docker, for the local ClickHouse

Then, from the repository root:

```bash
make dev-db    # ClickHouse only, on 127.0.0.1:9000
make dev-api   # the server on http://localhost:8080, sign-in off
make dev-web   # the app with live reload on http://localhost:5173/ui/ (talks to :8080)
```

To send some data, run one of the [examples](examples/) or point an app at
`http://localhost:8080/v1/ingest`. `make help` lists every target.

## Run the tests

```bash
make test      # Go (vet + race), Python SDK and web (typecheck + unit tests)
```

The ClickHouse tests need a running ClickHouse (`make dev-db`) and create their own throwaway
databases:

```bash
PYLOGGLE_TEST_CLICKHOUSE_DSN='clickhouse://pyloggle:pyloggle@localhost:9000/default' \
  go test -tags integration -count=1 ./internal/storage/clickhouse/
```

CI runs all of these on every push and pull request, plus a packaging dry run and the
repository checks below. You can run the repository checks yourself:

```bash
scripts/check-tree.sh      # no caches, machine paths, oversized files or broken links
scripts/check-versions.sh  # the SDK, the app and CHANGELOG.md name the same version
```

## Things that have their own step

**Golden files.** The Go decoder is tested against real output from the Python SDK, kept in
`internal/model/testdata/`. If you change what the SDK sends, regenerate it with `make golden`
and commit the result.

**Third-party notices.** If you add, remove or upgrade a Go module or a web dependency the
app ships, run `make licenses` (after `npm ci` in `web/`) and commit the updated
`THIRD_PARTY_LICENSES.md` and `web/public/licenses.txt`. CI fails when they are out of date.

**Database changes.** Add a new numbered file in `internal/storage/clickhouse/migrations/` and
register it in `migrate.go`. Never edit a migration that has been released: servers that
already ran it will not run it again.

**Settings.** Every environment variable the server reads is listed on the
[configuration page](https://koneb71.github.io/pyloggle/reference/configuration). A test
compares that page with `internal/config/config.go`, so a new setting needs a row there too.
The same goes for HTTP routes and the
[HTTP API page](https://koneb71.github.io/pyloggle/reference/http-api).

**Docs.** Pages live in `docs/` as plain Markdown, so they read well on GitHub too. Preview
the site with `make docs-dev`. The build fails on a broken link.

**Screenshots.** With a server running and some data in it, `make screenshots` captures every
page in light and dark mode. Check each image for personal data before you commit it.

## Write for people

The app is used by people who are not observability experts, so every word on screen is plain
language: "Requests" rather than "Traces", "Came back" rather than "Regressed", "People
affected" rather than "Affected users". Terms that need a definition go through
`web/src/lib/glossary.ts`, which gives the plain label, the technical name and one sentence of
explanation. `web/src/lib/jargon.test.ts` fails when an old technical term returns to the
screen; add to its list when you replace another one. Code, API fields and logs may keep the
technical names.

The same goes for error messages, alert texts and the docs: say what happened and what to do
next, in full sentences.

## Make a pull request

1. Fork the repository and create a branch from `main`.
2. Keep the change focused: one fix or feature per pull request.
3. Add or update tests for what you changed.
4. Add a line under `## Unreleased` at the top of [CHANGELOG.md](CHANGELOG.md) when users will
   notice the change. If the newest release is still on top, add that heading above it.
5. Run `make test` and the checks above, then open the pull request and fill in the template.
6. Commit as yourself. Commit messages and pull requests credit no AI assistant: no
   `Co-Authored-By` trailer for one, no session link, no "Generated with" footer. CI checks
   this with `scripts/check-commits.sh`.

Small, well-described pull requests are reviewed fastest. A maintainer may push small fixes to
your branch before merging.

Maintainers publish releases as [RELEASING.md](RELEASING.md) describes.

## License

Pyloggle is licensed under the [Apache License 2.0](LICENSE). By sending a contribution you
agree that it is licensed under the same terms (section 5 of the license). There is no separate
contributor agreement to sign.
