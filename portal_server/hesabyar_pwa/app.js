"use strict";

const $ = id => document.getElementById(id);
const sources = [...document.querySelectorAll("[data-source]")];
const sourceNames = {site: "سایت", excel: "اکسل", database: "دیتابیس"};
const examples = [...document.querySelectorAll("[data-example]")];
const erpShortcuts = [...document.querySelectorAll("[data-erp-command]")];
const shareGuide = $("share-status").textContent;
const session = new URLSearchParams(location.hash.slice(1)).get("session");
if (session) history.replaceState(null, "", location.pathname + location.search);
const sessionShape = /^[A-Za-z0-9_-]{43}$/;
const portalMode = location.pathname?.startsWith("/executive/hesabyar/");
if (portalMode) $("erp-guide").hidden = true;
let headers = sessionShape.test(session || "") ? {"X-Hesabyar-Session": session} : {};
const Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
let config, selected, connected = false, answer, request, microphone, utterance, sharing = false;
let pairingRequest, delivering;

async function initialize() {
  for (const button of sources) button.disabled = true;
  $("connection-status").textContent = "در حال بررسی منبع…";
  try {
    const response = await fetch("./api/config", {headers, cache: "no-store"});
    if (response.status === 401) {
      if (portalMode) $("demo-title").textContent = "ورود به Textile ERP";
      $("unlock").hidden = portalMode;
      $("erp-login").hidden = !portalMode;
      $("connection-status").textContent = portalMode ? "با حساب خودت در Textile ERP وارد شو." : "نشانی کامل را باز کن یا کد آغاز نشست را وارد کن.";
      return;
    }
    if (portalMode && response.status === 403) {
      $("connection-status").textContent = "این حساب مجوز استفاده از اطلاعات مالی یا عملیاتی حسابیار را ندارد.";
      return;
    }
    if (!response.ok) throw new Error("No source");
    config = await response.json();
    $("unlock").hidden = true;
    $("erp-login").hidden = true;
    $("mode").textContent = config.demo ? `نسخه آزمایشی · داده ساختگی${config.glm ? " · GLM" : ""}`
      : config.kind === "site" ? (config.live ? "Textile ERP · دادهٔ شرکت" : "Textile ERP آزمایشی · فقط‌خواندنی") : "فایل واقعی · فقط روی این کامپیوتر";
    $("mode").hidden = false;
    $("demo-title").textContent = config.live ? "اتصال به Textile ERP" : "حسابداری شما کجاست؟";
    $("input-info").textContent = config.demo ? "با یکی از منابع و داده ساختگی شروع کن."
      : config.kind === "site" ? "سایت را انتخاب کن. فقط جستجوی مشتری، پارچه و نخ فعال است."
      : "منبعی را که در PowerShell تعیین کردی انتخاب کن.";
    $("input-info").hidden = Boolean(config.live);
    $("source-choices").hidden = Boolean(config.live);
    $("connect").textContent = config.demo ? "فعال کردن داده نمونه" : "اتصال فقط‌خواندنی";
    $("examples").hidden = !config.demo;
    $("transcript").placeholder = config.live ? (config.commands.includes("جستجوی پارچه") ? "مثلاً جستجوی پارچه متقال" : "یکی از فرمان‌های مجاز را بگو یا بنویس")
      : config.kind === "site" ? "مثلاً جستجوی پارچه نمونه" : "مثلاً موجودی کد ۱۲۳ را بگو";
    $("erp-guide").hidden = Boolean(config.live);
    $("rubika-choice").hidden = config.kind === "site" && !config.live;
    $("delivery-hint").hidden = config.kind === "site" && !config.live;
    if (config.kind === "site") $("rubika-reply").checked = false;
    $("rubika-setup").hidden = !config.rubika;
    if (config.rubika) showPairing(config.rubika);
    for (const button of sources) button.disabled = !config.demo && config.kind !== button.dataset.source;
    $("connection-status").textContent = "";
    if (config.live) {
      for (const shortcut of erpShortcuts) shortcut.hidden = !config.commands.includes(shortcut.dataset.erpCommand);
      $("erp-shortcuts").hidden = !erpShortcuts.some(shortcut => !shortcut.hidden);
      const site = sources.find(button => button.dataset.source === "site");
      site.click();
      if (config.granted) showConnected();
    }
  } catch {
    $("connection-status").textContent = "سرویس در دسترس نیست. اتصال و لینک حساب‌یار را بررسی کن و صفحه را تازه کن.";
  }
}

async function unlock() {
  const code = $("unlock-code").value.trim();
  $("unlock-code").value = "";
  if (!sessionShape.test(code)) {
    $("connection-status").textContent = "کد بعد از ‎#session=‎ را از لینک خصوصی بردار.";
    return;
  }
  headers = {"X-Hesabyar-Session": code};
  $("unlock-button").disabled = true;
  try { await initialize(); } finally { $("unlock-button").disabled = false; }
}
$("unlock-button").addEventListener("click", unlock);
$("unlock-code").addEventListener("keydown", event => {
  if (event.key === "Enter" && !event.isComposing && !$("unlock-button").disabled) unlock();
});

async function post(path, body, signal) {
  const response = await fetch(`./api/${path}`, {
    method: "POST", body: JSON.stringify(body), signal,
    headers: {...headers, "Content-Type": "application/json"}, cache: "no-store"
  });
  if (!response.ok) {
    if (portalMode && [401, 403].includes(response.status) && path === "command") {
      endErpConnection("دسترسی حسابیار پایان یافته است. دوباره اتصال را برقرار کن.");
      if (response.status === 401) $("erp-login").hidden = false;
    }
    throw new Error("Service unavailable");
  }
  return await response.json();
}

function showPairing(status, command = "") {
  $("pair-rubika").hidden = status === "paired";
  $("check-rubika").hidden = status !== "pending" && status !== "retry";
  $("disconnect-rubika").hidden = status !== "paired";
  $("pair-command").textContent = command;
  $("pair-status").textContent = {
    paired: "گفت‌وگوی خصوصی روبیکا متصل است.",
    not_paired: "برای ارسال خودکار، گفت‌وگوی خصوصی را وصل کن.",
    pending: "کد بالا را در گفت‌وگوی خصوصی بات آزمایشی بفرست و «بررسی اتصال» را بزن.",
    retry: "بررسی اتصال ممکن نشد؛ دوباره امتحان کن."
  }[status] || "";
}

async function pair(action) {
  if (!config?.rubika || pairingRequest) return;
  pairingRequest = true;
  for (const id of ["pair-rubika", "check-rubika", "disconnect-rubika"]) $(id).disabled = true;
  try {
    const data = await post(`rubika/${action}`, {});
    showPairing(data.status, data.command || (data.status === "pending" || data.status === "retry" ? $("pair-command").textContent : ""));
    if (action === "disconnect" && answer?.rubika) {
      answer.rubika = {status: "not_paired"};
      showDelivery();
    }
  } catch {
    $("pair-status").textContent = "دسترسی به سرویس روبیکا ممکن نشد.";
  } finally {
    for (const id of ["pair-rubika", "check-rubika", "disconnect-rubika"]) $(id).disabled = false;
    pairingRequest = false;
  }
}
$("pair-rubika").addEventListener("click", () => pair("pair"));
$("check-rubika").addEventListener("click", () => pair("check"));
$("disconnect-rubika").addEventListener("click", () => pair("disconnect"));

for (const button of sources) {
  button.addEventListener("click", () => {
    connected = false;
    selected = button.dataset.source;
    for (const choice of sources) choice.setAttribute("aria-pressed", String(choice === button));
    stopMic();
    cancelRequest();
    clearAnswer();
    $("transcript").value = "";
    $("voice-note").textContent = "";
    $("speech-options").hidden = true;
    $("setup").hidden = false;
    $("actions").hidden = true;
    $("source-note").textContent = config.demo ? (selected === "site"
      ? "این انتخاب اتصال ERP واقعی را فعال نمی‌کند. برای آزمون از داده نمونه استفاده می‌شود."
      : `جریان کار با ${sourceNames[selected]} را با داده نمونه امتحان می‌کنی.`)
      : config.kind === "site" ? (config.live ? `حسابیار با دسترسی همین حساب فقط اطلاعات را می‌خواند. فرمان‌های مجاز: ${config.commands.join("، ")}.` : "فقط داده شرکت آزمایشی انتخاب‌شده خوانده می‌شود.")
      : "فایل انتخاب‌شده فقط خوانده می‌شود و پیش‌نویس در فایل جداگانه ذخیره می‌شود.";
  });
}

function showConnected(focus = false) {
  connected = true;
  $("setup").hidden = true;
  $("actions").hidden = false;
  $("erp-disconnect").hidden = !config.live;
  $("connection-status").textContent = "";
  if (config.live) $("demo-title").textContent = "فرمان جدید";
  $("active-source").textContent = config.live ? "متصل · فقط‌خواندنی"
    : config.demo ? "داده نمونه فعال است؛ اطلاعات واقعی نگو." : `اتصال ${sourceNames[selected]} فقط‌خواندنی است.`;
  $("speech-options").hidden = true;
  $("voice-note").textContent = !config.live && (config.demo || config.kind === "site")
    ? "برای آزمایش صوت از کدهای نمونه استفاده کن؛ مرورگر ممکن است برای تشخیص صدا از سرویس خودش استفاده کند."
    : "برای شروع روی «گفتن فرمان» بزن.";
  if (focus) $("listen").focus();
}

function endErpConnection(message) {
  connected = false;
  stopMic();
  cancelRequest();
  clearAnswer();
  $("transcript").value = "";
  $("speech-options").hidden = true;
  $("actions").hidden = true;
  $("setup").hidden = false;
  $("demo-title").textContent = "اتصال به Textile ERP";
  $("connection-status").textContent = message;
}

$("connect").addEventListener("click", async () => {
  if (!config || !selected) return;
  $("connect").disabled = true;
  try {
    if (config.live) await post("grant", {});
    showConnected(true);
  } catch {
    $("connection-status").textContent = "اتصال برقرار نشد. مطمئن شو در حساب Textile ERP وارد شده‌ای.";
    if (config.live) $("erp-login").hidden = false;
  } finally {
    $("connect").disabled = false;
  }
});

$("erp-disconnect").addEventListener("click", async () => {
  $("erp-disconnect").disabled = true;
  try {
    await post("disconnect", {});
    endErpConnection("اتصال حسابیار قطع شد.");
    $("connect").focus();
  } catch {
    $("connection-status").textContent = "قطع اتصال تأیید نشد؛ دوباره امتحان کن.";
  } finally {
    $("erp-disconnect").disabled = false;
  }
});

const wantVoice = () => $("voice-reply").checked;
const wantRubika = () => $("rubika-reply").checked;

function stopSpeaking() {
  if (!utterance) return;
  utterance = null;
  window.speechSynthesis.cancel();
}

function clearAnswer() {
  stopSpeaking();
  answer = undefined;
  $("result").textContent = "";
  $("speech-panel").hidden = true;
  $("rubika-panel").hidden = true;
  $("share-text").textContent = "";
  $("speech-status").textContent = "";
  $("share-status").textContent = shareGuide;
  $("retry-rubika").hidden = true;
}

function speak() {
  if (!answer || !wantVoice()) return;
  const synth = window.speechSynthesis;
  const voice = synth?.getVoices().find(candidate => /^fa(?:-|_|$)/i.test(candidate.lang) &&
    (config.demo || candidate.localService));
  if (!voice || !window.SpeechSynthesisUtterance) {
    $("speech-status").textContent = config.demo ? "صدای فارسی در این مرورگر در دسترس نیست؛ پاسخ متنی بالاست."
      : "صدای فارسی روی این دستگاه پیدا نشد؛ پاسخ متنی بالاست.";
    return;
  }
  stopSpeaking();
  const next = new SpeechSynthesisUtterance(answer.text);
  next.lang = voice.lang;
  next.voice = voice;
  next.onstart = () => { if (utterance === next) $("speech-status").textContent = "در حال خواندن پاسخ…"; };
  next.onend = () => {
    if (utterance !== next) return;
    utterance = null;
    $("speech-status").textContent = "";
  };
  next.onerror = () => {
    if (utterance !== next) return;
    utterance = null;
    $("speech-status").textContent = "پخش ممکن نشد؛ دوباره «پخش پاسخ» را بزن یا متن را بخوان.";
  };
  utterance = next;
  $("speech-status").textContent = "در انتظار پخش…";
  try { synth.speak(next); } catch { next.onerror(); }
}

function showDelivery(autoplay = false) {
  $("speech-panel").hidden = !answer || !wantVoice();
  if (!wantVoice()) {
    stopSpeaking();
    $("speech-status").textContent = "";
  } else if (autoplay) {
    speak();
  }
  const shareable = Boolean(answer?.shareable && wantRubika());
  $("rubika-panel").hidden = !shareable;
  $("share-text").textContent = shareable ? answer.text : "";
  $("share").hidden = Boolean(config?.rubika) || typeof navigator.share !== "function";
  $("retry-rubika").hidden = !shareable || answer?.rubika?.status !== "retry";
  if (config?.rubika && shareable) {
    $("share-status").textContent = {
      queued: "پاسخ آمادهٔ ارسال است…",
      sending: "در حال تحویل به گفت‌وگوی خصوصی روبیکا…",
      sent: "بات پیام را به گفت‌وگوی متصل فرستاد.",
      retry: "تحویل بررسی نشد. می‌توانی فقط تحویل را دوباره امتحان کنی.",
      expired: "مهلت تحویل گذشته است.",
      full: "صف جلسه پر است؛ چند دقیقه بعد دوباره امتحان کن.",
      unknown: "نتیجهٔ ارسال معلوم نیست. گفت‌وگوی روبیکا را بررسی کن؛ این پیام خودکار تکرار نمی‌شود.",
      not_paired: "گفت‌وگوی خصوصی در زمان اجرای فرمان متصل نبود."
    }[answer.rubika?.status] || "برای ارسال، گزینهٔ روبیکا را قبل از اجرای فرمان انتخاب کن.";
  }
}

async function deliverRubika(current) {
  const delivery = current?.rubika;
  if (!wantRubika() || !delivery?.key || delivering === current || !["queued", "retry"].includes(delivery.status)) return;
  delivering = current;
  delivery.status = "sending";
  showDelivery();
  try {
    const data = await post("rubika/deliver", {key: delivery.key});
    delivery.status = data.status;
    if (data.status === "not_paired") showPairing("not_paired");
  } catch {
    delivery.status = "retry";
  } finally {
    if (delivering === current) delivering = undefined;
    if (answer === current) showDelivery();
  }
}
$("retry-rubika").addEventListener("click", () => deliverRubika(answer));

for (const id of ["voice-reply", "rubika-reply"]) {
  $(id).addEventListener("change", () => showDelivery());
}
$("replay").addEventListener("click", speak);
$("share").addEventListener("click", async () => {
  if (!wantRubika() || !answer?.shareable || sharing) return;
  const current = answer;
  sharing = true;
  $("share").disabled = true;
  try {
    await navigator.share({text: current.text});
    if (answer === current) $("share-status").textContent = "برای بررسی ارسال به گفت‌وگوی روبیکا برگرد.";
  } catch (error) {
    if (answer === current && error.name !== "AbortError") $("share-status").textContent = "اشتراک‌گذاری باز نشد؛ متن را کپی کن و در گفت‌وگوی روبیکا بچسبان.";
  } finally {
    sharing = false;
    $("share").disabled = false;
  }
});

function cancelRequest() {
  request?.abort();
  request = undefined;
  $("run").disabled = false;
}

async function execute() {
  if (!connected || request) return;
  const text = $("transcript").value.trim();
  clearAnswer();
  if (!text || (!wantVoice() && !wantRubika())) {
    $("result").textContent = text ? "دست‌کم یکی از دو روش دریافت نتیجه را انتخاب کن." : "فرمان را بگو یا بنویس.";
    return;
  }
  const controller = new AbortController();
  request = controller;
  $("run").disabled = true;
  $("transcript").blur?.();
  $("result").textContent = "در حال بررسی فرمان…";
  $("result").scrollIntoView?.({block: "nearest"});
  try {
    const rubika = Boolean(config.rubika && wantRubika());
    const data = await post("command", rubika ? {text, rubika} : {text}, controller.signal);
    if (request !== controller) return;
    answer = data;
    $("result").textContent = data.text;
    $("result").scrollIntoView?.({block: "nearest"});
    showDelivery(true);
    if (rubika) deliverRubika(data);
  } catch {
    if (request === controller) $("result").textContent = "فرمان اجرا نشد. اتصال محلی را بررسی کن و دوباره امتحان کن.";
  } finally {
    if (request === controller) cancelRequest();
  }
}

function stopMic() {
  if (!microphone) return;
  const session = microphone;
  microphone = undefined;
  clearTimeout(session.timer);
  $("listen-label").textContent = "گفتن فرمان";
  $("listen").setAttribute("aria-pressed", "false");
  try { session.recognizer?.abort(); } catch { /* Already stopped. */ }
}

function offerSpeechOptions(message, browserFallback = false) {
  stopMic();
  $("voice-note").textContent = message;
  $("browser-speech").hidden = !browserFallback;
  $("speech-options").hidden = false;
}

function recognitionError(error, local) {
  const messages = {
    "not-allowed": "مجوز میکروفون برای این صفحه داده نشده است. می‌توانی از میکروفون صفحه‌کلید استفاده کنی.",
    "audio-capture": "مرورگر به میکروفون دسترسی ندارد. می‌توانی از میکروفون صفحه‌کلید استفاده کنی.",
    "service-not-allowed": "سرویس تشخیص صدا در این مرورگر در دسترس نیست.",
    "language-not-supported": "تشخیص فارسی در این مرورگر آماده نیست.",
    network: "سرویس تشخیص صدای مرورگر پاسخ نداد. دوباره امتحان کن یا از صفحه‌کلید استفاده کن.",
  };
  offerSpeechOptions(messages[error] || "تشخیص صدا شروع نشد. دوباره امتحان کن یا از صفحه‌کلید استفاده کن.",
    local && config.live && (error === "language-not-supported" || error === "service-not-allowed"));
}

function startRecognition(session, local, recognizer) {
  let active;
  try {
    active = recognizer || new Recognition();
    session.recognizer = active;
    active.lang = "fa-IR";
    active.continuous = false;
    active.interimResults = false;
    if (local) active.processLocally = true;
  } catch {
    recognitionError(undefined, local);
    return;
  }
  const current = () => microphone === session && session.recognizer === active;
  let speechStarted = false;
  active.onstart = () => {
    if (current()) $("voice-note").textContent = "در حال شنیدن… فرمان را بگو.";
  };
  active.onspeechstart = () => { speechStarted = true; };
  active.onend = () => {
    if (!current()) return;
    if (!speechStarted && !session.retried) {
      session.retried = true;
      $("voice-note").textContent = "در حال آماده‌کردن دوبارهٔ میکروفون…";
      startRecognition(session, local);
    } else {
      offerSpeechOptions("فرمانی دریافت نشد. دوباره بگو یا از صفحه‌کلید استفاده کن.", local && config.live);
    }
  };
  active.onerror = event => {
    if (current() && event.error !== "no-speech") recognitionError(event.error, local);
  };
  active.onresult = event => {
    if (!current()) return;
    const result = event.results[event.resultIndex];
    const heard = result?.isFinal ? result[0]?.transcript?.trim() : "";
    if (!heard) return;
    stopMic();
    if (heard.length > 240) {
      $("voice-note").textContent = "فرمان طولانی بود؛ درخواست را کوتاه‌تر بگو.";
      return;
    }
    $("transcript").value = heard;
    $("voice-note").textContent = "متن تشخیص‌داده‌شده در کادر فرمان است.";
    execute();
  };
  try { active.start(); } catch (error) {
    recognitionError(error.name === "NotAllowedError" ? "not-allowed" : undefined, local);
  }
}

async function listen(useBrowser = false) {
  if (!connected || request || microphone) return;
  if (!wantVoice() && !wantRubika()) {
    $("voice-note").textContent = "اول روش دریافت نتیجه را انتخاب کن.";
    return;
  }
  if (!Recognition) {
    offerSpeechOptions("تشخیص مستقیم صدا در این مرورگر در دسترس نیست. از میکروفون صفحه‌کلید استفاده کن.");
    return;
  }
  stopSpeaking();
  $("speech-options").hidden = true;
  const session = {};
  microphone = session;
  $("listen-label").textContent = "توقف شنیدن";
  $("listen").setAttribute("aria-pressed", "true");
  $("voice-note").textContent = "در حال آماده‌کردن میکروفون…";
  const local = !useBrowser && (config.live || (!config.demo && config.kind !== "site"));
  session.timer = setTimeout(() => {
    if (microphone === session) offerSpeechOptions("زمان دریافت فرمان تمام شد. دوباره امتحان کن یا از صفحه‌کلید استفاده کن.", local && config.live);
  }, 25000);
  if (!local) {
    startRecognition(session, false);
    return;
  }
  try {
    if (typeof Recognition.available === "function") {
      const active = new Recognition();
      if ("processLocally" in active) {
        const status = await Recognition.available({langs: ["fa-IR"], processLocally: true});
        if (microphone !== session) return;
        if (status === "available") {
          startRecognition(session, true, active);
          return;
        }
      }
    }
  } catch { /* Check whether the session can show alternatives. */ }
  if (microphone === session) {
    offerSpeechOptions("تشخیص فارسی روی دستگاه آماده نیست. یکی از راه‌های زیر را انتخاب کن.", config.live);
  }
}

$("listen").addEventListener("click", () => {
  if (!microphone) return listen();
  stopMic();
  $("voice-note").textContent = "شنیدن فرمان متوقف شد.";
});
$("start-browser-speech").addEventListener("click", () => {
  if (config?.live && !$("speech-options").hidden && !$("browser-speech").hidden) return listen(true);
});
$("keyboard-dictation").addEventListener("click", () => {
  if (!connected) return;
  stopMic();
  $("voice-note").textContent = "میکروفون صفحه‌کلید را بزن و فرمان را بگو. بعد از بررسی متن، «اجرا» را بزن.";
  $("transcript").focus();
});

$("run").addEventListener("click", () => { stopMic(); execute(); });
$("transcript").addEventListener("keydown", event => {
  if (event.key === "Enter" && !event.isComposing) { stopMic(); execute(); }
});
for (const example of examples) {
  example.addEventListener("click", () => {
    if (request) return;
    stopMic();
    $("transcript").value = example.dataset.example;
    execute();
  });
}
for (const shortcut of erpShortcuts) {
  shortcut.addEventListener("click", () => {
    if (!connected || shortcut.hidden || request) return;
    stopMic();
    $("transcript").value = `${shortcut.dataset.template || shortcut.dataset.erpCommand} `;
    $("transcript").focus();
  });
}

if ("serviceWorker" in navigator) navigator.serviceWorker.register("./sw.js").catch(() => {});
let installPrompt;
window.addEventListener("beforeinstallprompt", event => {
  event.preventDefault();
  installPrompt = event;
  $("install-button").hidden = false;
});
$("install-button").addEventListener("click", async () => {
  if (!installPrompt) return;
  const active = installPrompt;
  installPrompt = undefined;
  $("install-button").hidden = true;
  await active.prompt();
});
window.addEventListener("appinstalled", () => { installPrompt = undefined; $("install-button").hidden = true; });
initialize();
