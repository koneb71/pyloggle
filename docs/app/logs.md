# Logs

**Logs** holds every message your apps wrote: log lines, errors, and a line for each web
request when request messages are turned on. Search them, filter them, chart them, and follow
one request from start to finish.

![Logs: the search bar, filters, a chart by severity, the messages and one message's details](../images/screenshots/logs-details-light.png#gh-light-mode-only)
![Logs: the search bar, filters, a chart by severity, the messages and one message's details](../images/screenshots/logs-details-dark.png#gh-dark-mode-only)

## Finding messages

- **Type in the search bar**: words find messages that contain them, and `field:value` narrows
  by a field, such as `service:api level:error`. The [search syntax](search.md) page has every
  rule and field. Press `/` to jump to the search bar from anywhere on the page.
- **Add filter** lets you pick a field by its plain name and a value from what is in the time
  range, without typing.
- **The filters sidebar** lists the most common values of each field with counts: severity,
  app, environment, code module, server, version, and for web requests the method, response code
  and route. Click a value to show only it; add any detail your app sends under *Extra details*.
  On a phone, the **Filters** button opens it over the list.
- The search reads back as chips in plain words ("Severity is Error"), so you can check it.
- **The chart** shows how many messages arrived over time, colored by severity or another field.
  Drag across it to zoom in.

## Reading a message

Click a message to open its details: what it says, which app, server, version and code module
wrote it, the web request if there was one, and every extra detail it carries. From there:

- **All messages from this request** shows only the messages logged while handling the same
  request.
- **Show surrounding messages** shows what the same app wrote just before and after.
- **Every step of this request** opens the request's timeline on [Requests](requests.md), when
  it was recorded.
- **Raw data** shows the message exactly as stored.

Use `j` and `k` to move to the next and previous message.

## Similar messages

![Similar messages: messages grouped by their shape, with counts](../images/screenshots/logs-similar-light.png#gh-light-mode-only)
![Similar messages: messages grouped by their shape, with counts](../images/screenshots/logs-similar-dark.png#gh-dark-mode-only)

**Similar messages** groups messages by their shape: numbers, IDs and quoted text are blanked
out, so "user 42 not found" and "user 43 not found" count as one. It is the quickest way to see
what fills your logs, or what changed after a deploy.

## Charts

![Charts: the slowest 5% of requests by route over time](../images/screenshots/logs-charts-light.png#gh-light-mode-only)
![Charts: the slowest 5% of requests by route over time](../images/screenshots/logs-charts-dark.png#gh-dark-mode-only)

**Charts** counts or measures messages over time: how many, or the sum, average, typical (p50),
slow (p95) or slowest (p99) value of a number such as `duration_ms`, split by any field. For
example, *Slow (p95) of Duration split by Route* shows which endpoints got slower. Click a row to
show only its messages.

## Live, saved and exported

- **Go live** adds new messages as they arrive, every few seconds.
- **Saved** keeps a search under a name, for you or for everyone in the project.
- **Export** downloads the messages you are looking at as a spreadsheet (CSV) or one message per
  line (NDJSON), up to the server's limit.

How long messages are kept depends on the server's settings; see
[retention](../self-hosting/retention-and-footprint.md).
