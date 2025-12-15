import os

from fastapi import FastAPI

from pyloggle import Client, ClientConfig
from pyloggle.fastapi import PyloggleFastAPIMiddleware

app = FastAPI()

client = Client(
    ClientConfig(
        ingest_url=os.getenv("PYLOGGLE_INGEST_URL", "http://localhost:8080/v1/ingest"),
        api_key=os.getenv("PYLOGGLE_API_KEY") or os.getenv("PYLOGGLE_TOKEN"),
        service="example-fastapi",
        env="dev",
        project_id=os.getenv("PYLOGGLE_PROJECT_ID", "default"),
    )
)

app.add_middleware(PyloggleFastAPIMiddleware, client=client)


@app.get("/boom")
def boom():
    raise RuntimeError("boom from fastapi")


