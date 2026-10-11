"""Flask app instrumented with pyloggle: request scope, unhandled exceptions (once), logs."""
import logging

from flask import Flask

import pyloggle
from pyloggle.flask import init_app

logging.basicConfig(level=logging.INFO)

client = pyloggle.init(service="example-flask", env="dev", emit_request_latency=True)

app = Flask(__name__)
init_app(app, client)


@app.get("/hello")
def hello():
    logging.getLogger(__name__).info("hello from flask")
    return {"ok": True}


@app.get("/boom")
def boom():
    pyloggle.add_breadcrumb("about to fail", category="demo")
    logging.getLogger(__name__).warning("about to raise")
    try:
        {}["missing"]
    except KeyError as exc:
        raise RuntimeError("boom from flask") from exc


if __name__ == "__main__":
    app.run(host="127.0.0.1", port=5050, debug=False)
