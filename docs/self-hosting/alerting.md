# Alerting

Pyloggle can tell you when something goes wrong, by e-mail, Slack, Telegram or a webhook, and
send a recap of yesterday every morning. This page covers turning alerts on, connecting the
channels, and how often each kind of alert is sent.

## Turn alerts on

Alert checks are off until you set:

```bash
ALERTING_ENABLED=true
```

The server then checks every rule every 30 seconds (`ALERTING_POLL_INTERVAL`). Until it is on,
the **Alerts** page says so, and rules can be written but never fire. The **System** page
shows when the rules were last checked, under *Alerts engine*.

## Connect the channels

Each rule says who to tell. E-mail and Telegram need a server setting first; Slack and
webhooks work with an address in the rule.

### E-mail

```bash
ALERT_SMTP_HOST=smtp.example.com
ALERT_SMTP_PORT=587
ALERT_SMTP_USERNAME=alerts@example.com
ALERT_SMTP_PASSWORD=...
ALERT_SMTP_FROM=alerts@example.com
```

On port 587 the connection is upgraded to TLS when the mail server offers it. For port 465, set
`ALERT_SMTP_TLS=true`. Any mail service with SMTP works, including Amazon SES, Postmark,
Mailgun, Google Workspace and Microsoft 365.

### Slack

1. In Slack, create an app for your workspace, turn on **Incoming Webhooks**, and add a webhook
   to the channel that should get the alerts.
2. Copy the webhook address (`https://hooks.slack.com/services/…`).
3. Either paste it into the rule's **Slack** field, or set it for the whole server:

   ```bash
   ALERT_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/T000/B000/XXXX
   ```

   Rules can then tick **The server's Slack channel** instead of pasting the address.

### Telegram

1. Talk to [@BotFather](https://t.me/BotFather) in Telegram, send `/newbot`, and copy the
   token it gives you.
2. Set it on the server:

   ```bash
   ALERT_TELEGRAM_BOT_TOKEN=123456789:AAH...
   ```

3. Add the bot to the group that should get the alerts and send any message in the group.
4. Find the group's chat ID: open
   `https://api.telegram.org/bot<token>/getUpdates` in a browser and look for `"chat":{"id":…}`.
   Group IDs are negative, such as `-1001234567890`.
5. Put the chat ID in the rule's **Telegram** field.

Messages are formatted with bold titles and links. Set `ALERT_TELEGRAM_PARSE_MODE=none` for
plain text.

### Webhooks

Put any address in the rule's **Webhook** field. Pyloggle sends a `POST` with a JSON body
that holds the alert in numbers and in words; the
[webhook payload](../reference/webhook-payload.md) page describes every field. Use it to open
tickets, page someone through your on-call tool, or post to a chat Pyloggle does not support.

## Create a rule

Open **Alerts** and choose **New alert**. A rule reads as a sentence: *Alert me when* something
happens, *for* an app and environment (or all of them), and *tell* these people.

| Alert me when | What it watches |
|---|---|
| a problem marked as fixed comes back | An issue you resolved happens again |
| a new problem appears | An error with a cause Pyloggle has not seen before |
| a problem suddenly happens much more | One issue happens many more times than in the same length of time just before |
| errors happen too often | All errors together, per minute, over a time window |
| messages matching a search cross a limit | Any [search](../app/search.md), such as `"payment failed"` or `http.status:>=500 route:/checkout`, counted over a time window |
| a recap of yesterday, every day | A summary of the day before, sent at a set hour (below) |

**Test this alert** shows, before you save, what the rule would find right now: how many
messages a search matches, or the recap it would send.

Only administrators can create and change rules when `PYLOGGLE_UI_ADMINS` is set.

## How often you hear about it

An alert is worth reading only if it is not the same one again. Pyloggle remembers what each
rule has already told you:

| Rule | Sends |
|---|---|
| A new problem appears | Once per problem |
| A fixed problem comes back | Once each time it comes back after being resolved. Assigning, commenting or ignoring it does not count as coming back |
| A problem jumps | At most once a day per problem |
| Errors happen too often, a search crosses a limit | Once when it starts. It sends again only after a check finds things back to normal and it then starts again |
| Recap of yesterday | Once a day |

On top of that, **After alerting, wait** sets the shortest time between two messages from the
same rule.

If no channel could deliver an alert, for example because the mail server was down, nothing is
recorded as sent, and the next check tries again.

> [!NOTE]
> Right after upgrading from 0.3, problems that were already inside a rule's time window may
> alert one last time, because nothing has been recorded for them yet.

## What an alert says

Every alert says what happened in a sentence, where, and when, then tells the story of the
first few problems behind it: where in the code it failed, the steps just before, the request,
and links to the details. This is an e-mail (Telegram and Slack carry the same, formatted):

```
Too many errors: Billing errors

43 errors in 3 hours, 14.3 an hour. The limit is 7.2 an hour.

Where: billing · production
When:  Oct 12, 14:39–17:39 UTC (3 hours)

1. CardNetworkTimeout: card network did not answer within 10s
   43 times in 3 hours, 3 in the 3 hours before · 40 people
   Happened in app/payments.py, line 131, in charge
     resp = network.authorize(card, amount, timeout=10)
   billing · production · version 2.3.0 · POST /charge → 500
   Just before:
     17:25:42 · log · authorizing card for order 81723
     17:25:42 · HTTP · POST https://cards.example.net/authorize -> timeout
     17:25:42 · log · retrying card network
   Details: https://logs.example.com/ui/issues/fcbc4ae3…?event=d8752c00…
   Messages from this request: https://logs.example.com/ui/logs?from=…&q=request%3Areq-3af95790679c

Open in Pyloggle: https://logs.example.com/ui/logs?from=…&q=service%3Abilling+env%3Aproduction+level%3Aerror

--
Sent by Pyloggle because of the alert rule "Billing errors".
```

The links need `PYLOGGLE_PUBLIC_URL`, the address people use to reach Pyloggle.

The steps before an error are text your app logged, so they go only to the people the rule
names. Webhooks never get them, nor any user IDs, e-mail addresses or request addresses.

## The recap of yesterday

A recap rule sends one message a day, at the hour you choose in the time zone you choose. It
covers the whole of yesterday in that time zone. If the server was down at that hour, the recap
goes out as soon as it is back, the same day.

```
Yesterday in Shop: Morning recap

Oct 11: 54 errors, up from 3 the day before. 1 new problem. 0.9% of requests failed.

Where: all apps · all environments
When:  Sunday, Oct 11 (Europe/Berlin)

New problems:
  • TemplateNotFound: emails/receipt.html: 12 times  https://logs.example.com/ui/issues/ec02e23a…

Happened most:
  • OperationalError: canceling statement due to statement timeout: 29 times · 28 people  https://logs.example.com/ui/issues/4d2b6f76…
  • KeyError: 'currency': 13 times · 13 people  https://logs.example.com/ui/issues/a26b3675…
  • TemplateNotFound: emails/receipt.html: 12 times  https://logs.example.com/ui/issues/ec02e23a…

Apps that need a look:
  • api had trouble: 1.5% of requests failed, 29 errors
  • search went silent: last heard from Oct 11 20:06

Still going on:
  • Billing errors: too many errors, since Oct 12 19:26

Open in Pyloggle: https://logs.example.com/ui/overview?from=2026-10-10T22%3A00%3A00Z&to=2026-10-11T22%3A00%3A00Z
```

A quiet day gets a short message: "Oct 11 was quiet: no errors." The recap lists problems and
apps, never people.

## When alerts do not arrive

- Check that `ALERTING_ENABLED=true` and that the rule is turned on.
- **Alerts sent**, on the Alerts page, lists what each rule sent. The server log names every
  delivery that failed and why, for example a refused SMTP login or an unknown Telegram chat.
- On `/metrics`, `pyloggle_alert_firings_total` counts alerts by kind,
  `pyloggle_alert_notify_failures_total` failed deliveries by channel, and
  `pyloggle_alert_last_run_timestamp_seconds` says when the rules were last checked.
