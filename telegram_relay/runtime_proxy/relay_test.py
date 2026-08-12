import importlib.util
import os
import pathlib
import unittest
from unittest import mock

os.environ.setdefault("TEXTILE_TELEGRAM_UPSTREAM_RELAY_URL", "https://script.google.com/macros/s/example/exec")
os.environ.setdefault("TEXTILE_TELEGRAM_RELAY_TOKEN", "relay-token")
os.environ.setdefault("TEXTILE_TELEGRAM_BOT_TOKEN", "123456:bot-token")

MODULE_PATH = pathlib.Path(__file__).with_name("relay.py")
SPEC = importlib.util.spec_from_file_location("relay", MODULE_PATH)
relay = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(relay)


class RelayFallbackTest(unittest.TestCase):
    def test_poll_failure_is_an_empty_success(self):
        with mock.patch.object(relay, "_upstream", side_effect=TimeoutError()):
            self.assertEqual(
                relay.call_with_fallback("getUpdates", "GET", {"offset": ["1"]}, None),
                {"ok": True, "result": []},
            )

    def test_get_me_is_cached(self):
        value = {"ok": True, "result": {"username": "bot"}}
        relay._get_me_cache = None
        with mock.patch.object(relay, "_upstream", return_value=value) as upstream:
            self.assertEqual(relay.call_with_fallback("getMe", "GET", {}, None), value)
            self.assertEqual(relay.call_with_fallback("getMe", "GET", {}, None), value)
            self.assertEqual(upstream.call_count, 1)


if __name__ == "__main__":
    unittest.main()
