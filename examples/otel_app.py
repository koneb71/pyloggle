"""FastAPI app instrumented with OpenTelemetry only (no pyloggle SDK).

Logs and traces go to pyloggle's OTLP door with nothing but an API key:

    pip install -r examples/requirements-otel.txt
    export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8080      # /v1/logs and /v1/traces are appended
    export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=$PYLOGGLE_API_KEY"
    export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
    export OTEL_SERVICE_NAME=otel-example
    export OTEL_RESOURCE_ATTRIBUTES=deployment.environment=dev,service.version=1.0.0
    python -m uvicorn examples.otel_app:app --port 5052

    curl -sS http://127.0.0.1:5052/hello ; curl -sS http://127.0.0.1:5052/boom

Exceptions recorded on spans (exception.type/message/stacktrace) become issues; log records
with exception attributes do too. Python-formatted stack traces are parsed back into frames.
"""
import logging

from fastapi import FastAPI
from opentelemetry import trace
from opentelemetry._logs import set_logger_provider
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

tracer_provider = TracerProvider()
tracer_provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(tracer_provider)

logger_provider = LoggerProvider()
logger_provider.add_log_record_processor(BatchLogRecordProcessor(OTLPLogExporter()))
set_logger_provider(logger_provider)
logging.basicConfig(level=logging.INFO)
logging.getLogger().addHandler(LoggingHandler(level=logging.INFO, logger_provider=logger_provider))

app = FastAPI()
FastAPIInstrumentor.instrument_app(app)
log = logging.getLogger("otel.example")


@app.get("/hello")
def hello():
    log.info("hello from the OTEL example")
    return {"ok": True}


@app.get("/boom")
def boom():
    try:
        {}["missing"]
    except KeyError as exc:
        raise RuntimeError("boom via OpenTelemetry") from exc
