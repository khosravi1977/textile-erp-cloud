import React, { useEffect, useState } from 'react';

import { buildLedgerHealth } from './ledgerHealth.js';

const apiBase = window.ERP_FINANCIAL_API || import.meta.env.VITE_API_BASE || (
  window.location.port === '5173' ? `${window.location.protocol}//${window.location.hostname}:8081/api` : '/api/financial/api'
);
const portalFinancialSession = window.ERP_PORTAL_FINANCIAL_SESSION || '';

const money = value => Number(value || 0).toLocaleString('fa-IR');
const ratio = value => value === null || !Number.isFinite(value) ? 'بدون بدهی جاری' : Number(value).toLocaleString('fa-IR', { maximumFractionDigits: 2 });
const percent = value => value === null || !Number.isFinite(value) ? '-' : `${Number(value).toLocaleString('fa-IR', { maximumFractionDigits: 1 })}٪`;

async function ledgerRequest() {
  const request = () => fetch(`${apiBase}/accounting/reports`, {
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...(localStorage.getItem('financial-auth-token') ? { Authorization: `Bearer ${localStorage.getItem('financial-auth-token')}` } : {}),
    },
  });

  let response = await request();
  if (response.status === 401 && portalFinancialSession) {
    const session = await fetch(portalFinancialSession, { credentials: 'same-origin', headers: { Accept: 'application/json' } });
    const data = await session.json().catch(() => ({}));
    if (session.ok && data.token) {
      localStorage.setItem('financial-auth-token', data.token);
      response = await request();
    }
  }
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || 'دریافت اطلاعات رسمی دفترکل انجام نشد.');
  return data;
}

function Metric({ label, value, hint, tone = 'text-slate-100' }) {
  return <div className="rounded-xl border border-slate-700 bg-slate-900 p-4">
    <div className="text-xs text-slate-400">{label}</div>
    <div className={`mt-2 text-xl font-bold ${tone}`}>{value}</div>
    {hint && <div className="mt-2 text-xs leading-6 text-slate-500">{hint}</div>}
  </div>;
}

export default function LedgerFinancialHealthPage() {
  const [report, setReport] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const load = () => {
    setLoading(true);
    setError('');
    ledgerRequest().then(setReport).catch(err => setError(err.message || 'خطا در دریافت سلامت مالی')).finally(() => setLoading(false));
  };

  useEffect(load, []);

  if (loading) return <div className="rounded-xl border border-slate-700 bg-slate-900 p-6 text-slate-300">در حال محاسبه سلامت مالی از دفترکل رسمی...</div>;
  if (error) return <div className="rounded-xl border border-red-800 bg-red-950 p-6 text-red-100"><div className="font-bold">سلامت مالی در دسترس نیست</div><div className="mt-2 text-sm">{error}</div><button className="mt-4 rounded-md bg-red-800 px-4 py-2 text-sm" onClick={load}>تلاش دوباره</button></div>;

  const health = buildLedgerHealth(report || {});
  const statusClass = health.status === 'critical'
    ? 'border-red-700 bg-red-950 text-red-100'
    : health.status === 'warning'
      ? 'border-amber-700 bg-amber-950 text-amber-100'
      : 'border-emerald-700 bg-emerald-950 text-emerald-100';
  const statusLabel = health.status === 'critical' ? 'نیازمند اقدام فوری' : health.status === 'warning' ? 'نیازمند پایش' : 'وضعیت مناسب';

  return <div className="space-y-5">
    <div className={`rounded-xl border p-5 ${statusClass}`}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-bold">سلامت مالی مبتنی بر دفترکل</h2>
          <p className="mt-1 text-xs opacity-80">منبع اعداد: فقط اسناد Posted در دفاتر حسابداری؛ نه جمع‌های محلی صفحه.</p>
        </div>
        <div className="rounded-full border border-current px-4 py-2 text-sm font-bold">{statusLabel}</div>
      </div>
    </div>

    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
      <Metric label="درآمد ثبت‌شده" value={`${money(health.income)} تومان`} tone="text-emerald-300" />
      <Metric label="هزینه + بهای تمام‌شده" value={`${money(health.expense)} تومان`} tone="text-red-300" />
      <Metric label="سود/زیان خالص" value={`${money(health.netProfit)} تومان`} tone={health.netProfit >= 0 ? 'text-emerald-300' : 'text-red-300'} />
      <Metric label="حاشیه سود خالص" value={percent(health.profitMargin)} />
      <Metric label="نقد و بانک" value={`${money(health.cash)} تومان`} tone="text-blue-300" />
      <Metric label="مطالبات + اسناد دریافتنی" value={`${money(health.receivables)} تومان`} />
      <Metric label="موجودی ثبت‌شده" value={`${money(health.inventory)} تومان`} />
      <Metric label="بدهی جاری ثبت‌شده" value={`${money(health.currentLiabilities)} تومان`} tone="text-amber-300" />
      <Metric label="سرمایه در گردش" value={`${money(health.workingCapital)} تومان`} tone={health.workingCapital >= 0 ? 'text-emerald-300' : 'text-red-300'} />
      <Metric label="نسبت جاری" value={ratio(health.currentRatio)} hint="دارایی جاری دفترکل ÷ بدهی جاری دفترکل" />
      <Metric label="حقوق مالکانه تعدیل‌شده" value={`${money(health.adjustedEquity)} تومان`} hint="حقوق مالکانه ثبت‌شده + سود/زیان خالص" />
      <Metric label="بدهی به حقوق مالکانه" value={health.debtToEquity === null ? 'قابل محاسبه نیست' : ratio(health.debtToEquity)} />
    </div>

    <div className="rounded-xl border border-slate-700 bg-slate-900 p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-bold">کنترل تراز دفترکل</h3>
        <button onClick={load} className="rounded-md border border-slate-600 px-3 py-2 text-xs text-slate-200">بروزرسانی از دفترکل</button>
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <Metric label="جمع بدهکار" value={`${money(health.totalDebit)} تومان`} />
        <Metric label="جمع بستانکار" value={`${money(health.totalCredit)} تومان`} />
        <Metric label="اختلاف" value={`${money(health.imbalance)} تومان`} tone={health.balanced ? 'text-emerald-300' : 'text-red-300'} />
      </div>
    </div>

    <div className="rounded-xl border border-slate-700 bg-slate-900 p-5">
      <h3 className="mb-3 font-bold">هشدارهای حسابداری</h3>
      {health.alerts.length === 0
        ? <div className="rounded-lg border border-emerald-800 bg-emerald-950 p-4 text-sm text-emerald-100">هشدار بحرانی از نسبت‌ها و تراز دفترکل فعلی استخراج نشد.</div>
        : <div className="grid gap-3 md:grid-cols-2">{health.alerts.map((item, index) => <div key={`${item.title}-${index}`} className={`rounded-lg border p-4 ${item.severity === 'critical' ? 'border-red-800 bg-red-950 text-red-100' : 'border-amber-800 bg-amber-950 text-amber-100'}`}><div className="font-bold">{item.title}</div><div className="mt-2 text-sm leading-6 opacity-90">{item.message}</div></div>)}</div>}
    </div>
  </div>;
}
