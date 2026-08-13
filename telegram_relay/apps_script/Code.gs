const RELAY_TOKEN_SHA256 = "SET_IN_DEPLOYED_COPY_ONLY";

const METHOD_RULES = Object.freeze({
  getMe: Object.freeze({ httpMethod: "GET", query: [], body: [] }),
  deleteWebhook: Object.freeze({
    httpMethod: "POST",
    query: [],
    body: ["drop_pending_updates"],
  }),
  getUpdates: Object.freeze({
    httpMethod: "GET",
    query: ["offset", "timeout", "allowed_updates"],
    body: [],
  }),
  sendMessage: Object.freeze({
    httpMethod: "POST",
    query: [],
    body: ["chat_id", "text", "disable_web_page_preview"],
  }),
});

function doGet() {
  return jsonResponse({
    ok: true,
    service: "viora-textile-telegram-relay",
  });
}

function doPost(event) {
  try {
    const raw = event && event.postData ? String(event.postData.contents || "") : "";
    if (!raw || raw.length > 65536) {
      return relayError("invalid request size");
    }

    const input = JSON.parse(raw);
    requireObject(input);
    if (!validRelayToken(String(input.relayToken || ""))) {
      return relayError("unauthorized");
    }

    const botToken = String(input.botToken || "");
    if (!/^\d{6,16}:[A-Za-z0-9_-]{30,100}$/.test(botToken)) {
      return relayError("invalid bot token");
    }

    const method = String(input.method || "");
    const rule = METHOD_RULES[method];
    if (!rule || String(input.httpMethod || "").toUpperCase() !== rule.httpMethod) {
      return relayError("method not allowed");
    }

    const queryInput = copyObject(input.query);
    if (method === "getUpdates") {
      // Apps Script web requests have a shorter practical response window than
      // Telegram long polling. Short polls keep the relay reliably JSON-only.
      queryInput.timeout = "5";
    }
    const query = pickQuery(queryInput, rule.query);
    const body = pickBody(input.body, rule.body);
    const endpoint =
      "https://api.telegram.org/bot" +
      botToken +
      "/" +
      method +
      (query ? "?" + query : "");

    const options = {
      method: rule.httpMethod.toLowerCase(),
      muteHttpExceptions: true,
      followRedirects: false,
    };
    if (rule.httpMethod === "POST") {
      options.contentType = "application/json";
      options.payload = JSON.stringify(body);
    }

    const response = UrlFetchApp.fetch(endpoint, options);
    const responseText = response.getContentText();
    if (responseText.length > 1048576) {
      return relayError("upstream response too large");
    }
    JSON.parse(responseText);
    return ContentService.createTextOutput(responseText).setMimeType(
      ContentService.MimeType.JSON
    );
  } catch (error) {
    return relayError("relay request failed");
  }
}

function validRelayToken(value) {
  if (!/^[A-Za-z0-9_-]{43,128}$/.test(value)) {
    return false;
  }
  if (!/^[a-f0-9]{64}$/.test(RELAY_TOKEN_SHA256)) {
    return false;
  }
  return safeEqual(sha256(value), RELAY_TOKEN_SHA256);
}

function sha256(value) {
  return Utilities.computeDigest(
    Utilities.DigestAlgorithm.SHA_256,
    value,
    Utilities.Charset.UTF_8
  )
    .map(function (part) {
      return (part + 256).toString(16).slice(-2);
    })
    .join("");
}

function safeEqual(left, right) {
  if (left.length !== right.length) {
    return false;
  }
  let difference = 0;
  for (let index = 0; index < left.length; index += 1) {
    difference |= left.charCodeAt(index) ^ right.charCodeAt(index);
  }
  return difference === 0;
}

function pickQuery(value, allowedKeys) {
  if (value == null) {
    return "";
  }
  requireObject(value);
  rejectUnknownKeys(value, allowedKeys);
  const parts = [];
  allowedKeys.forEach(function (key) {
    if (!(key in value)) {
      return;
    }
    const rawValue = Array.isArray(value[key]) ? value[key][0] : value[key];
    const text = String(rawValue == null ? "" : rawValue);
    if (text.length > 256) {
      throw new Error("query value too long");
    }
    parts.push(encodeURIComponent(key) + "=" + encodeURIComponent(text));
  });
  return parts.join("&");
}

function pickBody(value, allowedKeys) {
  if (allowedKeys.length === 0) {
    if (value != null) {
      requireObject(value);
      rejectUnknownKeys(value, allowedKeys);
    }
    return {};
  }
  requireObject(value);
  rejectUnknownKeys(value, allowedKeys);
  const result = {};
  allowedKeys.forEach(function (key) {
    if (key in value) {
      result[key] = value[key];
    }
  });
  const encoded = JSON.stringify(result);
  if (encoded.length > 32768) {
    throw new Error("body too large");
  }
  return result;
}

function rejectUnknownKeys(value, allowedKeys) {
  const allowed = {};
  allowedKeys.forEach(function (key) {
    allowed[key] = true;
  });
  Object.keys(value).forEach(function (key) {
    if (!allowed[key]) {
      throw new Error("field not allowed");
    }
  });
}

function requireObject(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("object required");
  }
}

function copyObject(value) {
  if (value == null) {
    return {};
  }
  requireObject(value);
  const result = {};
  Object.keys(value).forEach(function (key) {
    result[key] = value[key];
  });
  return result;
}

function relayError(message) {
  return jsonResponse({ ok: false, description: message });
}

function jsonResponse(value) {
  return ContentService.createTextOutput(JSON.stringify(value)).setMimeType(
    ContentService.MimeType.JSON
  );
}
