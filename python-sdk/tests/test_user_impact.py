import hashlib
import hmac
import logging
import unittest

from pyloggle.context import get_user_id_hash, set_user_id_hash
from pyloggle.logging_handler import PyloggleHandler
from pyloggle.user import hash_user_id


class _CaptureClient:
    def __init__(self):
        class _Cfg:
            user_id_salt = None

        self.config = _Cfg()
        self.events = []

    def emit(self, event):
        self.events.append(event)


class TestUserImpact(unittest.TestCase):
    def test_hash_user_id_sha256_no_salt(self) -> None:
        self.assertEqual(hash_user_id("abc"), hashlib.sha256(b"abc").hexdigest())

    def test_hash_user_id_hmac_with_salt(self) -> None:
        expected = hmac.new(b"salt", b"abc", hashlib.sha256).hexdigest()
        self.assertEqual(hash_user_id("abc", salt="salt"), expected)
        self.assertNotEqual(hash_user_id("abc", salt="salt"), hash_user_id("abc"))

    def test_logging_handler_emits_user_id_hash_when_set(self) -> None:
        client = _CaptureClient()
        h = PyloggleHandler(client)  # type: ignore[arg-type]

        uid_hash = hash_user_id("user-123")
        set_user_id_hash(uid_hash)
        self.assertEqual(get_user_id_hash(), uid_hash)

        rec = logging.LogRecord(
            name="test",
            level=logging.INFO,
            pathname=__file__,
            lineno=1,
            msg="hello",
            args=(),
            exc_info=None,
        )
        h.emit(rec)

        self.assertEqual(len(client.events), 1)
        ev = client.events[0]
        self.assertIn("user", ev)
        self.assertEqual(ev["user"]["id_hash"], uid_hash)
        # Ensure raw user id is never included implicitly
        self.assertNotIn("user_id", ev)
        self.assertNotIn("id", ev.get("user", {}))


if __name__ == "__main__":
    unittest.main()


