# Issues

An **issue** is a group of errors with the same cause. Pyloggle groups them by the type of error
and the path through your code, not by line numbers, so you fix each problem once and see how
often it happens, to how many people, and since when.

## The list

![Issues: a list with tabs for unresolved, came back, resolved and ignored issues](../images/screenshots/issues-light.png#gh-light-mode-only)
![Issues: a list with tabs for unresolved, came back, resolved and ignored issues](../images/screenshots/issues-dark.png#gh-dark-mode-only)

- **Tabs**: *Unresolved* (still happening, or not yet fixed), *Came back* (marked as fixed,
  then happened again), *Resolved*, *Ignored*, and *All*.
- **Filters**: app, environment, version and who it is assigned to, plus words to search the
  title.
- **Sort** by the most recent, the newest, the most frequent, or the most people affected.
- Each row shows where the error happened, a 24-hour trend, how many times and to how many
  people.
- Tick several issues to resolve, ignore or reopen them at once.

## One issue

![An issue: the error, its numbers, how often it happened, and the code that failed](../images/screenshots/issue-light.png#gh-light-mode-only)
![An issue: the error, its numbers, how often it happened, and the code that failed](../images/screenshots/issue-dark.png#gh-dark-mode-only)

The top shows the error, where it happened, and its numbers: status, times, people affected,
first and last seen, version, app and who it is assigned to. Below, **How often it happened**
charts it over the selected range.

**The latest time** tells the full story of one occurrence. Use **Earlier** and **Later**, or
pick one under **Recent times**, to step through others.

| Tab | What it shows |
|---|---|
| What went wrong | The error and each step of the code that led to it, with the source lines and the values of the variables. *Your code* hides library code, *All code* shows everything, *Plain text* gives a traceback to copy |
| Steps before the error | What the app did just before: log messages, database queries, calls to other services |
| Web request | The request that failed: method, address, route, headers and body. A JSON or form body shows as fields you can fold, or as plain text to copy; secrets show as `[REDACTED]` |
| User & system | The person (as a hash), the server, Python version and the Pyloggle package version |
| Labels & details | Tags and extra details your app attached |
| Messages | Everything logged during the same request |
| Activity | Who resolved, assigned or commented, and when it came back |
| Full data | The occurrence exactly as stored |

![Steps before the error: a database query, a call to another service and a log message](../images/screenshots/issue-steps-light.png#gh-light-mode-only)
![Steps before the error: a database query, a call to another service and a log message](../images/screenshots/issue-steps-dark.png#gh-dark-mode-only)

On the right, **This time** sums up the occurrence, and **Where it happens** shows which
environments, routes, servers and versions it happens on, across every time in the range.

## Working on an issue

- **Assign** it to someone: type a name or e-mail address, or pick *Assign to me*.
- **Resolve** it:
  - *Resolve now*: it reopens, marked as *came back*, if it happens again.
  - *Fixed in the next version*: it reopens only if a version newer than the current one has
    it, so errors from servers still running the old version do not reopen it.
  - *Fixed in a specific version…*: the same, for a version you name.
- **Ignore** it for an hour, a day or a week, until it happens a number of times more, or
  forever. Ignored issues are still recorded.
- **Comment** under *Activity* to leave a note for the team.

An [alert rule](alerts.md) can tell you when a new problem appears or a fixed one comes back.
