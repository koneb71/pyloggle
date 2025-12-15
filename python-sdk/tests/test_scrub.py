import unittest

from pyloggle.scrub import ScrubConfig, scrub_event


class TestScrub(unittest.TestCase):
    def test_scrubs_http_headers_case_insensitive(self) -> None:
        cfg = ScrubConfig(header_keys=("authorization",), body_keys=(), keys=(), key_patterns=())
        ev = {"http": {"headers": {"Authorization": "Bearer abc", "User-Agent": "ua"}}}
        out = scrub_event(ev, config=cfg)
        self.assertEqual(out["http"]["headers"]["Authorization"], "[REDACTED]")
        self.assertEqual(out["http"]["headers"]["User-Agent"], "ua")

    def test_scrubs_http_body_nested(self) -> None:
        cfg = ScrubConfig(header_keys=(), body_keys=("password",), keys=(), key_patterns=())
        ev = {"http": {"body": {"user": {"password": "pw", "name": "n"}}}}
        out = scrub_event(ev, config=cfg)
        self.assertEqual(out["http"]["body"]["user"]["password"], "[REDACTED]")
        self.assertEqual(out["http"]["body"]["user"]["name"], "n")

    def test_scrubs_by_regex_anywhere(self) -> None:
        cfg = ScrubConfig(header_keys=(), body_keys=(), keys=(), key_patterns=(r".*token.*",))
        ev = {"attrs": {"access_token": "t1", "other": "x"}}
        out = scrub_event(ev, config=cfg)
        self.assertEqual(out["attrs"]["access_token"], "[REDACTED]")
        self.assertEqual(out["attrs"]["other"], "x")

    def test_legacy_scrub_keys_are_merged(self) -> None:
        cfg = ScrubConfig(header_keys=(), body_keys=(), keys=(), key_patterns=())
        ev = {"attrs": {"secret": "s", "ok": "v"}}
        out = scrub_event(ev, scrub_keys=("secret",), config=cfg)
        self.assertEqual(out["attrs"]["secret"], "[REDACTED]")
        self.assertEqual(out["attrs"]["ok"], "v")


if __name__ == "__main__":
    unittest.main()


