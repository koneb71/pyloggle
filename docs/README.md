# Pyloggle documentation

The documentation is published at **<https://koneb71.github.io/pyloggle/>**. The pages in this
folder are plain Markdown, so you can also read them right here on GitHub, starting with
[index.md](index.md).

## Work on the docs

You need Node.js 22. From the repository root:

```bash
make docs-dev          # live preview on http://localhost:5173/pyloggle/
```

or, inside this folder:

```bash
npm ci
npm run dev            # live preview
npm run build          # the static site, in .vitepress/dist; fails on a broken link
npm run preview        # serve the built site
```

`DOCS_BASE=/ npm run build` builds the site for the root of a host instead of `/pyloggle/`,
for a mirror on your own server.

## Conventions

- Write plain GitHub Markdown, so a page reads the same on GitHub and on the site. Tables,
  code blocks and GitHub's `> [!NOTE]` alerts all work in both. The home page (`index.md`) is
  the one exception: its layout comes from front matter.
- Link to other pages with relative paths that end in `.md`, such as
  `[sign-in](../self-hosting/sign-in.md)`. Link to files outside `docs/` with their full
  GitHub address.
- Use the words the app uses ("Issues", "Requests", "People affected") and the technical name
  in brackets when it helps, as described in
  [CONTRIBUTING.md](https://github.com/koneb71/pyloggle/blob/main/CONTRIBUTING.md#write-for-people).
- Screenshots live in `images/screenshots/`, a light and a dark version of each, shown with
  GitHub's `#gh-light-mode-only` and `#gh-dark-mode-only` markers (the site understands them
  too). To retake them, start a server with sign-in off and an empty database, then run
  `make demo-data docs-screenshots`. Look at each one before committing it.
- New pages need an entry in the sidebar in `.vitepress/config.mts`.

The site is built and published by `.github/workflows/docs.yml` on every push to `main` that
changes this folder.
