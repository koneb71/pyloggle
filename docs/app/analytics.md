# Analytics

**Analytics** shows how your apps' health and use change over days and weeks, and builds any
chart you need. Pick a time range (the last 30 days by default), and an app and environment to
narrow it down. It has three tabs.

## Errors

![Analytics, Errors: problems opened and fixed, time to fix, errors by version](../images/screenshots/analytics-errors-light.png#gh-light-mode-only)
![Analytics, Errors: problems opened and fixed, time to fix, errors by version](../images/screenshots/analytics-errors-dark.png#gh-dark-mode-only)

| Part | What it shows |
|---|---|
| New problems, Fixed, Came back | How many problems appeared, were marked fixed, and came back after a fix, against the period just before |
| Typical time to fix | Half of the problems fixed in the range took less than this |
| Opened and fixed | The same three, day by day (hour by hour for a range of two days or less) |
| Time to fix | How long the fixed problems had been open: within an hour, a day, a week, or longer. A problem that came back counts from its comeback |
| By version | Each recent version's errors, errors per 1,000 requests (when your app counts requests), problems first seen in it, and people affected. A red arrow means a version fails more often than the one before |
| Most frequent error types | Errors grouped by exception type, with problems and people |
| Noisiest apps | Errors grouped by app |
| Most affected people | The signed-in people who hit the most errors |

People appear by name only when your app sends who they are (`send_default_pii=True`);
otherwise each person is a short code, the start of the hashed ID. Versions and people come from
error details, which are kept for `PYLOGGLE_RETENTION_ERRORS_DAYS` (30 by default); the rest
comes from counts kept for `PYLOGGLE_RETENTION_STATS_DAYS` (a year by default). Click a row to see
its issues or messages.

## Usage

![Analytics, Usage: requests and people, pages and endpoints, traffic by version](../images/screenshots/analytics-usage-light.png#gh-light-mode-only)
![Analytics, Usage: requests and people, pages and endpoints, traffic by version](../images/screenshots/analytics-usage-dark.png#gh-dark-mode-only)

| Part | What it shows |
|---|---|
| Requests, People, Requests per person, Failed requests | Totals for the range, against the period just before |
| Active people | Different signed-in people per day (or hour), or per week |
| Requests and failures | Requests answered over time, and how many failed with a 5xx response |
| Pages and endpoints | The most used ones, ranked by requests or by how many people use them. Click one to chart it |
| Traffic by version | Requests each version answered over time: watch a release take over |

Usage is built from **request counts**. The Python package counts every web request its Flask,
FastAPI and Django integrations see, and OpenTelemetry server spans are counted on arrival. A
count is a small tally per endpoint and minute, so requests do not appear on **Logs**. See
[request counts](../send-data/configuration.md#request-counts).

People are counted from the hashed ID your app sends with `set_user()`. Pyloggle keeps an
estimate of how many there were, never a list of who: weekly figures are exact enough to compare,
and nobody can be looked up from them.

## Charts

![Analytics, Charts: the slowest 5% of requests by endpoint over time](../images/screenshots/analytics-charts-light.png#gh-light-mode-only)
![Analytics, Charts: the slowest 5% of requests by endpoint over time](../images/screenshots/analytics-charts-dark.png#gh-dark-mode-only)

**Charts** counts or measures **messages**, **errors** or **requests** over time, split by a
field:

| Chart | What you can show | Split by |
|---|---|---|
| Messages | How many, how many people, or the sum, average, lowest, highest, typical (p50), slow (p95) or slowest (p99) value of a number such as `duration_ms` or a detail your app sent | Any field |
| Errors | How many, how many problems, how many people, or a number measured like messages | Error type, app, environment, version, route, method, response code, server, code module, handled, person |
| Requests | How many, how many failed, the share that failed, how many people, and typical, slow and slowest response times | Endpoint, route, method, app, environment, version |

Search the way you search Logs ([search syntax](search.md)). Requests can be filtered by app,
environment, method, route, endpoint and version only, such as `endpoint:"POST /orders"` or
`route:/api/*`. Click a row of the table to narrow the chart to it; drag across the chart to zoom
into that time. On Logs, **Chart** opens these messages here.

### Saving and pinning

**Saved charts → Save this chart** keeps a chart under a name, for you or for everyone in the
project. Tick **Pin to Dashboards** and it shows at the top of **Dashboards**, drawn with the
dashboard's time range, app and environment. The pin in the **Saved charts** menu pins or unpins
it later.

A shared chart that is pinned shows on everyone's Dashboards, so only the person who saved it,
or an admin, can change, pin or delete it.
