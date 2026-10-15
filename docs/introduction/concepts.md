# Concepts

Pyloggle uses plain words for everything it shows. This page explains how the pieces fit
together, and ends with every word the app uses next to the name engineers know it by.

## Projects and apps

A **project** keeps one team's or product's data apart: its own access keys, limits, alert rules
and data. Inside a project, each **app** (a *service*) is one program that sends data, such as
`api`, `web` or `worker`. Each app runs in an **environment**, such as `production` or
`staging`, at a **version** (a *release*) on a **server** (a *host*).

## Errors and issues

Every error an app reports is one **time it happened** (an *occurrence*), with everything known
at that moment: the **code steps** that led to it, the values of variables in **your code**,
the **steps before the error**, the web request, the server and the version.

Errors with the same cause are grouped into one **issue**, so you fix each problem once. An
issue is **unresolved** until you mark it **resolved**; if it happens again after that, it
**came back**. You can also **ignore** it. An error nothing in the app caught, so the request or
job failed, is marked **crashed**.

**People affected** counts the different signed-in people an error hit. Pyloggle counts them
without knowing who they are: the user ID is hashed before it leaves your app.

## Messages

Every log line an app writes is a **message**, with a **severity** (debug, info, warning, error
or fatal) and the **code module** that wrote it. Errors are messages too. Messages of one web
request share a request ID, so you can read them together.

**Filters** list the most common values of each field; **similar messages** group messages by
their shape; **charts** count or measure them over time.

## Requests

A **request timeline** (a *trace*) shows everything one request did, across all the apps it
reached. Each part of it is a **step** (a *span*), such as a database query or a call to another
app. Response times are shown as **typical** (half of the requests were faster), **slow** (the
slowest 5%) and **slowest** (the slowest 1%). **Failed requests** ended with a server error.

## Access and alerts

Apps and scripts use **access keys**. Each key has **permissions**: **send data**, **view**, or
**make changes**.

An **alert rule** watches for something, such as a new problem or too many errors, and tells
the people it names. It compares against a **limit**, and after alerting it can **wait before
alerting again**.

## Words used in Pyloggle

| In the app | Also called | What it means |
|---|---|---|
| Issue | issue | Errors with the same cause, grouped together so you fix each problem once. |
| Time it happened | occurrence (event) | One single time the error happened, with everything recorded at that moment. |
| Times | events | How many times the error happened. |
| People affected | users | How many different signed-in people hit the error. |
| Unresolved | open | Still happening, or not yet marked as fixed. |
| Came back | regressed | Marked as fixed, then happened again. |
| Resolved | resolved | Marked as fixed. You'll be told if it happens again. |
| Ignored | ignored | Hidden from the list on purpose. It is still recorded. |
| Crashed | unhandled | Nothing in the app caught this error, so the request or job failed. |
| Where it happened | culprit | The function and file in your code where the error was raised. |
| Steps before the error | breadcrumbs | What the app did just before the error: queries, outgoing calls, log lines. |
| Version | release | The version of your app that was running, as reported by the SDK. |
| Environment | env | Where the app was running, for example production or staging. |
| App | service | The name your application reports itself as. |
| Server | host | The machine or container that sent the data. |
| Code module | logger | The part of your code that wrote the message. |
| Severity | level | How serious a message is: debug, info, warning, error or fatal. |
| Typical | p50 (median) | Half of the requests were faster than this. |
| Slow | p95 | The slowest 5% of requests took at least this long. |
| Slowest | p99 | The slowest 1% of requests took at least this long. |
| Failed requests | 5xx responses | Requests that ended with a server error (a 5xx response code). |
| Request timeline | trace | Everything one request did, step by step, across all your apps. |
| Step | span | One timed piece of work inside a request, like a database query or an outgoing call. |
| Filters | facets | The most common values of each field, so you can narrow the list with one click. |
| Similar messages | patterns | Messages grouped by their shape, with the changing parts (numbers, ids) blanked out. |
| Charts | analytics | Count or measure messages, errors or requests over time, split by a field. |
| People | unique users | How many different signed-in people, from the hashed ID your app sends. An estimate, never a list of who. |
| Wait before alerting again | cooldown | After an alert is sent, stay quiet for this long even if the condition is still true. |
| Limit | threshold | The number the alert compares against. |
| Permissions | scopes | What an access key is allowed to do. |
| Send data | ingest scope | The key can send logs, errors and traces to this project. |
| View | read scope | The key can read data, for scripts and the query API. |
| Make changes | write scope | The key can change settings such as alert rules and issue states. |
| Your code | in-app frames | Lines from your own project, as opposed to libraries. |
| Code steps | stack frames | The chain of function calls that led to the error. |
| Requests per minute | throughput | How many requests the app handled each minute. |
| Sampled | sampling | Only a share of these messages is kept to save space. |

The app's **Help** page shows the same list.
