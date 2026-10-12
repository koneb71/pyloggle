# Releasing Pyloggle

This is the maintainer's checklist. The first part publishes the project as open source with a
fresh history; the second part is the routine for every release after that.

## Part 1: the first public release (1.0.0)

The private repository's history holds build caches and personal details that should not
become public, so the public repository starts from a single commit. A force-push would not be
enough: GitHub keeps old commits reachable through pull request refs and direct links. Instead,
the old repository is kept private under another name, and a new public one is created.

### 1. Check the release branch

On GitHub, make sure CI is green on the branch that holds the release (`main` once it is merged,
or the branch it was prepared on). Then, in a terminal:

```bash
BRANCH=main        # or the branch the release was prepared on
git clone --branch "$BRANCH" --single-branch https://github.com/koneb71/pyloggle.git pyloggle-1.0
cd pyloggle-1.0
scripts/check-tree.sh                 # no caches, private paths or oversized files
scripts/check-versions.sh v1.0.0      # SDK, web app, docs and CHANGELOG all say 1.0.0
make web test                         # optional: CI already ran these
```

### 2. Make the single commit

Find your private commit address at <https://github.com/settings/emails>: turn on **Keep my
email addresses private**, and copy the `…@users.noreply.github.com` address shown there. Turn
on **Block command line pushes that expose my email** too.

```bash
NOREPLY="12345678+koneb71@users.noreply.github.com"    # yours, from the settings page
git config user.name "koneb71"
git config user.email "$NOREPLY"

git checkout --orphan fresh            # a branch with no history, holding the same files
git commit -m "Pyloggle 1.0.0"
git log --format='%h %an <%ae> %s'     # exactly one commit, with your noreply address
git diff --stat "origin/$BRANCH" fresh # prints nothing: the files are identical
```

### 3. Keep the old repository private, under another name

On <https://github.com/koneb71/pyloggle/settings>:

1. **Repository name**: rename it to `pyloggle-archive`.
2. **Danger zone → Change visibility**: make sure it is **Private**.

Never make the archive public. If the old repository was ever public or forked, copies of its
history may exist elsewhere; this step does not reach them.

### 4. Create the public repository and push

Create <https://github.com/new>: owner `koneb71`, name `pyloggle`, **Public**, and no README,
`.gitignore` or license (the commit has them). Then push the one commit, and nothing else:

```bash
git remote add public https://github.com/koneb71/pyloggle.git
git push public fresh:main
```

The push starts the CI and Docs workflows. The Docs run builds the site but leaves a notice
instead of publishing it, until Pages is turned on in the next step.

### 5. Repository settings

On the new repository:

- **About** (the gear next to it on the main page):
  - Description: "Self-hosted errors, logs, request timelines and alerts for Python apps".
  - Website: `https://koneb71.github.io/pyloggle/`.
  - Topics: `python`, `logging`, `error-tracking`, `observability`, `opentelemetry`,
    `clickhouse`, `self-hosted`, `django`, `fastapi`, `flask`.
- **Settings → General**:
  - Features: Issues on; Wiki and Projects off.
  - Pull requests: allow squash merging, and **Automatically delete head branches**.
- **Settings → Pages → Build and deployment → Source: GitHub Actions**. Then
  **Actions → Docs → Run workflow** on `main`, and check that
  <https://koneb71.github.io/pyloggle/> loads.
- **Settings → Advanced security** (called *Code security* on some accounts), turn on:
  - Private vulnerability reporting ([SECURITY.md](SECURITY.md) points reporters to it).
  - Dependency graph, Dependabot alerts and Dependabot security updates. Version updates come
    from [`.github/dependabot.yml`](.github/dependabot.yml).
  - Secret scanning, with push protection.
- **Settings → Actions → General**:
  - Workflow permissions: **Read repository contents and packages permissions**. Each workflow
    asks for the extra permissions it needs.
  - Leave **Allow GitHub Actions to create and approve pull requests** off.
  - Fork pull request workflows: require approval for first-time contributors.
- **Settings → Rules → Rulesets → New branch ruleset** for the default branch:
  - Require a pull request before merging.
  - Require status checks to pass:
    - `Go (format, vet, unit tests)`
    - `Go (ClickHouse integration)`
    - `Web UI (typecheck, tests, build)`
    - `Python SDK 3.9`
    - `Python SDK 3.13`
    - `Packaging dry run`
    - `Repository checks`
  - Block force pushes and restrict deletions.
  - Add yourself to the bypass list if you want to push small fixes directly.

### 6. PyPI

The Python package is published with trusted publishing, so no PyPI token is stored anywhere.

1. Check that <https://pypi.org/project/pyloggle/> does not exist yet.
2. On PyPI, open **Your account → Publishing → Add a new pending publisher → GitHub**:
   - PyPI project name: `pyloggle`
   - Owner: `koneb71`
   - Repository name: `pyloggle`
   - Workflow name: `release.yml`
   - Environment name: `pypi`
3. On GitHub, open **Settings → Environments → New environment** and name it `pypi`. Under
   **Deployment branches and tags**, allow only tags matching `v*`. Adding yourself as a
   required reviewer makes every PyPI upload wait for your approval.

A pending publisher does not reserve the name, so tag the release soon after.

### 7. Tag 1.0.0

In the clone from step 2 (it still has your noreply address configured):

```bash
git fetch public
git tag -a v1.0.0 -m "Pyloggle 1.0.0" public/main
git push public v1.0.0
```

The **Release** workflow then:

1. checks that the versions match the tag, and takes the release notes from the
   `## 1.0.0` section of [CHANGELOG.md](CHANGELOG.md);
2. builds the binaries for Linux and macOS (amd64 and arm64) and creates the GitHub release
   with them and `SHA256SUMS`;
3. pushes the image `ghcr.io/koneb71/pyloggle` with the tags `1.0.0`, `1.0` and `latest`;
4. publishes the Python package to PyPI (after your approval, if the `pypi` environment asks
   for it).

### 8. Make the image public

A package published to ghcr.io for the first time is private. Open
<https://github.com/koneb71?tab=packages>, then **pyloggle → Package settings → Danger zone →
Change visibility → Public**. Until then, `docker compose pull` fails for everyone else.

### 9. Smoke test

Do this from a machine that never had the code checked out, or in a fresh directory:

```bash
# The image
docker run --rm ghcr.io/koneb71/pyloggle:1.0.0 -version          # pyloggle ingestd 1.0.0 (…)

# Compose, as the README tells newcomers
git clone https://github.com/koneb71/pyloggle.git && cd pyloggle/deploy
docker compose up -d                       # pulls the image, does not build
# open http://localhost:8080/ui/, create a key under Projects → Settings → New key
cd .. && python3 scripts/demo_data.py --url http://localhost:8080 --key pl_...

# The Python package
python3 -m venv /tmp/pl && /tmp/pl/bin/pip install pyloggle==1.0.0
/tmp/pl/bin/python -c "import pyloggle; print(pyloggle.__version__)"     # 1.0.0

# A release archive
curl -LO https://github.com/koneb71/pyloggle/releases/download/v1.0.0/pyloggle_1.0.0_linux_amd64.tar.gz
curl -LO https://github.com/koneb71/pyloggle/releases/download/v1.0.0/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
tar xzf pyloggle_1.0.0_linux_amd64.tar.gz && pyloggle_1.0.0_linux_amd64/ingestd -version
```

Then check by eye:

- The PyPI page shows the README with working links.
- The documentation site: the search works, and the screenshots appear in light and dark.
- In the app, the **Help** page links open the matching documentation pages.
- The README on GitHub shows the screenshot and the badges.

## Part 2: every later release

1. **Choose the version.** Pyloggle follows semantic versioning. Breaking changes need a new
   major version and an "Upgrading to …" page in `docs/self-hosting/`.
2. **Bump it in four places**, in a pull request:

   | File | Change |
   |---|---|
   | `python-sdk/pyloggle/_version.py` | `__version__ = "1.1.0"` |
   | `web/package.json` | `"version": "1.1.0"`, then `npm install --package-lock-only` in `web/` |
   | `docs/package.json` | `"version": "1.1.0"`, then `npm install --package-lock-only` in `docs/` |
   | `CHANGELOG.md` | rename `## Unreleased` to `## 1.1.0` |

   The server's version comes from the tag, so no Go file changes. Then check:

   ```bash
   scripts/check-versions.sh v1.1.0
   ```

3. **Merge** once CI is green.
4. **Tag** the merge commit on `main`:

   ```bash
   git tag -a v1.1.0 -m "Pyloggle 1.1.0" && git push origin v1.1.0
   ```

5. **Watch the Release workflow**, approve the PyPI upload if asked, and run the smoke test
   above with the new version.

### Pre-releases

A tag with a hyphen, such as `v1.1.0-rc.1`, makes a pre-release:

- The GitHub release is marked as a pre-release.
- The image gets no `latest` tag.
- PyPI receives `1.1.0rc1`. The versions in the four files must say `1.1.0-rc.1`, exactly as
  in the tag; the Python build normalizes it.

### If a release fails halfway

The workflow's jobs can be re-run from the Actions page. If the tag itself was wrong, delete
the GitHub release and the tag (`git push origin :refs/tags/v1.1.0`), fix the problem, and tag
again. A version that reached PyPI can never be uploaded again, even after deleting it, so
release the fix as the next patch version instead.
