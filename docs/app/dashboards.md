# Dashboards

**Dashboards** shows how a project is doing over time: the charts you pinned, then a few fixed
sections. Pick a time range, and an app and environment to narrow it down.

![Dashboards: errors over time and the most frequent problems](../images/screenshots/dashboards-light.png#gh-light-mode-only)
![Dashboards: errors over time and the most frequent problems](../images/screenshots/dashboards-dark.png#gh-dark-mode-only)

| Section | What it shows |
|---|---|
| Pinned charts | The charts pinned from [Analytics → Charts](analytics.md#saving-and-pinning), drawn with this page's time range, app and environment. **Open in Analytics** opens one to change it |
| Errors | Errors, unresolved and new issues, and people affected, compared with the period before; errors over time; the most frequent problems with their trend, times and people |
| Web requests | Requests per minute, the share that failed, typical and slow response times over time, and the busiest and slowest endpoints |
| Messages | Messages logged, the apps sending them, the share that are errors, and messages over time by severity |
| Custom measurements | Any metric your apps send through OpenTelemetry, such as memory use or queue length |

**Web requests** can be based on the requests your apps count (the Python package counts every
request; OpenTelemetry server spans are counted on arrival), or on OpenTelemetry's
`http.server.request.duration` metric. *Automatic* picks the counted requests and falls back to
the metric. The *Based on* menu shows which one is used. **Chart any endpoint** and each row of
*Busiest endpoints* open the endpoint in [Analytics → Charts](analytics.md#charts).

**Custom measurements** lists the metrics the project has received. Pick one, how to show it
(average, total, lowest, highest or latest value, per second for counters, or typical, slow and
slowest for histograms), and a field to split it by.

Drag across the messages chart to zoom into that time.
