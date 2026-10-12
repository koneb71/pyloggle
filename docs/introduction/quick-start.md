# Quick start

In about five minutes you will have Pyloggle running on your computer and a Python app sending
it errors and log messages.

You need [Docker](https://docs.docker.com/get-docker/) with Compose, `git`, and Python 3.9 or
newer for the app.

## 1. Start the server

```bash
git clone https://github.com/koneb71/pyloggle.git
cd pyloggle/deploy
docker compose up -d
```

This starts two containers: ClickHouse, the database, and the Pyloggle server. The first start
takes a minute while the images download and the database is set up. Check that the server is
up:

```bash
curl http://localhost:8080/healthz
```

It answers `ok`.

> [!NOTE]
> In this setup sign-in is off and the server only accepts connections from this computer.
> That is fine for a trial. Turn sign-in on before you let anyone else reach it.

## 2. Open the app

Open <http://localhost:8080/ui/> in your browser. You land on **Overview**, which stays empty
until an app sends something.

## 3. Get an access key

Apps need an access key to send data.

1. Open **Projects** in the sidebar. A project called **Default** is already there.
2. In its row, choose **Settings**, then **New key**.
3. Give the key a name, such as `my-laptop`, and leave **Send data** ticked.
4. Choose **Create key**. The key appears at the top of the page: copy it now, because it is
   shown only this once.

Below it, **Connect your app** now shows the settings for Python, Django, FastAPI and
OpenTelemetry with your key and this server's address filled in.

## 4. Send data from Python

Install the package:

```bash
pip install pyloggle
```

Tell it where the server is, in the environment of your app (use your own key):

```bash
export PYLOGGLE_INGEST_URL=http://localhost:8080
export PYLOGGLE_API_KEY=pl_your_key_here
```

Save this as `hello.py`:

```python
import logging

import pyloggle

logging.basicConfig(level=logging.INFO)
pyloggle.init(service="hello", env="dev")

log = logging.getLogger("hello")
log.info("Hello from my laptop")
log.warning("Disk is %d%% full", 91)


def checkout(cart):
    return cart["total"] / cart["items"]


checkout({"total": 30, "items": 0})
```

Pyloggle receives every message your app writes with Python's `logging`, at the level your
app's logging is set to. Python's own default skips `info` messages, which is why the script
calls `logging.basicConfig(level=logging.INFO)`.

Run it:

```bash
python hello.py
```

The script stops with a `ZeroDivisionError`. That is the point: Pyloggle catches uncaught errors
and sends them before the program exits.

## 5. See it in Pyloggle

- **Issues** lists `ZeroDivisionError`. Open it to see the line that failed and the value of
  `cart` at that moment. **Steps before the error** shows the two messages logged just before.
- **Logs** shows all three messages. Try typing `level:warn` in the search bar.
- **Overview** now shows one error and one app, called `hello`.

## Next steps

- Add Pyloggle to a real app: Flask, FastAPI, Django and Celery each need a line or two, and
  OpenTelemetry works for other languages.
- Get an alert when a new problem appears: open **Alerts** and choose **New alert**.
- Put the server where your team can reach it, with sign-in, HTTPS and backups.
- Stop the trial with `docker compose down` in the `deploy` folder. Add `-v` to delete the
  data too.
