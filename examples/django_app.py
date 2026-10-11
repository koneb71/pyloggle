"""Single-file Django app instrumented with pyloggle: one middleware line, settings.PYLOGGLE.

    pip install "pyloggle[django]"
    PYLOGGLE_INGEST_URL=http://127.0.0.1:8080 PYLOGGLE_API_KEY=pl_... python examples/django_app.py runserver 8000
    curl localhost:8000/hello ; curl localhost:8000/boom

/boom raises after a SQL query, so the issue carries the route template, the user hash (when
X-User is sent), the request headers and a db.query breadcrumb.
"""
import logging
import sys
import types

from django.conf import settings

settings.configure(
    DEBUG=False,
    SECRET_KEY="example",
    ALLOWED_HOSTS=["*"],
    ROOT_URLCONF=__name__,
    MIDDLEWARE=["pyloggle.django.PyloggleMiddleware", f"{__name__}.DemoUserMiddleware"],
    DATABASES={"default": {"ENGINE": "django.db.backends.sqlite3", "NAME": ":memory:"}},
    INSTALLED_APPS=[],
    PYLOGGLE={"service": "example-django", "env": "dev", "emit_request_latency": True},
)

import django  # noqa: E402

django.setup()

import pyloggle  # noqa: E402
import pyloggle.django  # noqa: E402
from django.db import connection  # noqa: E402
from django.http import JsonResponse  # noqa: E402
from django.urls import path  # noqa: E402

logging.basicConfig(level=logging.INFO)
pyloggle.django.init()  # optional: the middleware does this on first use


class DemoUserMiddleware:
    """Stands in for AuthenticationMiddleware: X-User: ada becomes an authenticated user."""

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        name = request.headers.get("X-User")
        request.user = types.SimpleNamespace(is_authenticated=bool(name), pk=name, email=f"{name}@example.com" if name else "", get_username=lambda: name)
        return self.get_response(request)


def hello(request):
    logging.getLogger(__name__).info("hello from django")
    return JsonResponse({"ok": True})


def boom(request):
    with connection.cursor() as cur:
        cur.execute("SELECT 1 + 1")
        cur.fetchone()
    pyloggle.add_breadcrumb("about to fail", category="demo")
    try:
        {}["missing"]
    except KeyError as exc:
        raise RuntimeError("boom from django") from exc


urlpatterns = [path("hello", hello), path("boom", boom)]


if __name__ == "__main__":
    from django.core.management import execute_from_command_line

    execute_from_command_line(sys.argv if len(sys.argv) > 1 else [sys.argv[0], "runserver", "8000"])
