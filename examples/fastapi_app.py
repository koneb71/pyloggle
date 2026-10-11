"""FastAPI app instrumented with pyloggle (pure ASGI middleware, works for Starlette too)."""
import logging

from fastapi import FastAPI

import pyloggle
from pyloggle.fastapi import PyloggleFastAPIMiddleware

logging.basicConfig(level=logging.INFO)

client = pyloggle.init(service="example-fastapi", env="dev", emit_request_latency=True)

app = FastAPI()
app.add_middleware(PyloggleFastAPIMiddleware, client=client)


@app.get("/hello")
def hello():
    logging.getLogger(__name__).info("hello from fastapi")
    return {"ok": True}


@app.get("/boom")
def boom():
    logging.getLogger(__name__).warning("about to raise from fastapi")
    raise RuntimeError("boom from fastapi")
