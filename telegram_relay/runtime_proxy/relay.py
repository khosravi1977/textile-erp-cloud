#!/usr/bin/env python3
"""Small authenticated runtime proxy for the Textile ERP Telegram relay.

It isolates transient Google Apps Script failures from the financial service.
Telegram updates are never acknowledged locally: an upstream failure returns an
empty result, so Telegram keeps queued updates for the next successful poll.
"""

from __future__ import annotations

import hmac
import json
import logging
import os
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import parse_qs, urlsplit


LISTEN_HOST = os.getenv("VIORA_RELAY_LISTEN_HOST", "127.0.0.1")
LISTEN_PORT = int(os.getenv("VIORA_RELAY_LISTEN_PORT", "18089"))
UPSTREAM_URL = os.environ["TEXTILE_TELEGRAM_UPSTREAM_RELAY_URL"].strip()
RELAY_TOKEN = os.environ["TEXTILE_TELEGRAM_RELAY_TOKEN"].strip()
BOT_TOKEN = os.environ["TEXTILE_TELEGRAM_BOT_TOKEN"].strip()
MAX_BODY = 32 * 1024
ALLOWED = {
    "getMe": ("GET", 3),
    "getUpdates": ("GET", 1),
    "deleteWebhook": ("POST", 2),
    "sendMessage": ("POST", 3),
}

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
LOGGER = logging.getLogger("viora-telegram-runtime")
_get_me_cache: dict[str, Any] | None = None
_get_me_cached_at = 0.0


def _upstream(method: str, http_method: str, query: dict[str, list[str]], body: Any) -> dict[str, Any]:
    payload = json.dumps(
        {
            "relayToken": RELAY_TOKEN,
            "botToken": BOT_TOKEN,
            "method": method,
            "httpMethod": http_method,
            "query": query,
            "body": body,
        },
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode()
    request = urllib.request.Request(
        UPSTREAM_URL,
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json", "User-Agent": "Viora-Telegram-Runtime/1.0"},
    )
    # The financial service uses a short polling deadline.  Return an empty
    # successful poll before that deadline when Google is slow, otherwise one
    # transient upstream delay incorrectly marks the bot as unavailable.
    upstream_timeout = 3.0 if method == "getUpdates" else 8.0
    with urllib.request.urlopen(request, timeout=upstream_timeout) as response:
        raw = response.read(1024 * 1024 + 1)
    if len(raw) > 1024 * 1024:
        raise ValueError("upstream response too large")
    decoded = json.loads(raw)
    if not isinstance(decoded, dict) or "ok" not in decoded:
        raise ValueError("invalid upstream response")
    return decoded


def call_with_fallback(method: str, http_method: str, query: dict[str, list[str]], body: Any) -> dict[str, Any]:
    global _get_me_cache, _get_me_cached_at
    if method == "getMe" and _get_me_cache and time.monotonic() - _get_me_cached_at < 3600:
        return _get_me_cache

    attempts = ALLOWED[method][1]
    last_error: Exception | None = None
    for attempt in range(attempts):
        try:
            result = _upstream(method, http_method, query, body)
            if method == "getMe" and result.get("ok") is True:
                _get_me_cache = result
                _get_me_cached_at = time.monotonic()
            return result
        except (OSError, TimeoutError, ValueError, json.JSONDecodeError, urllib.error.URLError) as error:
            last_error = error
            if attempt + 1 < attempts:
                time.sleep(0.8 * (attempt + 1))

    LOGGER.warning("upstream relay temporarily unavailable for %s: %s", method, type(last_error).__name__)
    if method == "getUpdates":
        return {"ok": True, "result": []}
    if method == "deleteWebhook":
        return {"ok": True, "result": True, "description": "webhook state retained"}
    if method == "getMe" and _get_me_cache:
        return _get_me_cache
    return {"ok": False, "description": "telegram relay temporarily unavailable"}


class Handler(BaseHTTPRequestHandler):
    server_version = "VioraTelegramRuntime/1.0"

    def log_message(self, format_string: str, *args: Any) -> None:
        LOGGER.info("request %s", format_string % args)

    def _authorized(self) -> bool:
        bearer = self.headers.get("Authorization", "")
        bot = self.headers.get("X-Telegram-Bot-Token", "")
        return hmac.compare_digest(bearer, f"Bearer {RELAY_TOKEN}") and hmac.compare_digest(bot, BOT_TOKEN)

    def _json(self, status: int, value: dict[str, Any]) -> None:
        encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(encoded)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(encoded)

    def _handle(self) -> None:
        if not self._authorized():
            self._json(401, {"ok": False, "description": "unauthorized"})
            return
        parsed = urlsplit(self.path)
        method = parsed.path.strip("/")
        rule = ALLOWED.get(method)
        if not rule or self.command != rule[0]:
            self._json(404, {"ok": False, "description": "method not allowed"})
            return
        body: Any = None
        if self.command == "POST":
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if length < 0 or length > MAX_BODY:
                    raise ValueError("invalid body size")
                body = json.loads(self.rfile.read(length) or b"{}")
                if not isinstance(body, dict):
                    raise ValueError("body must be an object")
            except (ValueError, json.JSONDecodeError):
                self._json(400, {"ok": False, "description": "invalid request"})
                return
        result = call_with_fallback(method, self.command, parse_qs(parsed.query), body)
        self._json(200, result)

    def do_GET(self) -> None:  # noqa: N802
        self._handle()

    def do_POST(self) -> None:  # noqa: N802
        self._handle()


def main() -> None:
    if not UPSTREAM_URL.startswith("https://script.google.com/"):
        raise SystemExit("invalid upstream relay URL")
    if not RELAY_TOKEN or not BOT_TOKEN:
        raise SystemExit("relay credentials are missing")
    server = ThreadingHTTPServer((LISTEN_HOST, LISTEN_PORT), Handler)
    LOGGER.info("runtime relay listening on %s:%d", LISTEN_HOST, LISTEN_PORT)
    server.serve_forever()


if __name__ == "__main__":
    main()
