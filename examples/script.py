import logging
import os

from pyloggle import Client, ClientConfig
from pyloggle.logging_handler import PyloggleHandler


def main():
    client = Client(
        ClientConfig(
            ingest_url=os.getenv("PYLOGGLE_INGEST_URL", "http://localhost:8080/v1/ingest"),
            api_key=os.getenv("PYLOGGLE_API_KEY") or os.getenv("PYLOGGLE_TOKEN"),
            service="example-script",
            env="dev",
            project_id=os.getenv("PYLOGGLE_PROJECT_ID", "default"),
        )
    )

    logger = logging.getLogger("example")
    logger.setLevel(logging.INFO)
    logger.addHandler(PyloggleHandler(client))

    try:
        1 / 0
    except Exception:
        logger.exception("boom from script")

    client.flush()


if __name__ == "__main__":
    main()


