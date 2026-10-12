# Projects

A **project** keeps one team's or product's data apart from the rest, with its own access keys,
limits and alert rules. Everything you see in Pyloggle belongs to the project chosen at the top
of the sidebar.

![Projects: the list of projects with their limits and storage, and the Connect your app guide](../images/screenshots/projects-light.png#gh-light-mode-only)
![Projects: the list of projects with their limits and storage, and the Connect your app guide](../images/screenshots/projects-dark.png#gh-dark-mode-only)

## Connect your app

**Connect your app** shows how to send data to the current project, for plain Python, Django,
FastAPI and OpenTelemetry, with this server's address filled in. When you have just created an
access key, it is filled in too.

## A project's settings

Choose **Settings** on a project's row:

![A project's settings: access keys with their permissions](../images/screenshots/project-settings-light.png#gh-light-mode-only)
![A project's settings: access keys with their permissions](../images/screenshots/project-settings-dark.png#gh-dark-mode-only)

- **Access keys**: create a key for each app or script, with only the permissions it needs:
  - *Send data*: send logs, errors, requests and metrics;
  - *View*: read data, for scripts and the [HTTP API](../reference/http-api.md);
  - *Make changes*: change issues, saved searches and alert rules.

  A new key is shown once; copy it then. Each key lists its first characters and when it was
  last used. **Disable key** stops it at once, without touching the others.
- **Storage**: how much the project stores per kind of data, and how much it received each day.
- **Edit**: the name, the description and the limits: *Messages per second*, *Storage limit
  (GB)* and *Keep data for (days)*, as [retention](../self-hosting/retention-and-footprint.md)
  explains.
- **Danger zone**: erase one person's data (their messages and error reports, found by their
  e-mail address or user ID), or delete the project with all its data.

**New project** creates a project and its first access key.

## Admin activity

**Admin activity** records who created, changed or deleted projects and keys, and who erased
data, newest first.

When `PYLOGGLE_UI_ADMINS` is set, only the people it names can change projects and keys.
