# Deployment

Pyloggle is two programs: the Pyloggle server (`ingestd`) and ClickHouse, the database it
stores everything in. This page shows three ways to run them, from simplest to most
hands-on, and what to put in front of them before other people use the server.

| Way | Good for |
|---|---|
| [Docker Compose](#docker-compose) | Most servers. Both programs, with tuned settings, in one command |
| [Release binary](#release-binary-and-your-own-clickhouse) | Servers without Docker, or a ClickHouse you already run |
| [From source](#from-source) | Changing Pyloggle itself |

## What you need

- **A Linux server** on x86-64 or ARM64. macOS works for trying it out.
- **2 CPUs and 2 GB of memory** for a small team. The Compose file gives ClickHouse 1.5 GB and
  the Pyloggle server 256 MB.
- **Disk:** roughly 100 bytes per message for each day you keep it. Five million messages a day,
  kept 30 days, need about 15 GB. [Retention and footprint](retention-and-footprint.md) has
  the details and the measurements.
- **ClickHouse 24.3 or newer.** Pyloggle is tested with 24.3, the long-term support release
  the Compose file uses.

## Docker Compose

```bash
git clone https://github.com/koneb71/pyloggle.git
cd pyloggle/deploy
PYLOGGLE_VERSION=1.0.0 docker compose up -d
```

This starts ClickHouse with Pyloggle's memory and disk settings and the Pyloggle server from
the published image `ghcr.io/koneb71/pyloggle`. `PYLOGGLE_VERSION` picks the version; without
it you get `latest`. When the image cannot be pulled, Compose builds it from the checkout.

Out of the box the server listens on `127.0.0.1:8080` only, with sign-in off, which is right
for a first look and wrong for anything else. Before other people use it:

1. Turn on sign-in and set the session secret, as the [sign-in guide](sign-in.md) explains.
2. Set `PYLOGGLE_PUBLIC_URL` to the address people will use, such as
   `https://logs.example.com`, so alerts link to the right place.
3. Put [a reverse proxy with HTTPS](#https-with-a-reverse-proxy) in front of port 8080.
4. Change the ClickHouse password (`CLICKHOUSE_PASSWORD`, and the same password in
   `CLICKHOUSE_DSN`) and keep ClickHouse's ports closed to the outside, as they are in the
   Compose file.

Put your settings in a `docker-compose.override.yml` next to the Compose file, so a later
`git pull` does not conflict with them:

```yaml
services:
  ingestd:
    environment:
      PYLOGGLE_PUBLIC_URL: https://logs.example.com
      PYLOGGLE_UI_AUTH: oidc
      PYLOGGLE_OIDC_ISSUER: https://accounts.google.com
      PYLOGGLE_OIDC_CLIENT_ID: "1234567890-abc.apps.googleusercontent.com"
      PYLOGGLE_OIDC_CLIENT_SECRET: ${OIDC_CLIENT_SECRET}
      PYLOGGLE_UI_SESSION_SECRET: ${PYLOGGLE_UI_SESSION_SECRET}
      ALERTING_ENABLED: "true"
```

Compose reads the `${…}` values from your environment or from a `.env` file in the same
folder. Keep the `.env` file out of version control. Every setting is listed on the
[configuration page](../reference/configuration.md).

**Upgrading** is pulling the new version and starting again. The server updates the database
tables itself when it starts:

```bash
PYLOGGLE_VERSION=1.1.0 docker compose up -d
```

Read the [upgrade notes](upgrading.md) before moving to a new major version.

## Release binary and your own ClickHouse

Each [release](https://github.com/koneb71/pyloggle/releases) has archives for Linux and
macOS on x86-64 and ARM64, with the app built in, and a `SHA256SUMS` file to check them.

```bash
VERSION=1.0.0
curl -LO https://github.com/koneb71/pyloggle/releases/download/v$VERSION/pyloggle_${VERSION}_linux_amd64.tar.gz
curl -LO https://github.com/koneb71/pyloggle/releases/download/v$VERSION/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf pyloggle_${VERSION}_linux_amd64.tar.gz
sudo install pyloggle_${VERSION}_linux_amd64/ingestd /usr/local/bin/
ingestd -version
```

### Prepare ClickHouse

Install ClickHouse from [its own packages](https://clickhouse.com/docs/en/install), then
create a database and a user for Pyloggle:

```sql
CREATE DATABASE pyloggle;
CREATE USER pyloggle IDENTIFIED BY 'a long random password';
GRANT ALL ON pyloggle.* TO pyloggle;
```

The server creates and updates its tables in that database by itself. It also reads ClickHouse's
`system` tables for the System page, which every user may do by default.

ClickHouse's defaults are made for much bigger machines. On a small server, use the settings
in [`deploy/clickhouse/`](https://github.com/koneb71/pyloggle/tree/main/deploy/clickhouse):
copy `config.d/pyloggle.xml` to `/etc/clickhouse-server/config.d/` and
`users.d/pyloggle.xml` to `/etc/clickhouse-server/users.d/`, then restart ClickHouse.
[Retention and footprint](retention-and-footprint.md) explains each setting.

### Run the server with systemd

Put the settings in `/etc/pyloggle/env`, readable only by root:

```bash
CLICKHOUSE_DSN=clickhouse://pyloggle:a-long-random-password@127.0.0.1:9000/pyloggle
PYLOGGLE_PUBLIC_URL=https://logs.example.com
PYLOGGLE_UI_AUTH=oidc
PYLOGGLE_OIDC_ISSUER=https://accounts.google.com
PYLOGGLE_OIDC_CLIENT_ID=1234567890-abc.apps.googleusercontent.com
PYLOGGLE_OIDC_CLIENT_SECRET=...
PYLOGGLE_UI_SESSION_SECRET=...
ALERTING_ENABLED=true
GOMEMLIMIT=200MiB
```

and create `/etc/systemd/system/pyloggle.service`:

```ini
[Unit]
Description=Pyloggle
After=network-online.target clickhouse-server.service
Wants=network-online.target

[Service]
EnvironmentFile=/etc/pyloggle/env
ExecStart=/usr/local/bin/ingestd
DynamicUser=yes
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

The server keeps nothing on its own disk, so it can run as a throwaway user with a read-only
system. Start it and follow its log:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now pyloggle
journalctl -u pyloggle -f
```

The first lines name the version and, if something is missing from the settings, say what.

## HTTPS with a reverse proxy

The server speaks plain HTTP. Put a reverse proxy in front of it for HTTPS. It must pass on
the original host name and scheme, so sign-in sends people back to the right address and
cookies are marked secure.

With [Caddy](https://caddyserver.com/), which gets a certificate by itself:

```
logs.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

With nginx:

```nginx
server {
    listen 443 ssl;
    server_name logs.example.com;
    ssl_certificate     /etc/letsencrypt/live/logs.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/logs.example.com/privkey.pem;

    # Error reports with local variables can be large; exports can take a while.
    client_max_body_size 20m;
    proxy_read_timeout 120s;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Apps can send to the same address. If your apps run on the same private network, they can
also send to port 8080 directly.

## From source

You need Go 1.26 and Node.js 22 or newer.

```bash
git clone https://github.com/koneb71/pyloggle.git
cd pyloggle
make build            # builds the app, then bin/ingestd with the app inside
```

`go install github.com/koneb71/pyloggle/cmd/ingestd@latest` also works, but builds the server
without the app: the pages show "UI not built". It is fine for a server that only receives
data and serves the API.

## Backups

Everything lives in ClickHouse: projects, keys, alert rules and all the data. Back up the
`pyloggle` database with ClickHouse's own
[`BACKUP` command](https://clickhouse.com/docs/en/operations/backup), or stop ClickHouse and
copy its data directory (the `clickhouse_data` volume with Docker Compose). The Pyloggle
server itself holds nothing that needs a backup apart from its settings.

## Checking that it works

- `GET /healthz` answers `ok` while the server runs. Docker uses it for the container's health.
- The **System** page in the app shows the database, disk use, queues and alert checks.
- `GET /metrics` gives the same figures to Prometheus. Set `PYLOGGLE_METRICS_TOKEN` to require
  a token.
