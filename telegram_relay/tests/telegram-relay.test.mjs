import assert from "node:assert/strict";
import test from "node:test";

import { handleTelegramRelay } from "../worker/telegram-relay.js";

const relayToken = "r".repeat(48);
const botToken = `123456789:${"b".repeat(35)}`;

test("rejects unauthenticated Telegram relay requests", async () => {
  let called = false;
  const response = await handleTelegramRelay(
    new Request("https://relay.example/api/telegram/getMe"),
    { TELEGRAM_RELAY_TOKEN: relayToken },
    async () => {
      called = true;
      return new Response();
    },
  );

  assert.equal(response.status, 401);
  assert.equal(called, false);
});

test("forwards only an authenticated allowlisted method", async () => {
  let forwardedURL = "";
  let forwardedInit;
  const incomingURL = "https://relay.example/api/telegram/getUpdates?timeout=25&offset=4&ignored=value";
  const response = await handleTelegramRelay(
    new Request(incomingURL, {
      headers: {
        Authorization: `Bearer ${relayToken}`,
        "X-Telegram-Bot-Token": botToken,
      },
    }),
    { TELEGRAM_RELAY_TOKEN: relayToken },
    async (url, init) => {
      forwardedURL = String(url);
      forwardedInit = init;
      return new Response(JSON.stringify({ ok: true, result: [] }), {
        headers: { "Content-Type": "application/json" },
      });
    },
  );

  assert.equal(response.status, 200);
  assert.equal(incomingURL.includes(botToken), false);
  assert.equal(
    forwardedURL,
    `https://api.telegram.org/bot${botToken}/getUpdates?timeout=25&offset=4`,
  );
  assert.equal(forwardedInit.method, "GET");
  assert.equal((await response.json()).ok, true);
});

test("keeps relay health private", async () => {
  const hidden = await handleTelegramRelay(
    new Request("https://relay.example/health"),
    { TELEGRAM_RELAY_TOKEN: relayToken },
  );
  assert.equal(hidden.status, 404);

  const healthy = await handleTelegramRelay(
    new Request("https://relay.example/health", {
      headers: { Authorization: `Bearer ${relayToken}` },
    }),
    { TELEGRAM_RELAY_TOKEN: relayToken },
  );
  assert.equal(healthy.status, 200);
  assert.deepEqual(await healthy.json(), {
    status: "ok",
    service: "textile-telegram-relay",
  });
});

test("server-renders the Persian relay status page", async () => {
  const workerURL = new URL("../dist/server/index.js", import.meta.url);
  workerURL.searchParams.set("test", `${process.pid}-${Date.now()}`);
  const { default: worker } = await import(workerURL.href);

  const response = await worker.fetch(
    new Request("http://localhost/", { headers: { accept: "text/html" } }),
    {
      TELEGRAM_RELAY_TOKEN: relayToken,
      ASSETS: { fetch: async () => new Response("Not found", { status: 404 }) },
    },
    { waitUntil() {}, passThroughOnException() {} },
  );
  const html = await response.text();

  assert.equal(response.status, 200);
  assert.match(html, /واسط امن گزارش‌های تلگرام/);
  assert.match(html, /وضعیت سرویس/);
  assert.doesNotMatch(html, /codex-preview|react-loading-skeleton/i);
});
