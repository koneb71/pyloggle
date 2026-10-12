# Requests

**Requests** shows each request your apps handled, step by step: how long it took, what it
spent its time on, and where it failed, even when one request crosses several apps. In
OpenTelemetry terms, each request is a *trace* and each step a *span*.

Requests come from apps that send through [OpenTelemetry](../send-data/opentelemetry.md),
including FastAPI's own telemetry. The page is hidden when the server does not keep them
(`OTLP_STORE_SPANS=false`).

![Requests: how long requests take, the busiest operations, and the list of requests](../images/screenshots/requests-light.png#gh-light-mode-only)
![Requests: how long requests take, the busiest operations, and the list of requests](../images/screenshots/requests-dark.png#gh-dark-mode-only)

## The list

- **How long requests take** shows how many requests finished within each duration range.
- **Busiest operations** lists the most frequent steps with their count, typical and slow
  durations and the share that failed. Click one to show only it.
- **Requests** lists each request, newest first: its app, method and route, the response code,
  which other apps it reached, its duration, its number of steps and whether it failed.

Search with the same [syntax](search.md#fields-on-the-requests-page) as Logs: `status:error` for
failed steps, `duration:>250` for slow ones, `service:api` for one app.

## One request

![A request's timeline: each step as a bar, colored by app, with failed steps in red](../images/screenshots/request-light.png#gh-light-mode-only)
![A request's timeline: each step as a bar, colored by app, with failed steps in red](../images/screenshots/request-dark.png#gh-dark-mode-only)

The timeline shows each step as a bar, placed where it started and as long as it took, nested
under the step that called it and colored by app. Failed steps are red. Click a step to see its
details, such as the database query or the address it called; the arrow next to a step hides the
steps inside it.

Below the timeline:

- **Messages logged during this request**, from every app it reached;
- **Errors** it caused, each linked to its issue.

**Messages from this request** opens them on [Logs](logs.md).

Request steps are kept for 7 days by default (`PYLOGGLE_RETENTION_SPANS_DAYS`).
