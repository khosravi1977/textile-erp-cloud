const allowedMethods = new Map([
  ["getMe", new Set(["GET"])],
  ["deleteWebhook", new Set(["POST"])],
  ["getUpdates", new Set(["GET"])],
  ["sendMessage", new Set(["POST"])],
]);

const allowedQueryKeys = new Set(["timeout", "allowed_updates", "offset"]);
const tokenPattern = /^\d{8,12}:[A-Za-z0-9_-]{30,}$/;
const maximumBodyBytes = 64 * 1024;

function jsonResponse(status, data) {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
    },
  });
}

async function secretsEqual(left, right) {
  const encoder = new TextEncoder();
  const [leftHash, rightHash] = await Promise.all([
    crypto.subtle.digest("SHA-256", encoder.encode(left)),
    crypto.subtle.digest("SHA-256", encoder.encode(right)),
  ]);
  const leftBytes = new Uint8Array(leftHash);
  const rightBytes = new Uint8Array(rightHash);
  let difference = 0;
  for (let index = 0; index < leftBytes.length; index += 1) {
    difference |= leftBytes[index] ^ rightBytes[index];
  }
  return difference === 0;
}

async function authorized(request, expectedToken) {
  if (typeof expectedToken !== "string" || expectedToken.length < 43) {
    return false;
  }
  const authorization = request.headers.get("Authorization") ?? "";
  if (!authorization.startsWith("Bearer ")) {
    return false;
  }
  return secretsEqual(authorization.slice(7), expectedToken);
}

function forwardedQuery(source) {
  const result = new URLSearchParams();
  for (const [key, value] of source.searchParams) {
    if (!allowedQueryKeys.has(key) || value.length > 256) {
      continue;
    }
    result.append(key, value);
  }
  return result.toString();
}

export async function handleTelegramRelay(request, env, fetchImpl = fetch) {
  const url = new URL(request.url);
  const relayToken = env?.TELEGRAM_RELAY_TOKEN ?? "";

  if (url.pathname === "/health") {
    if (!(await authorized(request, relayToken))) {
      return jsonResponse(404, { error: "not found" });
    }
    return jsonResponse(200, { status: "ok", service: "textile-telegram-relay" });
  }

  const prefix = "/api/telegram/";
  if (!url.pathname.startsWith(prefix)) {
    return null;
  }
  if (!(await authorized(request, relayToken))) {
    return jsonResponse(401, { error: "unauthorized" });
  }

  const methodName = url.pathname.slice(prefix.length);
  const acceptedHTTPMethods = allowedMethods.get(methodName);
  if (!acceptedHTTPMethods || methodName.includes("/")) {
    return jsonResponse(404, { error: "unsupported telegram method" });
  }
  if (!acceptedHTTPMethods.has(request.method)) {
    return jsonResponse(405, { error: "method not allowed" });
  }

  const botToken = request.headers.get("X-Telegram-Bot-Token") ?? "";
  if (!tokenPattern.test(botToken)) {
    return jsonResponse(400, { error: "invalid bot credentials" });
  }

  const contentLength = Number(request.headers.get("Content-Length") ?? "0");
  if (Number.isFinite(contentLength) && contentLength > maximumBodyBytes) {
    return jsonResponse(413, { error: "request too large" });
  }

  let body;
  if (request.method !== "GET") {
    const bytes = await request.arrayBuffer();
    if (bytes.byteLength > maximumBodyBytes) {
      return jsonResponse(413, { error: "request too large" });
    }
    body = bytes;
  }

  const query = forwardedQuery(url);
  const upstreamURL = `https://api.telegram.org/bot${botToken}/${methodName}${query ? `?${query}` : ""}`;
  let upstream;
  try {
    upstream = await fetchImpl(upstreamURL, {
      method: request.method,
      headers: request.method === "GET" ? undefined : { "Content-Type": "application/json" },
      body,
      redirect: "manual",
    });
  } catch {
    return jsonResponse(502, { error: "telegram upstream unavailable" });
  }

  return new Response(upstream.body, {
    status: upstream.status,
    headers: {
      "Content-Type": upstream.headers.get("Content-Type") ?? "application/json; charset=utf-8",
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
