"use strict";

const $ = id => document.getElementById(id);
const sources = [...document.querySelectorAll("[data-source]")];
const sourceNames = {site: "سایت", excel: "اکسل", database: "دیتابیس"};
const examples = [...document.querySelectorAll("[data-example]")];
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
    $("input-info").textContent = config.demo ? "با یکی از منابع و داده ساختگی شروع کن."
      : config.kind === "site" ? (config.live ? "سایت را انتخاب کن. فرمان‌های در دسترس مطابق دسترسی حسابت نمایش داده می‌شوند." : "سایت را انتخاب کن. فقط جستجوی مشتری، پارچه و نخ فعال است.")
      : "منبعی را که در PowerShell تعیین کردی انتخاب کن.";
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
    $("setup").hidden = false;
    $("actions").hidden = true;
    $("source-note").textContent = config.demo ? (selected === "site"
      ? "این انتخاب اتصال ERP واقعی را فعال نمی‌کند. برای آزمون از داده نمونه استفاده می‌شود."
      : `جریان کار با ${sourceNames[selected]} را با داده نمونه امتحان می‌کنی.`)
      : config.kind === "site" ? (config.live ? `حسابیار فقط از همین حساب می‌خواند. فرمان‌های مجاز: ${config.commands.join("، ")}.` : "فقط داده شرکت آزمایشی انتخاب‌شده خوانده می‌شود.")
      : "فایل انتخاب‌شده فقط خوانده می‌شود و پیش‌نویس در فایل جداگانه ذخیره می‌شود.";
  });
}

function showConnected() {
  connected = true;
  $("setup").hidden = true;
  $("actions").hidden = false;
  $("erp-disconnect").hidden = !config.live;
  $("connection-status").textContent = "";
  $("active-source").textContent = config.demo ? "داده نمونه فعال است؛ اطلاعات واقعی نگو." : `اتصال ${sourceNames[selected]} فقط‌خواندنی است.`;
  $("listen").disabled = !Recognition;
  $("erp-speech-fallback").hidden = !config.live || !Recognition;
  $("voice-note").textContent = Recognition ? (!config.live && (config.demo || config.kind === "site")
    ? "برای آزمایش صوت از کدهای نمونه استفاده کن؛ مرورگر ممکن است برای تشخیص صدا از سرویس خودش استفاده کند."
    : "فرمان صوتی با تشخیص فارسی روی دستگاه کار می‌کند. اگر در دسترس نبود، فرمان را بنویس.")
    : "این مرورگر فرمان صوتی وب ندارد؛ فرمان را در کادر بنویس.";
  $("listen").focus();
}

function endErpConnection(message) {
  connected = false;
  stopMic();
  cancelRequest();
  clearAnswer();
  $("transcript").value = "";
  $("allow-browser-speech").checked = false;
  $("actions").hidden = true;
  $("setup").hidden = false;
  $("connection-status").textContent = message;
}

$("connect").addEventListener("click", async () => {
  if (!config || !selected) return;
  $("connect").disabled = true;
  try {
    if (config.live) await post("grant", {});
    showConnected();
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
  $("result").textContent = "در حال بررسی فرمان…";
  try {
    const rubika = Boolean(config.rubika && wantRubika());
    const data = await post("command", rubika ? {text, rubika} : {text}, controller.signal);
    if (request !== controller) return;
    answer = data;
    $("result").textContent = data.text;
    showDelivery(true);
    if (rubika) deliverRubika(data);
  } catch {
    if (request === controller) $("result").textContent = "فرمان اجرا نشد. اتصال محلی را بررسی کن و دوباره امتحان کن.";
  } finally {
    if (request === controller) cancelRequest();
  }
}

function releaseMic(active) {
  if (active !== microphone) return;
  microphone = undefined;
  $("listen").textContent = "🎙️ گفتن فرمان";
  $("listen").setAttribute("aria-pressed", "false");
}

function stopMic() {
  if (!microphone) return;
  const active = microphone;
  releaseMic(active);
  try { active.abort(); } catch { /* Already stopped. */ }
}

$("listen").addEventListener("click", async () => {
  if (!Recognition || !connected || request) return;
  if (microphone) {
    stopMic();
    $("voice-note").textContent = "شنیدن فرمان متوقف شد.";
    return;
  }
  if (!wantVoice() && !wantRubika()) {
    $("voice-note").textContent = "اول روش دریافت نتیجه را انتخاب کن.";
    return;
  }
  stopSpeaking();
  const active = new Recognition();
  microphone = active;
  active.lang = "fa-IR";
  active.continuous = false;
  active.interimResults = false;
  $("listen").textContent = "توقف شنیدن";
  $("listen").setAttribute("aria-pressed", "true");
  active.onend = () => {
    if (microphone !== active) return;
    releaseMic(active);
    $("voice-note").textContent = "فرمانی دریافت نشد؛ دوباره بگو یا بنویس.";
  };
  active.onerror = () => {
    if (microphone !== active) return;
    releaseMic(active);
    $("voice-note").textContent = "تشخیص فرمان ممکن نشد؛ مجوز میکروفون را بررسی کن یا فرمان را بنویس.";
  };
  active.onresult = event => {
    if (microphone !== active) return;
    const result = event.results[event.resultIndex];
    const heard = result?.isFinal ? result[0]?.transcript?.trim() : "";
    if (!heard) return;
    releaseMic(active);
    if (heard.length > 240) {
      $("voice-note").textContent = "فرمان طولانی بود؛ درخواست را کوتاه‌تر بگو.";
      return;
    }
    $("transcript").value = heard;
    $("voice-note").textContent = "متن تشخیص‌داده‌شده در کادر بالاست.";
    execute();
  };
  try {
    if (config.live || (!config.demo && config.kind !== "site")) {
      let localReady = false;
      if ("processLocally" in active && typeof Recognition.available === "function") {
        try { localReady = await Recognition.available({langs: ["fa-IR"], processLocally: true}) === "available"; }
        catch { /* The user may explicitly allow the browser fallback. */ }
      }
      if (!localReady && !(config.live && $("allow-browser-speech").checked)) {
        if (microphone !== active) return;
        releaseMic(active);
        $("voice-note").textContent = config.live ? "تشخیص فارسی روی دستگاه آماده نیست. فرمان را بنویس یا اجازهٔ تشخیص در سرویس مرورگر را فعال کن."
          : "تشخیص فارسی روی دستگاه آماده نیست؛ برای این فایل، فرمان را تایپ کن.";
        return;
      }
      if (localReady) active.processLocally = true;
    }
    if (microphone !== active) return;
    $("voice-note").textContent = "در حال شنیدن…";
    active.start();
  } catch { active.onerror(); }
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
