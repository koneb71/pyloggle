"""FastAPI app instrumented with pyloggle (pure ASGI middleware, works for Starlette too).

On FastAPI 0.142+ the native OpenTelemetry telemetry can run alongside: the SDK keeps the
issues (locals, breadcrumbs, request context) and FastAPI's own spans and HTTP metrics give the
traces and the dashboards. Set the OTLP environment variables and FASTAPI_OTEL_AUTO_CONFIGURE=true
(see otel_app.py); without them the telemetry dict is inert.
"""
import logging

from fastapi import FastAPI

import pyloggle
from pyloggle.fastapi import PyloggleFastAPIMiddleware, telemetry

logging.basicConfig(level=logging.INFO)

client = pyloggle.init(service="example-fastapi", env="dev")

app = FastAPI(telemetry=telemetry(exclude=lambda scope: scope["path"] == "/health"))
app.add_middleware(PyloggleFastAPIMiddleware, client=client)


@app.get("/hello")
def hello():
    logging.getLogger(__name__).info("hello from fastapi")
    return {"ok": True}


@app.get("/boom")
def boom():
    logging.getLogger(__name__).warning("about to raise from fastapi")
    raise RuntimeError("boom from fastapi")


@app.get("/health")
def health():
    return {"ok": True}
