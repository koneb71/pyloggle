# Alerts

**Alerts** lists the rules that tell you when something goes wrong, and the alerts they sent.
Setting up e-mail, Slack, Telegram and the server side is covered in the
[alerting guide](../self-hosting/alerting.md); this page is about writing rules.

![Alerts: rules written as sentences, and the alerts sent recently](../images/screenshots/alerts-light.png#gh-light-mode-only)
![Alerts: rules written as sentences, and the alerts sent recently](../images/screenshots/alerts-dark.png#gh-dark-mode-only)

Each rule reads as a sentence, followed by how far back it looks, how often it alerts, who it
tells and when it last sent something. **Turn off** pauses a rule without deleting it.

## Writing a rule

Choose **New alert**:

![The New alert dialog: when, where and who to tell](../images/screenshots/alert-form-light.png#gh-light-mode-only)
![The New alert dialog: when, where and who to tell](../images/screenshots/alert-form-dark.png#gh-dark-mode-only)

1. **Name** it. The name starts every message the rule sends.
2. **When**: pick what to watch for, and fill in the numbers:
   - *a problem marked as fixed comes back*;
   - *a new problem appears*;
   - *a problem suddenly happens much more*: how many more times than in the same length of time
     before;
   - *errors happen too often*: more than a number of errors a minute, over a time window;
   - *messages matching a search cross a limit*: a [search](search.md), a comparison and a number;
   - *a recap of yesterday, every day*: the hour and the time zone to send it in.
3. **Where**: one app and environment, or all of them. Rules about errors can also be limited to
   one route, such as `POST /checkout`.
4. **Tell**: e-mail addresses, Telegram chats, Slack webhooks or the server's Slack channel, and
   webhook addresses. Any combination works.
5. **After alerting, wait**: the shortest time between two messages from this rule.

**Test this alert** shows, before you save, how many messages a search matches right now, or
the recap the rule would send today.

Each problem alerts a rule once, a jump at most once a day, and "too many errors" or a search
when it starts and again only after it went back to normal; the
[alerting guide](../self-hosting/alerting.md#how-often-you-hear-about-it) explains exactly when.

## Alerts sent

**Alerts sent** lists the most recent alerts, newest first, each with the sentence it sent.
*Open* goes to the matching page in Pyloggle; *Technical details* shows the full alert as the
[webhook payload](../reference/webhook-payload.md) describes it.

When `PYLOGGLE_UI_ADMINS` is set, only the people it names can create and change rules.
