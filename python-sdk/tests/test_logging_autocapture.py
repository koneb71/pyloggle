import logging
import unittest

from pyloggle.logging_handler import PyloggleHandler, install_pyloggle_logging


class _CaptureClient:
    def __init__(self):
        class _Cfg:
            user_id_salt = None

        self.config = _Cfg()
        self.events = []

    def emit(self, event):
        self.events.append(event)


class TestLoggingAutoCapture(unittest.TestCase):
    def test_install_is_idempotent_on_root(self) -> None:
        root = logging.getLogger()
        before = list(root.handlers)
        try:
            # Ensure we start without a PyloggleHandler on root for this test.
            root.handlers = [h for h in root.handlers if not isinstance(h, PyloggleHandler)]

            client = _CaptureClient()
            install_pyloggle_logging(client)  # type: ignore[arg-type]
            install_pyloggle_logging(client)  # type: ignore[arg-type]

            pyloggle_handlers = [h for h in root.handlers if isinstance(h, PyloggleHandler)]
            self.assertEqual(len(pyloggle_handlers), 1)
        finally:
            root.handlers = before

    def test_framework_logger_is_installed_when_not_propagating(self) -> None:
        root = logging.getLogger()
        fw = logging.getLogger("uvicorn.access")

        root_before = list(root.handlers)
        fw_before_handlers = list(fw.handlers)
        fw_before_propagate = fw.propagate
        try:
            # Simulate an access logger that does NOT propagate to root.
            fw.propagate = False
            fw.handlers = [h for h in fw.handlers if not isinstance(h, PyloggleHandler)]
            root.handlers = [h for h in root.handlers if not isinstance(h, PyloggleHandler)]

            client = _CaptureClient()
            install_pyloggle_logging(client)  # type: ignore[arg-type]

            self.assertTrue(any(isinstance(h, PyloggleHandler) for h in root.handlers))
            self.assertTrue(any(isinstance(h, PyloggleHandler) for h in fw.handlers))
        finally:
            root.handlers = root_before
            fw.handlers = fw_before_handlers
            fw.propagate = fw_before_propagate


if __name__ == "__main__":
    unittest.main()


