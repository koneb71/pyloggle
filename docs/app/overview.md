# Overview

**Overview** answers one question: is everything OK right now? It is the first page you see
when you open Pyloggle.

![Overview: a status line, five headline numbers, the problems that need attention, recent versions and alerts](../images/screenshots/overview-light.png#gh-light-mode-only)
![Overview: a status line, five headline numbers, the problems that need attention, recent versions and alerts](../images/screenshots/overview-dark.png#gh-dark-mode-only)

## What it shows

- **The status line** says "All clear", or lists what needs attention: problems that came back,
  new problems, problems happening much more often, apps that failed or stopped sending. Each
  part is a link.
- **Five numbers** for the selected time range, each compared with the same length of time just
  before: errors, people affected, the share of web requests that failed, the typical response
  time (with the slowest 5%), and messages logged. Hover over an underlined label to see what it
  means.
- **Needs attention** lists the issues that are new, came back after being fixed, or happen much
  more often than before, with a small chart of each. Open one to see what went wrong.
- **Apps** shows each app's health: *OK*, *degraded* (some requests fail), *failing* (many
  fail), or *silent* (it stopped sending).
- **Recent versions** lists the versions of your apps in the order they first appeared, with
  their errors, so you can see whether a deploy made things worse.
- **Recent alerts** shows the last notifications Pyloggle sent.

## Changing what you see

- The time picker at the top right sets the range: the last hour, day or week, or any range you
  choose. **Live** keeps the page up to date.
- The project switcher at the top of the sidebar changes the project.

Every page keeps its settings in the address bar, so a link you copy shows someone else exactly
what you see.
