# Security

Pyloggle holds what your apps say about themselves: error reports with the values of local
variables, log messages, request details. Treat it like any other system with production data.
This page lists what to set before a server is reachable by other people, and what the
Python package keeps out of the data in the first place.

To report a security problem in Pyloggle itself, follow the
[security policy](https://github.com/koneb71/pyloggle/blob/main/SECURITY.md).

## Checklist for a shared server

1. **Turn sign-in on.** `PYLOGGLE_UI_AUTH=oidc` or `azure`, never `disabled`, on a server
   other people can reach. Limit who gets in with `PYLOGGLE_OIDC_ALLOWED_DOMAINS` or
   `PYLOGGLE_OIDC_ALLOWED_EMAILS`, and who may change settings with `PYLOGGLE_UI_ADMINS`. See
   [sign-in](sign-in.md).
2. **Serve it over HTTPS** with a reverse proxy, as in [deployment](deployment.md). Sign-in
   cookies are marked secure when the proxy says the request came over HTTPS.
3. **Require an access key for sending data.** By default, data sent without a key goes to the
   `default` project, which is convenient on a laptop and an open door on the internet. Set
   `PYLOGGLE_DEFAULT_PROJECT_ID=` (empty) once your apps use keys. With sign-in on, reading
   through the API always needs a key.
4. **Give each key only what it needs.** Apps and OpenTelemetry exporters need *Send data*
   only. Scripts and dashboards that read need *View*. Keep *Make changes* for automation that
   must change issues or rules. Create one key per app, so you can disable one without touching
   the others.
5. **Keep ClickHouse private.** Pyloggle is the only thing that should talk to it. In the
   Compose file its ports are not published; on your own server, bind it to `127.0.0.1` or a
   private network. Change its default password.
6. **Protect `/metrics`** with `PYLOGGLE_METRICS_TOKEN`, or keep it reachable only from your
   monitoring network. It names your projects and apps.
7. **Leave the 0.1 compatibility settings off**: `PYLOGGLE_ADMIN_TOKEN` and
   `INGEST_SHARED_SECRET` give broad access with a single shared value.
8. **Keep the session secret secret.** Anyone with `PYLOGGLE_UI_SESSION_SECRET` can create a
   sign-in cookie. Store it like a password, and change it if it leaks; that signs everyone
   out.

## What the Python package leaves out

The `pyloggle` package cleans each event in your app's process, before it is sent:

- **People are counted, not named.** The user ID is sent as a SHA-256 hash, so Pyloggle can
  count the people an error affects without storing who they are. Set `user_id_salt` to a
  secret value to make it a keyed hash (HMAC), so short IDs such as `42` cannot be guessed
  back from it. The user's e-mail address, user name and IP address are sent only with
  `send_default_pii=True`.
- **Web requests** carry their method, route, full address with the query string, status,
  headers (with the secret ones blanked out), user agent and the client's IP address (read from
  `X-Forwarded-For` only with `trust_proxy_headers=True`), but not the body.
- **Secrets are blanked out** as `[REDACTED]`:
  - headers such as `Authorization`, `Cookie`, `Set-Cookie` and `X-API-Key`;
  - fields anywhere in the event whose name contains `password`, `secret`, `token` or
    `api_key`, plus `passwd`, `passphrase`, `client_secret` and `private_key`;
  - values that look like bearer tokens, JWTs, Stripe secret keys, AWS access key IDs or
    Slack tokens, wherever they appear.

  Add your own names with `scrub_keys=["iban", "ssn"]` in `pyloggle.init()`.
- **Request bodies are not sent** unless you turn them on with `emit_request_body=True`, and
  then only their first 4 KB (`max_request_body_bytes`), cleaned the same way.
- **Local variables** in error reports are what makes them useful, and also where personal
  data hides. By default only the variables of your own code are sent (`locals_scope="in_app"`),
  each cut to 256 characters, at most 20 per step. Set `with_locals=False` to send none.

The [SDK configuration](../send-data/configuration.md) lists every option.

## What leaves Pyloggle

- **Alerts** by e-mail, Slack and Telegram name problems and apps, and include the steps just
  before an error, which are text your app logged. They go only to the people the rule names.
- **Webhooks** get counts and locations, never user IDs, e-mail addresses, request addresses,
  steps, or the rule's other recipients. See the [webhook payload](../reference/webhook-payload.md).
- **Exports** from Logs are limited to `PYLOGGLE_EXPORT_MAX_ROWS` rows each.

## Deleting data

- Data is deleted by age, a day at a time, as set in
  [Retention and footprint](retention-and-footprint.md).
- To remove one person's data, for example after a deletion request, use
  `POST /projects/{id}/purge-user` in the [HTTP API](../reference/http-api.md#projects-and-access-keys)
  with their e-mail address or user ID hash. Their messages and error reports are deleted;
  counts of people affected are kept, as numbers.
- Deleting a project on the **Projects** page removes all its data, keys and rules.

Administrators' changes to projects, keys and data are recorded under **Admin activity** on
the Projects page.
