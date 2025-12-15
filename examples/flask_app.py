import os

from flask import Flask

from pyloggle import Client, ClientConfig
from pyloggle.flask import init_app

import logging

app = Flask(__name__)

# Ensure INFO-level application logs are emitted (Pyloggle's handler doesn't change levels).
logging.basicConfig(level=logging.INFO)

client = Client(
    ClientConfig(
        ingest_url=os.getenv("PYLOGGLE_INGEST_URL", "http://localhost:8080/v1/ingest"),
        api_key=os.getenv("PYLOGGLE_API_KEY") or os.getenv("PYLOGGLE_TOKEN"),
        service="example-flask",
        env="dev",
        project_id=os.getenv("PYLOGGLE_PROJECT_ID", "default"),
    )
)

init_app(app, client)


@app.get("/hello")
def hello():
    logging.getLogger(__name__).info("hello from flask")
    return {"ok": True}


@app.get("/boom")
def boom():
    logging.getLogger(__name__).warning("about to raise from flask")
    raise RuntimeError("boom from flask")


if __name__ == "__main__":
    app.run(port=5050, debug=True)


