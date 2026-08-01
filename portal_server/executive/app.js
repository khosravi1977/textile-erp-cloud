"use strict";

const state = {
  session: null,
  operational: null,
  financial: null,
  financialAlerts: [],
  hall: null,
  errors: {},
  activeTab: "operations",
  loading: true,
  lastUpdated: null,
  refreshSeconds: 60,
  nextRefreshAt: 0,
  installPrompt: null,
  tvMode: false,
  tvTimer: null
};

const tabs = ["operations", "finance", "hall"];
const numberFormat = new Intl.NumberFormat("fa-IR", { maximumFractionDigits: 1 });
const integerFormat = new Intl.NumberFormat("fa-IR", { maximumFractionDigits: 0 });

function byId(id) { return document.getElementById(id); }
function valueNumber(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}
function formatNumber(value, integer = false) {
  return (integer ? integerFormat : numberFormat).format(valueNumber(value));
}
function formatOptionalNumber(value, integer = false) {
  if (value === null || value === undefined || value === "") return "—";
  return formatNumber(value, integer);
}
function formatMoney(value) { return integerFormat.format(valueNumber(value)); }
function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
function list(value) { return Array.isArray(value) ? value : []; }
function firstValue(object, ...keys) {
  for (const key of keys) {
    if (object && object[key] !== undefined && object[key] !== null) return object[key];
  }
  return null;
}

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, {
    credentials: "same-origin",
    cache: "no-store",
    ...options,
    headers: { Accept: "application/json", ...(options.headers || {}) }
  });
  let payload = null;
  try { payload = await response.json(); } catch (_) { payload = null; }
  if (!response.ok) {
    const error = new Error(payload?.error || "دریافت اطلاعات انجام نشد.");
    error.status = response.status;
    error.payload = payload;
    throw error;
  }
  return payload;
}

async function loadDashboard(manual = false) {
  if (state.loading && !manual && state.lastUpdated) return;
  state.loading = true;
  document.body.classList.add("loading");
  state.errors = {};
  setConnection("loading", manual ? "در حال به‌روزرسانی دستی" : "در حال دریافت داده‌های زنده");

  try {
    state.session = await fetchJSON("/api/portal/executive-session");
  } catch (error) {
    state.loading = false;
    document.body.classList.remove("loading");
    if (error.status === 401) {
      window.location.assign("/login?next=%2Fexecutive%2F");
      return;
    }
    state.errors.session = error.message;
    setConnection("offline", "اتصال امن مرکز فرمان برقرار نشد");
    renderAll();
    return;
  }

  state.refreshSeconds = Math.max(30, valueNumber(state.session.refreshSeconds) || 60);
  const requests = [];

  if (state.session.operationalReady) {
    requests.push(
      fetchJSON("/api/operational/api/management-report")
        .then((data) => { state.operational = data; })
        .catch((error) => { state.errors.operational = error.message; state.operational = null; })
    );
  } else {
    state.errors.operational = state.session.operationalMessage || "داده عملیاتی آماده نیست.";
    state.operational = null;
  }

  if (state.session.financialReady && state.session.financialToken) {
    const headers = { Authorization: `Bearer ${state.session.financialToken}` };
    requests.push(
      fetchJSON("/api/financial/api/workspace/summary", { headers })
        .then((data) => { state.financial = data; })
        .catch((error) => { state.errors.financial = error.message; state.financial = null; })
    );
    requests.push(
      fetchJSON("/api/financial/api/workspace/alerts", { headers })
        .then((data) => { state.financialAlerts = list(data?.rows); })
        .catch((error) => { state.errors.financialAlerts = error.message; state.financialAlerts = []; })
    );
  } else {
    state.errors.financial = state.session.financialMessage || "داده مالی آماده نیست.";
    state.financial = null;
    state.financialAlerts = [];
  }

  if (state.session.hallMonitorReady) {
    requests.push(
      fetchJSON("/api/portal/executive-hall")
        .then((data) => { state.hall = data; })
        .catch((error) => { state.errors.hall = error.message; state.hall = null; })
    );
  } else {
    state.hall = null;
  }

  await Promise.allSettled(requests);
  state.loading = false;
  state.lastUpdated = new Date();
  state.nextRefreshAt = Date.now() + state.refreshSeconds * 1000;
  document.body.classList.remove("loading");
  setConnection(navigator.onLine ? "online" : "offline", navigator.onLine ? "داده‌های مرکز فرمان به‌روز است" : "اتصال اینترنت قطع شده است");
  renderAll();
}

function setConnection(mode, title) {
  const dot = byId("liveDot");
  dot.className = `live-dot ${mode === "online" ? "online" : mode === "offline" ? "offline" : ""}`;
  byId("connectionTitle").textContent = title;
}

function metricCard(label, value, unit, subline, tone = "") {
  return `<article class="metric-card ${escapeHTML(tone)}"><div class="label">${escapeHTML(label)}</div><strong>${escapeHTML(value)}${unit ? `<span class="unit">${escapeHTML(unit)}</span>` : ""}</strong><div class="subline">${escapeHTML(subline || "—")}</div></article>`;
}

function renderAll() {
  const company = state.session?.company || "شرکت نساجی";
  const manager = state.session?.displayName || state.session?.username || "مدیر";
  byId("companyName").textContent = company;
  byId("managerName").textContent = manager;
  byId("lastUpdated").textContent = state.lastUpdated
    ? `آخرین دریافت: ${state.lastUpdated.toLocaleTimeString("fa-IR", { hour: "2-digit", minute: "2-digit", second: "2-digit" })}`
    : "هنوز به‌روزرسانی نشده";
  renderPriority();
  renderOperations();
  renderFinance();
  renderHall();
}

function renderPriority() {
  if (state.errors.session) {
    byId("priorityItems").innerHTML = `<span class="priority-item">${escapeHTML(state.errors.session)}</span>`;
    return;
  }
  const operationalAlerts = list(state.operational?.notifications);
  const severeOperational = operationalAlerts.filter((row) => ["critical", "warning"].includes(String(row?.type || "").toLowerCase()));
  const financialAlerts = state.financialAlerts;
  const messages = [];
  if (severeOperational[0]) messages.push(severeOperational[0].title || severeOperational[0].message);
  if (financialAlerts[0]) messages.push(financialAlerts[0].title || financialAlerts[0].message);
  const lowMachines = list(state.operational?.machines).filter((row) => valueNumber(row.remaining_percent) <= 10 || valueNumber(row.material_shortage) > 0);
  if (lowMachines.length) messages.push(`${formatNumber(lowMachines.length, true)} ماشین نیازمند بررسی مواد است`);
  if (!messages.length) messages.push("هشدار فوری ثبت نشده؛ وضعیت فعلی پایدار است");
  byId("priorityItems").innerHTML = messages.slice(0, 3).map((message) => `<span class="priority-item">${escapeHTML(message)}</span>`).join("");
}

function renderOperations() {
  const op = state.operational || {};
  const today = op.today || {};
  const month = op.month || {};
  const stock = op.stock || {};
  const alerts = list(op.notifications);

  byId("operationsMetrics").innerHTML = [
    metricCard("تولید امروز", formatNumber(today.metr), "متر", `${formatNumber(month.metr)} متر در ماه`, "positive"),
    metricCard("وزن تولید امروز", formatNumber(today.weight), "کیلو", `${formatNumber(month.weight)} کیلو در ماه`),
    metricCard("طاقه ثبت‌شده امروز", formatNumber(today.pieces, true), "طاقه", `${formatNumber(month.pieces, true)} طاقه در ماه`),
    metricCard("موجودی پارچه انبار", formatNumber(stock.total_taghe, true), "طاقه", `${formatNumber(stock.total_metr)} متر آماده خروج`, valueNumber(stock.total_taghe) > 30 ? "warning" : "")
  ].join("");

  const machineRows = list(op.month_by_machine);
  byId("machineProductionRows").innerHTML = machineRows.length
    ? machineRows.map((row) => `<tr><td>ماشین ${escapeHTML(row.machine || "—")}</td><td>${formatNumber(row.pieces, true)}</td><td>${formatNumber(row.metr)}</td><td>${formatNumber(row.weight)} کیلو</td></tr>`).join("")
    : emptyTableRow(4, state.errors.operational || "هنوز تولیدی برای ماه جاری ثبت نشده است.");

  byId("operationsAlertCount").textContent = formatNumber(alerts.length, true);
  byId("operationsAlerts").innerHTML = alerts.length
    ? alerts.slice(0, 12).map(renderAlert).join("")
    : `<div class="empty-state">${escapeHTML(state.errors.operational || "هشدار عملیاتی فعالی وجود ندارد.")}</div>`;
}

function renderFinance() {
  const summary = state.financial || {};
  const margin = valueNumber(summary.gross_margin);
  const cash = valueNumber(summary.cash_balance);

  byId("financeMetrics").innerHTML = [
    metricCard("موجودی بانک و صندوق", formatMoney(cash), "تومان", "مانده ثبت‌شده همه حساب‌ها", cash < 0 ? "danger" : "positive"),
    metricCard("حاشیه ناخالص", formatMoney(margin), "تومان", "فروش منهای خرید و هزینه", margin < 0 ? "danger" : "positive"),
    metricCard("مطالبات باز", formatMoney(summary.open_receivables), "تومان", "مبالغی که باید دریافت شود", valueNumber(summary.open_receivables) > 0 ? "warning" : ""),
    metricCard("بدهی و پرداختنی باز", formatMoney(summary.open_payables), "تومان", "تعهدهای پرداخت‌نشده", valueNumber(summary.open_payables) > cash ? "danger" : "")
  ].join("");

  byId("cashFlow").innerHTML = [
    ["فروش ثبت‌شده", summary.total_sales, "sales"],
    ["خرید ثبت‌شده", summary.total_purchases, "purchase"],
    ["هزینه ثبت‌شده", summary.total_expenses, "expense"]
  ].map(([label, value, kind]) => `<div class="flow-item ${kind}"><span>${label}</span><strong>${formatMoney(value)} <small>تومان</small></strong></div>`).join("");

  byId("financeAlertCount").textContent = formatNumber(state.financialAlerts.length, true);
  byId("financeAlerts").innerHTML = state.financialAlerts.length
    ? state.financialAlerts.slice(0, 12).map(renderAlert).join("")
    : `<div class="empty-state">${escapeHTML(state.errors.financial || state.errors.financialAlerts || "هشدار مالی فعالی وجود ندارد.")}</div>`;
}

function renderHall() {
  const specialized = state.hall?.source === "specialized" && state.hall?.data;
  if (specialized) {
    renderSpecializedHall(state.hall.data);
  } else {
    renderOperationalHallFallback();
  }
}

function renderSpecializedHall(payload) {
  const machines = list(firstValue(payload, "machines", "machineRows", "machine_rows"));
  const weavers = list(firstValue(payload, "weavers", "weaverRows", "weaver_rows"));
  const efficiency = valueNumber(firstValue(payload, "hallEfficiency", "hall_efficiency", "efficiency"));
  const activeMachines = valueNumber(firstValue(payload, "activeMachines", "active_machines")) || machines.filter((row) => String(row.status || "").toLowerCase() !== "stopped").length;
  const totalStops = valueNumber(firstValue(payload, "totalStops", "total_stops")) || machines.reduce((sum, row) => sum + valueNumber(firstValue(row, "stops", "stop_count")), 0);
  const attention = machines.filter((row) => ["alert", "warning", "watch", "stopped"].includes(String(row.status || "").toLowerCase())).length;
  const topWeaver = [...weavers].sort((a, b) => valueNumber(firstValue(b, "efficiency", "score")) - valueNumber(firstValue(a, "efficiency", "score")))[0];

  const generatedAt = firstValue(payload, "generatedAt", "generated_at");
  const generatedDate = generatedAt ? new Date(generatedAt) : null;
  const freshness = generatedDate && !Number.isNaN(generatedDate.getTime())
    ? `آخرین داده سالن: ${generatedDate.toLocaleString("fa-IR", { dateStyle: "short", timeStyle: "short" })}`
    : "متصل به سامانه تخصصی راندمان";
  byId("hallSourceLabel").textContent = freshness;
  byId("hallModeBadge").textContent = "راندمان تخصصی";
  const integrationNote = byId("hallIntegrationNote");
  integrationNote.hidden = !payload.sample;
  integrationNote.textContent = payload.sample ? "داده‌های تخصصی سالن هنوز آزمایشی‌اند و نباید مبنای تصمیم نهایی قرار گیرند. پس از ثبت و تأیید اولین شیفت واقعی، این پیام خودکار حذف می‌شود." : "";
  byId("hallMetrics").innerHTML = [
    metricCard("راندمان کل سالن", formatNumber(efficiency), "درصد", "میانگین آخرین داده‌های تأییدشده", efficiency < 80 ? "danger" : "positive"),
    metricCard("ماشین فعال", formatNumber(activeMachines, true), "ماشین", `از ${formatNumber(machines.length, true)} ماشین پایش‌شده`),
    metricCard("توقف ثبت‌شده", formatNumber(totalStops, true), "مورد", "مجموع توقف‌های بازه فعلی", totalStops > 0 ? "warning" : ""),
    metricCard("نیازمند توجه", formatNumber(attention, true), "ماشین", topWeaver ? `بافنده برتر: ${topWeaver.name || topWeaver.weaver || "—"}` : "بررسی خودکار وضعیت", attention > 0 ? "warning" : "positive")
  ].join("");

  byId("hallTableTitle").textContent = "راندمان ماشین‌ها و بافنده‌ها";
  byId("hallTableHead").innerHTML = "<tr><th>ماشین</th><th>بافنده</th><th>راندمان</th><th>متر تولید</th><th>توقف</th><th>وضعیت</th></tr>";
  byId("hallMachineRows").innerHTML = machines.length
    ? machines.map((row) => {
        const status = String(row.status || "good").toLowerCase();
        return `<tr><td>ماشین ${escapeHTML(firstValue(row, "machine", "number", "id", "machineNumber") || "—")}</td><td>${escapeHTML(firstValue(row, "weaver", "weaverName", "weaver_name") || "—")}</td><td>${formatOptionalNumber(firstValue(row, "efficiency", "efficiency_percent"))}٪</td><td>${formatOptionalNumber(firstValue(row, "meters", "productionMeters", "production_meters"))}</td><td>${formatNumber(firstValue(row, "stops", "stop_count"), true)}</td><td>${statusPill(status)}</td></tr>`;
      }).join("")
    : emptyTableRow(6, "هنوز داده‌ای از ماشین‌های سالن دریافت نشده است.");

  const weaverCard = byId("hallWeaverCard");
  weaverCard.hidden = weavers.length === 0;
  byId("hallWeaverRows").innerHTML = weavers.length
    ? [...weavers]
        .sort((a, b) => valueNumber(firstValue(a, "rank")) - valueNumber(firstValue(b, "rank")))
        .map((row, index) => {
          const machineNumbers = list(firstValue(row, "machineNumbers", "machine_numbers", "machines"));
          const recovery = firstValue(row, "averageRecoveryMinutes", "average_recovery_minutes");
          return `<tr><td>${formatNumber(firstValue(row, "rank") || index + 1, true)}</td><td>${escapeHTML(firstValue(row, "name", "weaver") || "—")}</td><td>${escapeHTML(machineNumbers.length ? machineNumbers.join("، ") : "—")}</td><td>${formatOptionalNumber(firstValue(row, "efficiency", "score"))}٪</td><td>${formatOptionalNumber(firstValue(row, "performanceScore", "performance_score"))}</td><td>${recovery === null || recovery === undefined ? "—" : `${formatNumber(recovery)} دقیقه`}</td></tr>`;
        }).join("")
    : emptyTableRow(6, "داده‌ای برای مقایسه بافنده‌ها ثبت نشده است.");
}

function renderOperationalHallFallback() {
  const machines = list(state.operational?.machines);
  const totalMeter = machines.reduce((sum, row) => sum + valueNumber(row.total_meter), 0);
  const totalWaste = machines.reduce((sum, row) => sum + valueNumber(row.actual_waste), 0);
  const remainingValues = machines.map((row) => valueNumber(row.remaining_percent)).filter((value) => value >= 0);
  const averageRemaining = remainingValues.length ? remainingValues.reduce((a, b) => a + b, 0) / remainingValues.length : 0;
  const attention = machines.filter((row) => valueNumber(row.material_shortage) > 0 || valueNumber(row.remaining_percent) <= 10 || valueNumber(row.waste_percent_input) > 5).length;

  byId("hallSourceLabel").textContent = "متصل به داده‌های عملیاتی فعلی";
  byId("hallModeBadge").textContent = "پایش پایه";
  const note = byId("hallIntegrationNote");
  note.hidden = false;
  note.textContent = state.errors.hall || "تا تکمیل سامانه تخصصی راندمان، این تب از تولید، چله، مواد و ضایعات ثبت‌شده در بخش عملیاتی استفاده می‌کند. پس از آماده‌شدن سامانه تخصصی، راندمان بافنده، توقف و پارگی تار و پود بدون تغییر این برنامه به همین تب افزوده می‌شود.";
  byId("hallMetrics").innerHTML = [
    metricCard("ماشین دارای چله فعال", formatNumber(machines.length, true), "ماشین", "بر اساس آخرین چله متصل"),
    metricCard("تولید ثبت‌شده ماشین‌ها", formatNumber(totalMeter), "متر", "مجموع تولید چله‌های فعال"),
    metricCard("میانگین مواد باقی‌مانده", formatNumber(averageRemaining), "درصد", "تار و پود قابل مصرف", averageRemaining <= 15 ? "danger" : "positive"),
    metricCard("ضایعات ثبت‌شده", formatNumber(totalWaste), "کیلو", `${formatNumber(attention, true)} ماشین نیازمند توجه`, attention ? "warning" : "positive")
  ].join("");

  byId("hallTableTitle").textContent = "مواد، تولید و ضایعات ماشین‌ها";
  byId("hallWeaverCard").hidden = true;
  byId("hallTableHead").innerHTML = "<tr><th>ماشین</th><th>شماره چله</th><th>متر تولید</th><th>مواد باقی‌مانده</th><th>ضایعات</th><th>وضعیت</th></tr>";
  byId("hallMachineRows").innerHTML = machines.length
    ? machines.map((row) => {
        const shortage = valueNumber(row.material_shortage);
        const remaining = valueNumber(row.remaining_percent);
        const waste = valueNumber(row.waste_percent_input);
        const status = shortage > 0 || remaining <= 10 ? "danger" : waste > 5 || remaining <= 25 ? "warning" : "good";
        return `<tr><td>ماشین ${escapeHTML(row.machine || "—")}</td><td>${escapeHTML(row.shom_chelle || "—")}</td><td>${formatNumber(row.total_meter)}</td><td>${formatNumber(remaining)}٪</td><td>${formatNumber(waste)}٪</td><td>${statusPill(status)}</td></tr>`;
      }).join("")
    : emptyTableRow(6, state.errors.operational || "ماشین دارای چله فعال ثبت نشده است.");
}

function renderAlert(row) {
  const type = String(row?.type || row?.severity || "info").toLowerCase();
  const tone = type === "critical" || type === "danger" ? "critical" : type === "warning" ? "warning" : "info";
  return `<article class="alert-item ${tone}"><strong>${escapeHTML(row?.title || "پیام مدیریتی")}</strong><p>${escapeHTML(row?.message || row?.detail || "برای مشاهده جزئیات وارد بخش مربوط شوید.")}</p></article>`;
}

function statusPill(status) {
  const normalized = String(status || "good").toLowerCase();
  if (["danger", "critical", "alert", "stopped"].includes(normalized)) return '<span class="status-pill danger">اقدام فوری</span>';
  if (["warning", "watch"].includes(normalized)) return '<span class="status-pill warning">نیازمند بررسی</span>';
  return '<span class="status-pill">عادی</span>';
}

function emptyTableRow(columns, message) {
  return `<tr class="empty-row"><td colspan="${columns}">${escapeHTML(message)}</td></tr>`;
}

function setTab(tab, userInitiated = false) {
  if (!tabs.includes(tab)) tab = "operations";
  state.activeTab = tab;
  for (const name of tabs) {
    const button = document.querySelector(`[data-tab="${name}"]`);
    const panel = byId(`panel${name[0].toUpperCase()}${name.slice(1)}`);
    const active = name === tab;
    button.classList.toggle("active", active);
    button.setAttribute("aria-selected", String(active));
    panel.classList.toggle("active", active);
    panel.hidden = !active;
  }
  const url = new URL(window.location.href);
  url.searchParams.set("tab", tab);
  history.replaceState(null, "", url);
  if (userInitiated && state.tvMode) restartTVRotation();
}

function toggleTVMode() {
  state.tvMode = !state.tvMode;
  document.body.classList.toggle("tv-mode", state.tvMode);
  byId("tvButton").textContent = state.tvMode ? "خروج از حالت تلویزیون" : "حالت تلویزیون";
  localStorage.setItem("viora-executive-tv", state.tvMode ? "1" : "0");
  if (state.tvMode) {
    if (document.documentElement.requestFullscreen) document.documentElement.requestFullscreen().catch(() => {});
    restartTVRotation();
  } else {
    if (document.fullscreenElement && document.exitFullscreen) document.exitFullscreen().catch(() => {});
    clearInterval(state.tvTimer);
    state.tvTimer = null;
  }
}

function restartTVRotation() {
  clearInterval(state.tvTimer);
  if (!state.tvMode) return;
  state.tvTimer = setInterval(() => {
    const index = tabs.indexOf(state.activeTab);
    setTab(tabs[(index + 1) % tabs.length]);
  }, 25000);
}

function updateCountdown() {
  if (!state.nextRefreshAt) {
    byId("countdown").textContent = "";
    return;
  }
  const seconds = Math.max(0, Math.ceil((state.nextRefreshAt - Date.now()) / 1000));
  byId("countdown").textContent = `به‌روزرسانی بعدی: ${formatNumber(seconds, true)} ثانیه`;
}

function setupInstall() {
  window.addEventListener("beforeinstallprompt", (event) => {
    event.preventDefault();
    state.installPrompt = event;
  });
  window.addEventListener("appinstalled", () => {
    state.installPrompt = null;
    byId("installButton").textContent = "نصب شد";
  });
  byId("installButton").addEventListener("click", async () => {
    if (state.installPrompt) {
      state.installPrompt.prompt();
      await state.installPrompt.userChoice;
      state.installPrompt = null;
      return;
    }
    byId("installDialog").showModal();
  });
}

function setupEvents() {
  document.querySelectorAll("[data-tab]").forEach((button) => button.addEventListener("click", () => setTab(button.dataset.tab, true)));
  byId("refreshButton").addEventListener("click", () => loadDashboard(true));
  byId("tvButton").addEventListener("click", toggleTVMode);
  window.addEventListener("online", () => { setConnection("online", "اینترنت برقرار شد؛ در حال به‌روزرسانی"); loadDashboard(true); });
  window.addEventListener("offline", () => setConnection("offline", "اتصال اینترنت قطع شده است"));
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && state.lastUpdated && Date.now() - state.lastUpdated.getTime() > 120000) loadDashboard(true);
  });
}

async function start() {
  setupEvents();
  setupInstall();
  if ("serviceWorker" in navigator) navigator.serviceWorker.register("/executive/sw.js", { scope: "/executive/" }).catch(() => {});

  const query = new URLSearchParams(window.location.search);
  setTab(query.get("tab") || "operations");
  const requestedTV = query.get("tv") === "1" || localStorage.getItem("viora-executive-tv") === "1";
  if (requestedTV) {
    state.tvMode = true;
    document.body.classList.add("tv-mode");
    byId("tvButton").textContent = "خروج از حالت تلویزیون";
    restartTVRotation();
  }
  await loadDashboard();
  setInterval(() => {
    updateCountdown();
    if (state.nextRefreshAt && Date.now() >= state.nextRefreshAt && document.visibilityState === "visible") loadDashboard();
  }, 1000);
}

start();
