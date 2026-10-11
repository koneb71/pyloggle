"""FastAPI 0.142+ with its native OpenTelemetry support and no pyloggle SDK: one request span
plus operation spans (dependencies, endpoint, serialization, background tasks), HTTP metrics and
an exception log per crash, all sent to pyloggle's OTLP doors by three environment variables.

    pip install -r examples/requirements-otel.txt
    export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080      # /v1/traces, /v1/metrics, /v1/logs are appended
    export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=$PYLOGGLE_API_KEY"
    export FASTAPI_OTEL_AUTO_CONFIGURE=true                       # 0.143+: exporters from the environment
    export OTEL_SERVICE_NAME=otel-example
    export OTEL_RESOURCE_ATTRIBUTES=deployment.environment=dev,service.version=1.0.0
    python -m uvicorn examples.otel_app:app --port 5052

    curl -sS http://127.0.0.1:5052/hello/ada ; curl -sS http://127.0.0.1:5052/boom

In pyloggle: Traces lists GET /boom with its operation spans, the exception log becomes an
issue with parsed frames, and the Dashboards request tiles can read the OTEL HTTP metrics.
"""
import logging

from fastapi import FastAPI

logging.basicConfig(level=logging.INFO)

app = FastAPI(telemetry={"exclude": lambda scope: scope["path"] == "/health"})
log = logging.getLogger("otel.example")


@app.get("/hello/{name}")
def hello(name: str):
    log.info("hello %s from the native OTEL example", name)
    return {"hello": name}


@app.get("/boom")
def boom():
    try:
        {}["missing"]
    except KeyError as exc:
        raise RuntimeError("boom via native OpenTelemetry") from exc


@app.get("/health")
def health():
    return {"ok": True}
