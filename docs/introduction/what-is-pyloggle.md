# What is Pyloggle?

Pyloggle collects the errors, log messages and requests of your apps and shows them in one
place, on a server you run yourself. It is made for teams with Python services who want clear
error reports, logs they can search, and alerts that say what happened, without sending their
data to someone else or running a large monitoring stack.

![Your apps send data to the Pyloggle server, which stores it in ClickHouse, serves the app to your browser and sends alerts](../images/how-it-works.svg)

## What you get

![The Overview page: what needs attention, the headline numbers, and recent versions](../images/screenshots/overview-light.png#gh-light-mode-only)
![The Overview page: what needs attention, the headline numbers, and recent versions](../images/screenshots/overview-dark.png#gh-dark-mode-only)

- **Errors you can act on.** Errors with the same cause are grouped into one *issue*, with the
  full story: the line of your code that failed, the values of its variables, the steps just
  before the error, the web request, and how many people it hit. Mark it fixed, and Pyloggle
  flags it the moment it comes back.
- **Every log message, searchable.** Type a few words or click a filter. Charts show how many
  messages each app wrote over time, similar messages are grouped together, and live mode
  follows new ones as they arrive.
- **Each request, step by step.** See how long requests take, which ones fail, and what one
  request did across all your apps, with the messages it logged and the errors it caused.
- **Dashboards** for errors, web traffic, response times and your own OpenTelemetry metrics,
  with the charts you pin.
- **Analytics** for the longer view: problems opened and fixed, time to fix, how each release
  did, how many people use each page and endpoint, and any chart of messages, errors or
  requests. Requests are counted without filling your logs.
- **Alerts that are worth reading.** Get an e-mail, Slack, Telegram or webhook message when a
  new problem appears, an old one comes back, errors jump, or a search finds matching messages.
  Each problem alerts once, and a daily recap tells you how yesterday went.
- **On your phone too.** Open an alert's link and the problem reads well on a small screen,
  with the same actions as on a desk: assign it, resolve it, or look at its messages.
- **Projects and access keys**, with limits on storage and speed for each project, and keys
  that can only send data, only read, or make changes.

## How it works

- **Your apps send data over HTTP.** Python apps use the `pyloggle` package, which works with
  Flask, FastAPI, Django, Celery and plain scripts. Apps in any language can send through
  OpenTelemetry instead.
- **The Pyloggle server** is one program, `ingestd`. It receives the data, groups errors into
  issues, checks your alert rules, and serves the app at `/ui` and the API.
- **ClickHouse** stores everything. It compresses log data well and deletes old data a whole
  day at a time, so a small server goes a long way: a 2-CPU, 2 GB machine is enough for a
  small team. The [measurements](../self-hosting/retention-and-footprint.md) have the details.

## When Pyloggle fits

Pyloggle is a good fit when you run a handful to a few dozen services, mostly in Python, and
want one small server you control.

Some things it does not do, at least today:

- It runs on one server. It does not spread its work across several machines.
- Only Python has its own package. Other languages send through OpenTelemetry, which gives
  logs, request steps and metrics, but not the detailed error reports of the Python package.
- OpenTelemetry over gRPC, span links and a service map are not supported.

## Open source

Pyloggle is free software under the
[Apache License 2.0](https://github.com/koneb71/pyloggle/blob/main/LICENSE). The code,
issues and releases are on [GitHub](https://github.com/koneb71/pyloggle), and contributions
are welcome: see the
[contributing guide](https://github.com/koneb71/pyloggle/blob/main/CONTRIBUTING.md).

Ready to try it? The [quick start](quick-start.md) takes about five minutes.
