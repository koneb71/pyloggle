---
layout: home

hero:
  name: Pyloggle
  text: Errors, logs and alerts for your Python apps
  tagline: Self-hosted, open source, and small enough for one server. Running in five minutes.
  image:
    src: /logo.svg
    alt: Pyloggle
  actions:
    - theme: brand
      text: Quick start
      link: /introduction/quick-start
    - theme: alt
      text: What is Pyloggle?
      link: /introduction/what-is-pyloggle
    - theme: alt
      text: GitHub
      link: https://github.com/koneb71/pyloggle

features:
  - icon: 🐞
    title: Errors that explain themselves
    details: Each error comes with the line that failed, the values of its variables, the steps before it, the request that caused it (body included, secrets blanked out) and the person it hit. Errors with the same cause are grouped into one issue.
  - icon: 🔎
    title: Logs you can search
    details: Find any message in seconds by typing words or clicking filters. Charts, similar messages and live mode show what is going on, and an admin can clear them all to start over.
  - icon: ⏱️
    title: Every request, step by step
    details: See which requests are slow or failing and follow one request across your apps, with its messages and errors.
  - icon: 🔔
    title: Alerts that say what happened
    details: E-mail, Slack, Telegram or webhook. Each problem alerts once, and a recap of yesterday arrives every morning.
  - icon: 🐍
    title: Made for Python, open to everything
    details: One package for Flask, FastAPI, Django, Celery and scripts. Any other app can send through OpenTelemetry.
  - icon: 🪶
    title: Light on resources
    details: One program and ClickHouse. A 2-CPU, 2 GB server is enough for a small team, and old data is dropped a day at a time.
---
