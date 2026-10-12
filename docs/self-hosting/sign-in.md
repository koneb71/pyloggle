# Sign-in

Pyloggle has no user accounts of its own. People sign in with the accounts they already have,
through a sign-in provider that speaks OpenID Connect: Google, Microsoft, Okta, Keycloak,
Authentik, GitLab and most others. This page shows how to connect one.

The server refuses to start until you choose how people sign in, with `PYLOGGLE_UI_AUTH`:

| Value | Who can open the app |
|---|---|
| `oidc` | People who sign in with your OpenID Connect provider |
| `azure` | People from your Microsoft Entra ID (Azure AD) tenant |
| `disabled` | Anyone who can reach the server. Only for a trial on your own computer or a closed network |

When `PYLOGGLE_UI_AUTH` is not set, the server picks `azure` if `PYLOGGLE_AZURE_TENANT_ID` is
set and `oidc` if `PYLOGGLE_OIDC_ISSUER` is set.

## Before you start

1. **Know the address people will use**, such as `https://logs.example.com`, and serve it over
   HTTPS ([deployment](deployment.md#https-with-a-reverse-proxy) shows how).
2. **The address the provider sends people back to** is that address followed by
   `/ui/oauth2/callback`:

   ```
   https://logs.example.com/ui/oauth2/callback
   ```

   You register it with the provider below.
3. **Create a session secret**. It signs the sign-in cookie; keep it secret, and keep it the
   same across restarts, or everyone is signed out:

   ```bash
   openssl rand -base64 32
   ```

## Any OpenID Connect provider

Register Pyloggle with your provider as a *web application* (also called a *confidential
client*) that uses the *authorization code* flow, with the address from step 2 as its
redirect (callback) URL. The provider gives you a client ID and a client secret. Then set:

```bash
PYLOGGLE_UI_AUTH=oidc
PYLOGGLE_OIDC_ISSUER=https://accounts.google.com
PYLOGGLE_OIDC_CLIENT_ID=1234567890-abc.apps.googleusercontent.com
PYLOGGLE_OIDC_CLIENT_SECRET=...
PYLOGGLE_OIDC_DISPLAY_NAME=Google        # the button says "Sign in with Google"
PYLOGGLE_UI_SESSION_SECRET=...           # from step 3
```

The issuer is the provider's base address. Pyloggle reads
`<issuer>/.well-known/openid-configuration` to find everything else, so open that address in a
browser to check it.

| Provider | Issuer |
|---|---|
| Google | `https://accounts.google.com` |
| Okta | `https://your-org.okta.com`, or `https://your-org.okta.com/oauth2/default` for its default authorization server |
| Keycloak | `https://keycloak.example.com/realms/your-realm` |
| Authentik | `https://authentik.example.com/application/o/your-app/` |
| GitLab | `https://gitlab.com`, or your own GitLab's address |
| Auth0 | `https://your-tenant.auth0.com/` |

Pyloggle asks for the `openid profile email` scopes, which every provider supports; change
them with `PYLOGGLE_OIDC_SCOPES` if yours needs others. It accepts ID tokens signed with RS256
or ES256, checks the issuer, audience, expiry and nonce, and uses PKCE.

## Microsoft Entra ID

1. In the Azure portal, open **Microsoft Entra ID** → **App registrations** → **New
   registration**.
2. Name it Pyloggle, choose **Accounts in this organizational directory only**, and add a
   **Web** redirect URI: `https://logs.example.com/ui/oauth2/callback`.
3. On the app's **Overview**, copy the **Application (client) ID** and the **Directory
   (tenant) ID**.
4. Under **Certificates & secrets**, create a client secret and copy its value.

Then set:

```bash
PYLOGGLE_UI_AUTH=azure
PYLOGGLE_AZURE_TENANT_ID=00000000-0000-0000-0000-000000000000
PYLOGGLE_AZURE_CLIENT_ID=11111111-1111-1111-1111-111111111111
PYLOGGLE_AZURE_CLIENT_SECRET=...
PYLOGGLE_UI_SESSION_SECRET=...
```

Only people from that tenant can sign in. Pyloggle identifies them by their object ID, which
stays the same when their name or e-mail address changes. Client secrets expire; put a
reminder in your calendar to renew it.

## Choosing who gets in

By default everyone your provider lets sign in can use Pyloggle. To narrow it down:

```bash
PYLOGGLE_OIDC_ALLOWED_DOMAINS=example.com                       # anyone @example.com
PYLOGGLE_OIDC_ALLOWED_EMAILS=contractor@partner.example.org     # plus these people
```

Matching either list is enough. Both work with `azure` too. The lists check the e-mail address
in the provider's sign-in token; an address the provider marks as unverified does not count.
Someone not on a list sees a page saying their account is not allowed.

With Google, the domain list is how you limit sign-in to your company's Google Workspace.

## Administrators

Some things affect everyone: projects, access keys, alert rules, deleting data, and the System
page. To limit them to a few people, list their e-mail addresses:

```bash
PYLOGGLE_UI_ADMINS=ana@example.com,ben@example.com
```

When the list is empty, everyone who can sign in is an administrator. Everyone else can still
look at all data and work on issues: assign them, comment, resolve and ignore them.

## Sessions

A sign-in lasts 8 hours. After that, the app sends people through the provider again, which is
usually instant while they are still signed in there. Signing out (the menu at the bottom of the
sidebar) ends the Pyloggle session only, not the one with the provider.

## When it does not work

The server checks the sign-in settings when it starts and names anything missing in its log:

```
configuration: sign-in with oidc is turned on but not configured: set PYLOGGLE_OIDC_CLIENT_SECRET. See https://koneb71.github.io/pyloggle/self-hosting/sign-in
```

Other common problems:

| What you see | What to check |
|---|---|
| The provider says the redirect URI does not match | The redirect URL registered with the provider must be exactly `https://<your address>/ui/oauth2/callback`. Behind a proxy, check that it passes the `Host` header and `X-Forwarded-Proto`, or set `PYLOGGLE_OIDC_REDIRECT_URL` (`PYLOGGLE_AZURE_REDIRECT_URL` for `azure`) |
| "issuer … does not match" | `PYLOGGLE_OIDC_ISSUER` must be the issuer exactly as `.well-known/openid-configuration` states it (a trailing `/` does not matter) |
| You sign in, then land on the sign-in page again | The session cookie was not kept. Serve Pyloggle over HTTPS, or make sure the proxy sends `X-Forwarded-Proto: https` |
| "This account … is not allowed" | The allow-lists. Check the address the page names |
| "… your sign-in provider did not confirm yours" | The provider marks the person's e-mail address as unverified. Verify the address in the provider |

## Scripts and access keys

Scripts do not sign in. They send an [access key](../reference/http-api.md#using-the-query-api)
with each request. While sign-in is on, reading data through the API always needs a key with
the *View* permission.
