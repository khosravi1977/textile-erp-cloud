import React, { useEffect, useMemo, useState } from 'react';

import { useRef } from 'react';
import QRCode from 'qrcode';
import { confirmMovementCounterparty, confirmedMovementCounterparty, movementCounterpartyLabel, movementNeedsCounterparty } from './counterparty.js';
import { isDateWithinInclusiveRange } from './dateRange.js';
import { isValidSayadId, issuedChecksForCheckbook, normalizeSayadId, validateCheckbookUpdate } from './checkbook.js';
import { expenseTraceId, linkedExpenseTraceId, mapOperationalExpense, matchesExpenseFilters, matchesExpenseTrace } from './expenseMapping.js';
import { formatTableValue, toPersianDigits } from './localization.js';
import { isMonetaryColumn, monetaryColumnTotals, parseLocalizedNumber } from './reportTotals.js';

const FINANCIAL_DEV_ORIGIN = `${window.location.protocol}//${window.location.hostname}:5173`;
const FINANCIAL_DEV_ORIGINS = new Set([
  'http://127.0.0.1:5173',
  'http://localhost:5173',
  FINANCIAL_DEV_ORIGIN,
]);
const API_BASE = window.ERP_FINANCIAL_API || import.meta.env.VITE_API_BASE || (
  window.location.port === '5173' ? `${window.location.protocol}//${window.location.hostname}:8081/api` : '/api/financial/api'
);
const PORTAL_FINANCIAL_SESSION = window.ERP_PORTAL_FINANCIAL_SESSION || '';
const MOBILE_APP_DOWNLOAD_URL = `${window.location.origin}/HesabYar.apk?v=1.0.3-production-20260803`;

const authHeaders = () => {

  const token = localStorage.getItem('financial-auth-token');

  return token ? { Authorization: `Bearer ${token}` } : {};

};

let portalFinancialRefreshPromise = null;

async function refreshPortalFinancialToken() {
  if (!PORTAL_FINANCIAL_SESSION) return null;
  if (!portalFinancialRefreshPromise) {
    portalFinancialRefreshPromise = fetch(PORTAL_FINANCIAL_SESSION, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
      .then(async res => {
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.token) {
          throw new Error(data.error || 'Portal financial session is not available');
        }
        localStorage.setItem('financial-auth-token', data.token);
        localStorage.setItem('financial-auth-profile', JSON.stringify(buildSessionProfile(data.token, data)));
        return data.token;
      })
      .finally(() => {
        portalFinancialRefreshPromise = null;
      });
  }
  return portalFinancialRefreshPromise;
}



const pages = [
  { id: 'dashboard', label: 'داشبورد' },
  { id: 'financialHealth', label: 'سلامت مالی' },
  { id: 'initialData', label: 'اطلاعات اولیه' },
  { id: 'operational', label: 'داده های عملیاتی' },
  { id: 'incomingInvoices', label: 'فاکتور ورود' },
  { id: 'chelleIncomingInvoices', label: 'ورود چله' },
  { id: 'yarnOutInvoices', label: 'خروج نخ' },
  { id: 'invoices', label: 'فاکتور مالی' },
  { id: 'inventory', label: 'انبار و نخ' },
  { id: 'costs', label: 'هزینه ها' },
  { id: 'receivableDocs', label: 'اسناد دریافتی' },
  { id: 'payableDocs', label: 'اسناد پرداختی' },
  { id: 'bankCash', label: 'بانک و صندوق' },
  { id: 'accounting', label: 'دفاتر و تراز' },
  { id: 'reports', label: 'گزارشات' },
  { id: 'taxReports', label: 'گزارش مالیاتی' },
  { id: 'credit', label: 'اعتبارسنجی' },
  { id: 'advisor', label: 'تحلیل و مشاور هوشمند' },
  { id: 'telegramReports', label: 'گزارش‌های تلگرام' },
  { id: 'mobileApp', label: 'اتصال به اپ حسابیار' },
];

const fullPageAccess = pages.map(page => page.id);

function decodeJwtPayload(token) {
  try {
    const [, payload] = String(token || '').split('.');
    if (!payload) return {};
    const normalized = payload.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized + '='.repeat((4 - normalized.length % 4) % 4);
    return JSON.parse(window.atob(padded));
  } catch {
    return {};
  }
}

function normalizeAccessList(list) {
  const allowed = Array.isArray(list) && list.length ? list.filter(item => fullPageAccess.includes(item)) : fullPageAccess;
  if (allowed.includes('incomingInvoices') && !allowed.includes('chelleIncomingInvoices')) {
    allowed.push('chelleIncomingInvoices');
  }
  if (allowed.includes('reports') && !allowed.includes('telegramReports')) {
    allowed.push('telegramReports');
  }
  return [...new Set(allowed)];
}

function buildSessionProfile(token, sessionData = {}) {
  const claims = decodeJwtPayload(token);
  const canManageTeam = typeof sessionData.canManageTeam === 'boolean'
    ? sessionData.canManageTeam
    : Boolean(claims.can_manage_team);
  return {
    username: sessionData.username || claims.username || '',
    displayName: sessionData.displayName || sessionData.display_name || claims.display_name || '',
    company: sessionData.company || claims.company_name || '',
    projectKey: sessionData.projectKey || claims.project_key || '',
    portalRole: sessionData.portalRole || claims.portal_role || claims.role || 'owner',
    canManageTeam,
    permissions: normalizeAccessList(sessionData.permissions || claims.permissions),
    portalLinked: Boolean(sessionData.projectKey || claims.project_key || sessionData.company || claims.company_name),
  };
}

async function copyText(value) {
  const text = String(value || '');
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const input = document.createElement('textarea');
  input.value = text;
  input.setAttribute('readonly', '');
  input.style.position = 'fixed';
  input.style.top = '-9999px';
  input.style.opacity = '0';
  document.body.appendChild(input);
  input.focus();
  input.select();
  document.execCommand('copy');
  document.body.removeChild(input);
}

async function portalApi(path, options = {}) {
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...(options.headers || {}),
    },
    ...options,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || 'Portal request failed');
  }
  return data;
}const tabs = [

  { id: 'outInvoices', label: 'فاکتور خروج', path: '/operational/out-invoices?limit=100' },

  { id: 'customers', label: 'مشتريان', path: '/operational/customers' },

  { id: 'kala', label: 'کالاها', path: '/operational/kala-items' },

  { id: 'yarn', label: 'نخ ها', path: '/operational/yarn-items' },

  { id: 'yarnIn', label: 'ورود نخ', path: '/operational/yarn-incoming?limit=100' },

  { id: 'yarnOut', label: 'خروج نخ', path: '/operational/yarn-outgoing?limit=100' },

  { id: 'miscIncoming', label: 'ورود قطعات', path: '/operational/misc-incoming?limit=100' },

  { id: 'spareParts', label: 'موجودي قطعات', path: '/operational/spare-parts-inventory?limit=100' },

  { id: 'expenses', label: 'هزينه هاي عملياتي', path: '/operational/expenses?limit=100' },

];



const money = value => Number(value || 0).toLocaleString('fa-IR');

const num = value => Number(value || 0).toLocaleString('fa-IR');

const today = () => new Date().toISOString().slice(0, 10);

const uid = prefix => `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;

const shortId = prefix => `${prefix}-${new Date().toISOString().slice(0, 10).replace(/-/g, '')}-${Math.random().toString(36).slice(2, 6).toUpperCase()}`;

function toJalali(date) {

  if (!date) return '-';

  const raw = String(date).trim();

  if (/^\d{4}\/\d{1,2}\/\d{1,2}/.test(raw)) {

    return raw.replace(/\d/g, d => '۰۱۲۳۴۵۶۷۸۹'[Number(d)]);

  }

  const d = new Date(raw);

  if (Number.isNaN(d.getTime())) return date;

  return new Intl.DateTimeFormat('fa-IR-u-ca-persian', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(d);

}

function jalaliSortKey(date) {
  const value = toJalali(date);
  if (!value || value === '-') return '';
  const latin = String(value).replace(/[۰-۹]/g, d => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(d)));
  const match = latin.match(/(1[34]\d{2})\D(\d{1,2})\D(\d{1,2})/);
  return match ? `${match[1]}${match[2].padStart(2, '0')}${match[3].padStart(2, '0')}` : '';
}

function addMonths(date, months) {

  const d = new Date(date || today());

  if (Number.isNaN(d.getTime())) return '';

  d.setMonth(d.getMonth() + Number(months || 0));

  return d.toISOString().slice(0, 10);

}

function sameMonthNext(date) { return sameMonth(date, 1); }

function customerPaymentSchedule(finance) {

  const byCustomer = [...new Set(finance.invoices.map(x => x.customer).filter(Boolean))];

  return byCustomer.map(customer => {

    const customerInvoices = finance.invoices.filter(x => x.customer === customer);

    const debt = customerInvoices.reduce((s, x) => s + invoiceDebt(x), 0);

    let legacyPlan;

    try { legacyPlan = JSON.parse(localStorage.getItem('textile-payment-plans') || '{}')[customer]; } catch { legacyPlan = undefined; }

    const personPlan = finance.paymentPlans?.[customer] || legacyPlan;

    const expected_cash = customerInvoices.reduce((s, x) => { const plan = personPlan || x.paymentTerms || { cashPercent: 30 }; return s + Math.round(invoiceDebt(x) * Number(plan.cashPercent ?? 30) / 100); }, 0);

    const expected_check = customerInvoices.reduce((s, x) => { const plan = personPlan || x.paymentTerms || { checkPercent: 70 }; return s + Math.round(invoiceDebt(x) * Number(plan.checkPercent ?? 70) / 100); }, 0);

    const latest = customerInvoices.slice().sort((a, b) => String(a.date).localeCompare(String(b.date))).pop();

    return {

      customer,

      debt,

      expected_cash,

      expected_check,

      expected_check_date: addMonths(latest?.date || today(), personPlan?.checkMonths ?? latest?.paymentTerms?.checkMonths ?? 3),

    };

  }).filter(x => x.debt > 0);

}

function paymentPlanFor(customer) {

  const plans = JSON.parse(localStorage.getItem('textile-payment-plans') || '{}');

  return plans[customer] || { cashPercent: 30, checkPercent: 70, checkMonths: 3 };

}



const paymentTypes = [

  { id: 'credit', label: 'نسيه' },

  { id: 'cash', label: 'نقدي' },

  { id: 'check', label: 'چک' },

  { id: 'barter_yarn', label: 'تهاتر نخ' },

  { id: 'barter_fabric', label: 'تهاتر پارچه' },

];



const expenseCategories = [

  'حقوق و دستمزد',

  'برق',

  'گاز',

  'آب',

  'حمل و نقل',

  'تعميرات',

  'مواد مصرفي',

  'هزينه بانکي',

  'اجاره',

  'بيمه',

  'ماليات و عوارض',

  'سربار توليد',

  'ساير',

];

const defaultExpenseGroups = [
  { name: 'حقوق', subgroups: ['حقوق ماهانه', 'اضافه‌کاری', 'پاداش', 'بیمه سهم کارفرما'] },
  { name: 'انرژی و قبوض', subgroups: ['برق', 'گاز', 'آب', 'اینترنت'] },
  { name: 'خرید', subgroups: ['مواد اولیه', 'قطعات', 'ملزومات', 'سایر'] },
  { name: 'حمل‌ونقل', subgroups: ['باربری', 'پیک', 'سوخت', 'سایر'] },
  { name: 'تعمیرات', subgroups: ['ماشین‌آلات', 'ساختمان', 'تجهیزات اداری', 'سایر'] },
  { name: 'درآمد', subgroups: ['فروش', 'خدمات', 'دریافت مشتری', 'سایر'] },
  { name: 'انتقال', subgroups: ['بین حساب‌ها', 'قرض', 'سرمایه', 'سایر'] },
];

const expenseGroup = row => row.group || row.title || 'سایر';
const expenseSubgroup = row => row.subgroup || row.title || 'سایر';
const expenseSourceLabel = row => row.source_type === 'mobile_sms' ? 'اپ موبایل' : row.source_type === 'operational_expense' ? 'عملیاتی' : 'ثبت وب‌اپ';



const columnLabels = {

  type: 'نوع',

  actionLink: 'اقدام پيشنهادي',

  budget_achievement: 'تحقق بودجه',

  expected_cash: 'نقد طبق قرار',

  expected_check: 'چک طبق قرار',

  expected_check_date: 'تاريخ چک طبق قرار',

  delay_days: 'روز تاخير',

  incoming_total: 'جمع فاکتور ورود',

  paid_out: 'پرداختي خروجي',

  payable_to_customer: 'مانده بستانکاري شخص',

  settlement: 'تسويه',

  inventory_type: 'نوع ورودي',

  assigned_debt: 'بدهي ناشي از واگذاري',

  assignedTo: 'واگذار شده به',

  forecast_amount: 'پيش بيني هزينه',

  forecast_months: 'افق ماه',

  from_location: 'از محل',

  invoiceDebt: 'مانده فاکتور',

  item_name: 'نام کالا',

  item_no: 'شماره کالا',

  monthly_average: 'ميانگين ماهانه',

  operation_type: 'نوع عمليات',

  person: 'شخص',

  part_name: 'نام قطعه',

  part_number: 'شماره فني',

  return_date: 'تاريخ برگشت',

  source: 'منبع',

  source_type: 'نوع منبع',

  group: 'گروه',

  subgroup: 'زیرگروه',

  status: 'وضعيت',

  to_location: 'به محل',

  account: 'حساب',

  code: 'کد حساب',

  amount: 'مبلغ',

  assigned_checks: 'چک واگذار شده',

  balance: 'مانده',

  balance_weight: 'مانده',

  condition_status: 'وضعيت قطعه',

  cost: 'بهاي تقريبي',

  count: 'تعداد',

  credit: 'بستانکار',

  customer: 'مشتري',

  customer_name: 'مشتري',

  date: 'تاريخ',

  debit: 'بدهکار',

  debt: 'مانده',

  debt_amount: 'مانده بدهي',

  description: 'شرح',

  direction: 'نوع گردش',

  doc_no: 'شماره سند',

  incoming_weight: 'ورود',

  invoice_no: 'شماره فاکتور',

  item: 'کالا',

  itemName: 'نام کالا',

  kind: 'نوع',

  gross_profit: 'سود ناخالص',

  margin_percent: 'درصد حاشيه',

  message: 'پيام',

  name: 'نام',

  nature: 'ماهیت',

  outgoing_weight: 'خروج',

  opening_type: 'نوع مانده',

  opening_balance: 'مانده افتتاحيه',

  opening_receivable: 'طلب افتتاحيه',

  opening_payable: 'بدهي افتتاحيه',

  owned_barter_value: 'ارزش تهاتر',

  owned_barter_weight: 'تهاتر مالکي',

  paid: 'پرداخت شده',

  payer: 'پرداخت کننده',

  party: 'طرف حساب',

  period: 'دوره',

  percent: 'درصد',

  previous_change: 'تغيير نسبت به دوره قبل',

  pricing_basis: 'مبناي قيمت',

  quantity: 'مقدار',

  sourceInvoice: 'فاکتور مرجع',

  title: 'عنوان',

  total: 'مبلغ کل',

  trackingNo: 'شماره رهگيري',

  unit_price: 'نرخ واحد',

  vendor_name: 'فروشنده',

  variance: 'انحراف',

  voucher: 'شماره سند',

  variance_percent: 'درصد انحراف',

  yarn_name: 'نام نخ',

};



function statusLabel(status) {

  return {

    open: 'باز',

    cleared: 'وصول شد',

    assigned: 'واگذار شد',

    paid: 'پرداخت شد',

    returned: 'مرجوع شده',

    bounced: 'برگشت‌خورده',

  }[status] || status || '-';

}



function Card({ children, className = '' }) {

  return <section className={`rounded-lg border border-slate-700 bg-slate-800 p-5 ${className}`}>{children}</section>;

}



function Field({ label, value, tone = '' }) {

  return (

    <div className="rounded-md border border-slate-700 bg-slate-900 p-3">

      <div className="text-xs text-slate-400">{label}</div>

      <div className={`mt-1 text-sm font-semibold ${tone || 'text-slate-100'}`}>{value || '-'}</div>

    </div>

  );

}



function TextInput(props) {

  return <input {...props} className={`rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-blue-500 ${props.className || ''}`} />;

}



function DateInput({ value, onChange, className = '', ...props }) {

  const [editing, setEditing] = useState(false);

  if (editing) {

    return <TextInput {...props} className={className} type="date" value={value || ''} onChange={onChange} autoFocus onBlur={() => setEditing(false)} />;

  }

  return <TextInput {...props} className={className} type="text" value={toJalali(value)} readOnly onFocus={() => setEditing(true)} onClick={() => setEditing(true)} />;

}



function SelectInput(props) {

  return <select {...props} className={`rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-blue-500 ${props.className || ''}`} />;

}



function PrimaryButton({ children, onClick, className = '', type = 'button', disabled = false }) {

  return <button type={type} disabled={disabled} className={`rounded-md bg-blue-600 px-4 py-2 text-sm font-bold text-white hover:bg-blue-500 disabled:cursor-not-allowed disabled:opacity-50 ${className}`} onClick={onClick}>{children}</button>;

}



function GhostButton({ children, onClick, disabled = false }) {

  return <button type="button" disabled={disabled} className="rounded-md border border-slate-600 px-3 py-2 text-xs font-bold text-slate-200 hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50" onClick={onClick}>{children}</button>;

}



function DangerButton({ children, onClick, disabled = false }) {

  return <button type="button" disabled={disabled} className="rounded-md bg-red-600 px-3 py-2 text-xs font-bold text-white hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-50" onClick={onClick}>{children}</button>;

}



function useLocalStorage(key, initialValue) {

  const [value, setValue] = useState(() => {

    try {

      const raw = localStorage.getItem(key);

      return raw ? JSON.parse(raw) : initialValue;

    } catch {

      return initialValue;

    }

  });

  useEffect(() => localStorage.setItem(key, JSON.stringify(value)), [key, value]);

  return [value, setValue];

}

async function apiJSON(path, options = {}) {
  const request = () => fetch(`${API_BASE}${path}`, {
    method: options.method || 'GET',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json', ...authHeaders(), ...(options.headers || {}) },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  let response = await request();
  if (response.status === 401 && PORTAL_FINANCIAL_SESSION) {
    await refreshPortalFinancialToken();
    response = await request();
  }
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(data.error || `HTTP ${response.status}`);
    error.status = response.status;
    error.data = data;
    throw error;
  }
  return data;
}

function useServerWorkspace(initialValue, enabled, writable = true) {
  const [value, setValue] = useState(initialValue);
  const [status, setStatus] = useState({ ready: false, saving: false, error: '', revision: 0 });
  const loadedRef = useRef(false);
  const revisionRef = useRef(0);
  const skipSaveRef = useRef(false);
  const valueRef = useRef(value);
  valueRef.current = value;

  useEffect(() => {
    if (!enabled) {
      loadedRef.current = false;
      revisionRef.current = 0;
      setValue(initialValue);
      setStatus({ ready: false, saving: false, error: '', revision: 0 });
      return;
    }
    let cancelled = false;
    setStatus(current => ({ ...current, ready: false, error: '' }));
    apiJSON('/workspace')
      .then(document => {
        if (cancelled) return;
        const serverState = document.state && typeof document.state === 'object' ? document.state : {};
        let next = { ...initialValue, ...serverState };
        if (!hasFinanceData(next)) {
          try {
            const legacy = JSON.parse(localStorage.getItem('textile-finance-v3') || 'null');
            if (hasFinanceData(legacy)) next = { ...initialValue, ...legacy };
          } catch {}
        }
        revisionRef.current = Number(document.revision || 0);
        skipSaveRef.current = true;
        setValue(next);
        loadedRef.current = true;
        setStatus({ ready: true, saving: false, error: '', revision: revisionRef.current });
      })
      .catch(error => {
        if (!cancelled) setStatus({ ready: false, saving: false, error: error.message || 'خطا در دریافت اطلاعات مالی', revision: 0 });
      });
    return () => { cancelled = true; };
  }, [enabled]);

  useEffect(() => {
    if (!enabled) return;
    const timer = window.setInterval(async () => {
      if (!loadedRef.current) return;
      try {
        const document = await apiJSON('/workspace');
        const revision = Number(document.revision || 0);
        if (revision <= revisionRef.current) return;
        revisionRef.current = revision;
        skipSaveRef.current = true;
        setValue({ ...initialValue, ...(document.state || {}) });
        setStatus({ ready: true, saving: false, error: '', revision });
      } catch {}
    }, 10000);
    return () => window.clearInterval(timer);
  }, [enabled]);

  useEffect(() => {
    if (!enabled || !writable || !loadedRef.current) return;
    if (skipSaveRef.current) {
      skipSaveRef.current = false;
      return;
    }
    const timer = setTimeout(async () => {
      setStatus(current => ({ ...current, saving: true, error: '' }));
      try {
        const document = await apiJSON('/workspace', {
          method: 'PUT',
          body: { state: valueRef.current, revision: revisionRef.current },
        });
        revisionRef.current = Number(document.revision || revisionRef.current);
        localStorage.removeItem('textile-finance-v3');
        setStatus({ ready: true, saving: false, error: '', revision: revisionRef.current });
      } catch (error) {
        if (error.status === 409 && error.data?.current) {
          const current = error.data.current;
          revisionRef.current = Number(current.revision || 0);
          skipSaveRef.current = true;
          setValue({ ...initialValue, ...(current.state || {}) });
          setStatus({ ready: true, saving: false, error: 'اطلاعات توسط کاربر دیگری تغییر کرده بود و آخرین نسخه سرور بارگذاری شد.', revision: revisionRef.current });
          return;
        }
        setStatus(current => ({ ...current, saving: false, error: error.message || 'ذخیره اطلاعات مالی ناموفق بود' }));
      }
    }, 650);
    return () => clearTimeout(timer);
  }, [enabled, writable, value]);

  return [value, setValue, status];
}



async function apiGet(path) {

  let res = await fetch(`${API_BASE}${path}`, { headers: authHeaders() });

  if (res.status === 401 && PORTAL_FINANCIAL_SESSION) {
    try {
      await refreshPortalFinancialToken();
      res = await fetch(`${API_BASE}${path}`, { headers: authHeaders() });
    } catch (err) {
      localStorage.removeItem('financial-auth-token');
      throw err;
    }
  }

  if (!res.ok) throw new Error(await res.text());

  return res.json();

}



async function apiGetSafe(path) {

  try {

    return await apiGet(path);

  } catch (err) {

    console.warn('API fallback', path, err.message);

    return { rows: [], error: err.message };

  }

}



function emptyFinance() {

  return {

    invoices: [],

    incomingInvoices: [],

    yarnOutInvoices: [],

    expenses: [],

    receivableDocs: [],

    payableDocs: [],

    checkbooks: [],

    accounts: [

      { id: 'cashbox-main', name: 'صندوق اصلي', type: 'صندوق', opening: 0 },

      { id: 'bank-main', name: 'بانک اصلي', type: 'بانک', opening: 0 },

    ],

    movements: [],

    ownedInventory: [],

    openingBalances: [],

    smsGroups: defaultExpenseGroups,

    smsBankSenders: [],

    mobileTransactions: [],

    journalEntries: [],

    fiscalPeriods: [],

    paymentPlans: {},

    accountingSettings: { defaultVatRate: 0 },

  };

}



function hasFinanceData(value) {

  if (!value) return false;

  return ['invoices', 'incomingInvoices', 'yarnOutInvoices', 'expenses', 'receivableDocs', 'payableDocs', 'movements', 'ownedInventory', 'openingBalances']

    .some(key => Array.isArray(value[key]) && value[key].length > 0);

}



function paidAmount(invoice) {

  return (invoice.payments || []).filter(p => p.type !== 'credit').reduce((s, p) => s + Number(p.amount || 0), 0);

}



function creditAmount(invoice) {

  return (invoice.payments || []).filter(p => p.type === 'credit').reduce((s, p) => s + Number(p.amount || 0), 0);

}



function invoiceDebt(invoice) {

  const explicitCredit = creditAmount(invoice);

  return explicitCredit || Math.max(Number(invoice.total || 0) - paidAmount(invoice), 0);

}



function sameMonth(date, offset = 0) {

  if (!date) return false;

  const now = new Date();

  const target = new Date(now.getFullYear(), now.getMonth() + offset, 1);

  const d = new Date(date);

  return !Number.isNaN(d.getTime()) && d.getFullYear() === target.getFullYear() && d.getMonth() === target.getMonth();

}



function accountBalance(account, movements) {

  return Number(account.opening || 0) + movements.reduce((sum, movement) => {
    const amount = Number(movement.amount || 0);
    if (movement.accountId === account.id) sum += movement.direction === 'in' ? amount : -amount;
    if (movement.transactionType === 'transfer' && movement.counterAccountId === account.id) sum += movement.direction === 'in' ? -amount : amount;
    return sum;
  }, 0);

}



function customerFinance(finance, customer) {

  const invoices = finance.invoices.filter(x => x.customer === customer);

  const openingRows = (finance.openingBalances || []).filter(x => x.customer === customer);

  const openingReceivable = openingRows.filter(x => x.type === 'receivable').reduce((s, x) => s + Number(x.amount || 0), 0);

  const openingPayable = openingRows.filter(x => x.type === 'payable').reduce((s, x) => s + Number(x.amount || 0), 0);

  const total = invoices.reduce((s, x) => s + Number(x.total || 0), 0);

  const paid = invoices.reduce((s, x) => s + paidAmount(x), 0);

  const cash = invoices.flatMap(x => x.payments || []).filter(p => p.type === 'cash').reduce((s, p) => s + Number(p.amount || 0), 0);

  const checks = invoices.flatMap(x => x.payments || []).filter(p => p.type === 'check').reduce((s, p) => s + Number(p.amount || 0), 0);

  const barter = invoices.flatMap(x => x.payments || []).filter(p => p.type === 'barter_yarn' || p.type === 'barter_fabric').reduce((s, p) => s + Number(p.amount || 0), 0);

  const assignedChecks = finance.receivableDocs.filter(x => x.assignedTo === customer).reduce((s, x) => s + Number(x.amount || 0), 0);

  const incomingInvoices = finance.incomingInvoices.filter(x => x.customer === customer);

  const yarnOutInvoices = (finance.yarnOutInvoices || []).filter(x => x.customer === customer);

  const financialIncomingInvoices = incomingInvoices.filter(x => !x.nonFinancial);

  const nonFinancialInventoryValue = incomingInvoices.filter(x => x.nonFinancial).reduce((s, x) => s + Number(x.amount || 0), 0);

  const incomingTotal = financialIncomingInvoices.reduce((s, x) => s + Number(x.amount || 0), 0);

  const yarnOutFinancialTotal = yarnOutInvoices.filter(x => x.outMode === 'sale' || x.outMode === 'barter').reduce((s, x) => s + Number(x.amount || 0), 0);

  const paidOut = financialIncomingInvoices.reduce((s, x) => s + (x.payments || []).filter(p => p.type !== 'credit').reduce((sum, p) => sum + Number(p.amount || 0), 0), 0);

  const payableToCustomer = financialIncomingInvoices.reduce((s, x) => s + Math.max(Number(x.amount || 0) - (x.payments || []).filter(p => p.type !== 'credit').reduce((sum, p) => sum + Number(p.amount || 0), 0), 0), 0);

  const manualCustomerReceipts = (finance.movements || []).filter(x => x.transactionType === 'customer_receipt' && !x.sourceInvoice && !x.sourceIncomingInvoice && confirmedMovementCounterparty(x) === customer).reduce((s, x) => s + Number(x.amount || 0), 0);

  const manualSupplierPayments = (finance.movements || []).filter(x => x.transactionType === 'supplier_payment' && !x.sourceInvoice && !x.sourceIncomingInvoice && confirmedMovementCounterparty(x) === customer).reduce((s, x) => s + Number(x.amount || 0), 0);

  const invoiceDebtTotal = invoices.reduce((s, x) => s + invoiceDebt(x), 0);

  const bouncedChecks = finance.receivableDocs
    .filter(x => x.customer === customer && (x.status === 'bounced' || x.status === 'returned'))
    .reduce((s, x) => s + Number(x.amount || 0), 0);

  const receivableBalance = Math.max(invoiceDebtTotal + yarnOutFinancialTotal + openingReceivable + bouncedChecks - manualCustomerReceipts, 0);

  const directAssignedChecks = finance.receivableDocs.filter(x => x.assignedTo === customer && !x.assignedIncomingInvoice).reduce((s, x) => s + Number(x.amount || 0), 0);

  const netBalance = receivableBalance - payableToCustomer - openingPayable + manualSupplierPayments + directAssignedChecks;

  return { invoices, incomingInvoices, openingRows, openingReceivable, openingPayable, total, paid, cash, checks, barter, bouncedChecks, assignedChecks, assignedDebt: 0, incomingTotal, paidOut, payableToCustomer, nonFinancialInventoryValue, yarnOutFinancialTotal, invoiceDebt: invoiceDebtTotal, debt: receivableBalance, netBalance };

}



function pendingOperationalCounts(finance, data) {

  const settledOutInvoices = new Set((finance.invoices || []).map(x => String(x.number)));

  const settledIncomingSources = new Set((finance.incomingInvoices || []).map(x => `${x.source_type || 'manual'}:${x.sourceId || x.id}`));

  const settledYarnOutSources = new Set((finance.yarnOutInvoices || []).map(x => `${x.source_type || 'manual'}:${x.sourceId || x.id}`));

  const settledExpenseSources = new Set((finance.expenses || []).map(x => `${x.source_type || 'manual'}:${x.sourceId || x.id}`));

  const outInvoices = (data.invoices || []).filter(x => !settledOutInvoices.has(String(x.shom_f_khor))).length;

  const yarnIn = (data.yarnIn || []).filter(x => !settledIncomingSources.has(`operational_yarn_in:${x.id}`)).length;

  const yarnOut = (data.yarnOut || []).filter(x => !settledYarnOutSources.has(`operational_yarn_out:${x.id}`)).length;

  const spareParts = (data.spareParts || []).filter(x => !settledIncomingSources.has(`operational_spare_part:${x.id}`)).length;

  const chelleIn = (data.chelleIn || []).filter(x => !settledIncomingSources.has(`operational_chelle_in:${x.id}`)).length;

  const expenses = (data.expenses || []).filter(x => !settledExpenseSources.has(`operational_expense:${x.id}`)).length;

  return { outInvoices, yarnIn, yarnOut, spareParts, chelleIn, expenses, total: outInvoices + yarnIn + yarnOut + spareParts + chelleIn + expenses };

}



function printSection(title, html) {

  const win = window.open('', '_blank', 'width=1100,height=800');

  if (!win) return;

  const template = document.createElement('template');

  template.innerHTML = String(html || '');

  template.content.querySelectorAll('script,style,iframe,object,embed,link,meta').forEach(node => node.remove());

  template.content.querySelectorAll('*').forEach(node => [...node.attributes].forEach(attribute => {

    const name = attribute.name.toLowerCase();

    const value = attribute.value.trim().toLowerCase();

    if (name.startsWith('on') || ((name === 'href' || name === 'src') && value.startsWith('javascript:'))) node.removeAttribute(attribute.name);

  }));

  template.content.querySelectorAll('table').forEach(table => {

    if (table.querySelector('tfoot')) return;

    const headers = [...table.querySelectorAll('thead tr:last-child th')];

    const bodyRows = [...table.querySelectorAll('tbody tr')];

    const lastRowFirstCell = bodyRows[bodyRows.length - 1]?.cells?.[0]?.textContent || '';

    if (!headers.length || !bodyRows.length || /^\s*جمع/.test(lastRowFirstCell)) return;

    const totals = headers.map((header, index) => {

      if (!isMonetaryColumn(header.textContent)) return null;

      const values = bodyRows.map(row => parseLocalizedNumber(row.cells[index]?.textContent)).filter(value => value !== null);

      return values.length ? { index, total: values.reduce((sum, value) => sum + value, 0) } : null;

    }).filter(Boolean);

    if (!totals.length) return;

    const totalIndexes = new Set(totals.map(item => item.index));

    const labelIndex = headers.findIndex((_, index) => !totalIndexes.has(index));

    const footer = document.createElement('tfoot');

    const row = document.createElement('tr');

    headers.forEach((_, index) => {

      const cell = document.createElement('td');

      const total = totals.find(item => item.index === index);

      if (total) cell.textContent = money(total.total);

      else if (index === (labelIndex >= 0 ? labelIndex : 0)) cell.textContent = 'جمع کل';

      row.appendChild(cell);

    });

    footer.appendChild(row);

    table.appendChild(footer);

  });

  win.document.write(`<!doctype html><html lang="fa" dir="rtl"><head><meta charset="UTF-8"><title>${escapePrintText(title)}</title><style>body{font-family:Tahoma;padding:24px;color:#111}table{width:100%;border-collapse:collapse;font-size:12px}th,td{border:1px solid #cbd5e1;padding:8px;text-align:right}th{background:#e5e7eb}tfoot td{background:#dcfce7;font-weight:900;border-top:2px solid #166534}</style></head><body><h1>${escapePrintText(title)}</h1>${template.innerHTML}<script>window.addEventListener('load',function(){setTimeout(function(){window.print()},350)})</script></body></html>`);

  win.document.close();

}

function escapePrintText(value) {

  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');

}

function excelXMLText(value) {

  return String(value ?? '')
    .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F]/g, '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');

}

function exportExcel(title, rows = [], columns = null, totals = null) {

  const safeRows = Array.isArray(rows) ? rows : [];

  const keys = columns?.length
    ? columns.map(column => Array.isArray(column) ? column : [column, columnLabels[column] || column])
    : [...new Set(safeRows.flatMap(row => Object.keys(row || {})))].map(key => [key, columnLabels[key] || key]);

  const cell = value => {

    const numeric = typeof value === 'number' && Number.isFinite(value);

    const normalized = value && typeof value === 'object' ? JSON.stringify(value) : value;

    return `<Cell><Data ss:Type="${numeric ? 'Number' : 'String'}">${excelXMLText(numeric ? value : normalized ?? '')}</Data></Cell>`;

  };

  const header = `<Row>${keys.map(([, label]) => `<Cell ss:StyleID="Header"><Data ss:Type="String">${excelXMLText(label)}</Data></Cell>`).join('')}</Row>`;

  const body = safeRows.map(row => `<Row>${keys.map(([key]) => cell(row?.[key])).join('')}</Row>`).join('');

  const automaticTotals = monetaryColumnTotals(safeRows, keys);

  const automaticByKey = new Map(automaticTotals.map(item => [item.key, item.total]));

  const shouldAddTotalRow = Boolean(totals) || automaticTotals.length > 0;

  const totalValue = key => totals && Object.prototype.hasOwnProperty.call(totals, key) ? totals[key] : automaticByKey.get(key);

  const totalKeys = new Set(keys.filter(([key]) => totalValue(key) !== undefined).map(([key]) => key));

  const totalLabelIndex = Math.max(0, keys.findIndex(([key]) => !totalKeys.has(key)));

  const totalRow = shouldAddTotalRow ? `<Row>${keys.map(([key], index) => totalKeys.has(key) ? cell(totalValue(key)) : index === totalLabelIndex ? cell(totals?.label || 'جمع کل') : cell('')).join('')}</Row>` : '';

  const xml = `<?xml version="1.0" encoding="UTF-8"?><?mso-application progid="Excel.Sheet"?><Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet"><Styles><Style ss:ID="Default" ss:Name="Normal"><Alignment ss:Horizontal="Right" ss:Vertical="Center"/><Font ss:FontName="Tahoma" ss:Size="11"/></Style><Style ss:ID="Header"><Font ss:FontName="Tahoma" ss:Size="11" ss:Bold="1"/><Interior ss:Color="#DDEBF7" ss:Pattern="Solid"/></Style></Styles><Worksheet ss:Name="${excelXMLText(String(title || 'گزارش').slice(0, 31))}"><Table>${header}${body}${totalRow}</Table><WorksheetOptions xmlns="urn:schemas-microsoft-com:office:excel"><DisplayRightToLeft/></WorksheetOptions></Worksheet></Workbook>`;

  const blob = new Blob([xml], { type: 'application/vnd.ms-excel;charset=utf-8' });

  const url = URL.createObjectURL(blob);

  const link = document.createElement('a');

  link.href = url;

  link.download = `${String(title || 'گزارش').replace(/[\\/:*?"<>|]/g, '-').trim() || 'گزارش'}.xls`;

  document.body.appendChild(link);

  link.click();

  link.remove();

  setTimeout(() => URL.revokeObjectURL(url), 1000);

}

async function buildQrImageUrl(value) {

  return QRCode.toDataURL(String(value || ''), { width: 360, margin: 2, errorCorrectionLevel: 'M' });

}



export default function App() {
  const [currentPage, setCurrentPage] = useState(() => {
    const requested = new URLSearchParams(window.location.search).get('page');
    return pages.some(page => page.id === requested) ? requested : 'dashboard';
  });
  const [isLoggedIn, setIsLoggedIn] = useState(Boolean(localStorage.getItem('financial-auth-token')));
  const [authBooting, setAuthBooting] = useState(Boolean(PORTAL_FINANCIAL_SESSION) && !localStorage.getItem('financial-auth-token'));
  const [sessionProfile, setSessionProfile] = useLocalStorage('financial-auth-profile', null);
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const writablePermissions = new Set(['initialData', 'incomingInvoices', 'yarnOutInvoices', 'invoices', 'inventory', 'costs', 'receivableDocs', 'payableDocs', 'bankCash', 'accounting']);
  const workspaceWritable = !sessionProfile?.portalLinked || (
    sessionProfile?.portalRole !== 'viewer' && (sessionProfile?.permissions || []).some(permission => writablePermissions.has(permission))
  );
  const [finance, setFinance, workspaceStatus] = useServerWorkspace(emptyFinance(), isLoggedIn && !authBooting, workspaceWritable);

  const safeFinance = { ...emptyFinance(), ...finance };
  const updateFinance = updater => setFinance(prev => updater({ ...emptyFinance(), ...prev }));
  const operationalState = useOperationalData();
  const pendingCounts = pendingOperationalCounts(safeFinance, operationalState.data);
  const [financialAlerts, setFinancialAlerts] = useState([]);

  useEffect(() => {
    if (!workspaceStatus.ready) return;
    apiJSON('/workspace/alerts')
      .then(result => setFinancialAlerts(Array.isArray(result.rows) ? result.rows : []))
      .catch(() => setFinancialAlerts([]));
  }, [workspaceStatus.ready, workspaceStatus.revision]);

  useEffect(() => {
    const chelleById = new Map((operationalState.data.chelleIn || []).map(row => [String(row.id), row]));
    if (!chelleById.size) return;

    setFinance(prev => {
      let changed = false;
      const incomingInvoices = (prev.incomingInvoices || []).map(invoice => {
        if (invoice.source_type !== 'operational_chelle_in') return invoice;
        const source = chelleById.get(String(invoice.sourceId || ''));
        const warper = String(source?.warper || '').trim();
        if (!warper || invoice.customer === warper) return invoice;
        changed = true;
        return {
          ...invoice,
          customer: warper,
          description: invoice.description?.includes('صاحب نخ')
            ? invoice.description
            : `${invoice.description || 'ورود چله عملیاتی'} | صاحب نخ ${source?.customer_name || '-'}`,
        };
      });
      return changed ? { ...prev, incomingInvoices } : prev;
    });
  }, [operationalState.data.chelleIn, setFinance]);

  const allowedPageIds = useMemo(
    () => normalizeAccessList(sessionProfile?.permissions, sessionProfile?.canManageTeam),
    [sessionProfile],
  );
  const allowedPages = useMemo(
    () => pages.filter(page => allowedPageIds.includes(page.id)),
    [allowedPageIds],
  );
  const activePage = allowedPages.find(page => page.id === currentPage) || allowedPages[0] || pages[0];

  const persistAuthSession = (token, sessionData = {}) => {
    localStorage.setItem('financial-auth-token', token);
    const profile = buildSessionProfile(token, sessionData);
    setSessionProfile(profile);
    setIsLoggedIn(true);
    setError('');
    return profile;
  };

  useEffect(() => {
    const token = localStorage.getItem('financial-auth-token');
    if (token && !sessionProfile) {
      setSessionProfile(buildSessionProfile(token));
    }
  }, []);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get('exportLocalStorage') === '1') {
      window.parent?.postMessage({
        type: 'TEXTILE_FINANCE_STORAGE',
        finance: localStorage.getItem('textile-finance-v3'),
        paymentPlans: localStorage.getItem('textile-payment-plans'),
      }, '*');
      return;
    }

    if (hasFinanceData(finance) || window.location.port === '5173') return;
    if (!['localhost', '127.0.0.1'].includes(window.location.hostname)) return;

    const frame = document.createElement('iframe');
    frame.src = `${FINANCIAL_DEV_ORIGIN}/financial/?exportLocalStorage=1`;
    frame.style.display = 'none';
    document.body.appendChild(frame);

    const receive = event => {
      if (!FINANCIAL_DEV_ORIGINS.has(event.origin)) return;
      if (event.data?.type !== 'TEXTILE_FINANCE_STORAGE') return;
      try {
        if (event.data.finance) {
          const parsed = JSON.parse(event.data.finance);
          if (hasFinanceData(parsed)) {
            localStorage.setItem('textile-finance-v3', event.data.finance);
            setFinance({ ...emptyFinance(), ...parsed });
          }
        }
        if (event.data.paymentPlans) {
          localStorage.setItem('textile-payment-plans', event.data.paymentPlans);
        }
      } catch (err) {
        console.warn('Could not migrate old financial localStorage', err);
      } finally {
        frame.remove();
        window.removeEventListener('message', receive);
      }
    };

    window.addEventListener('message', receive);
    const timer = setTimeout(() => {
      frame.remove();
      window.removeEventListener('message', receive);
    }, 5000);
    return () => {
      clearTimeout(timer);
      frame.remove();
      window.removeEventListener('message', receive);
    };
  }, []);

  useEffect(() => {
    if (!PORTAL_FINANCIAL_SESSION) {
      setAuthBooting(false);
      return;
    }

    let cancelled = false;
    const bootstrapPortalSession = async () => {
      try {
        const res = await fetch(PORTAL_FINANCIAL_SESSION, {
          credentials: 'same-origin',
          headers: { Accept: 'application/json' },
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.token) {
          throw new Error(data.error || 'Portal financial session is not available');
        }
        if (cancelled) return;
        persistAuthSession(data.token, data);
      } catch (err) {
        if (!cancelled) {
          localStorage.removeItem('financial-auth-token');
          localStorage.removeItem('financial-auth-profile');
          setSessionProfile(null);
          setIsLoggedIn(false);
          setError('');
        }
      } finally {
        if (!cancelled) {
          setAuthBooting(false);
        }
      }
    };

    bootstrapPortalSession();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!allowedPages.some(page => page.id === currentPage)) {
      setCurrentPage(allowedPages[0]?.id || 'dashboard');
    }
  }, [allowedPages, currentPage]);

  const handleLogin = async () => {
    try {
      const res = await fetch(`${API_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });
      const data = await res.json();
      if (!res.ok || !data.token) throw new Error(data.error || 'Login failed');
      persistAuthSession(data.token, data);
    } catch (err) {
      setError(err.message || 'Username or password is invalid');
    }
  };

  const handleLogout = () => {
    localStorage.removeItem('financial-auth-token');
    localStorage.removeItem('financial-auth-profile');
    setSessionProfile(null);
    setIsLoggedIn(false);
    if (window.ERP_PORTAL_PREFIX) window.location.assign('/module-logout?module=financial&login=1');
  };

  if (!isLoggedIn) {
    if (authBooting) {
      return (
        <div className="flex min-h-screen items-center justify-center bg-slate-950 px-4">
          <div className="w-full max-w-md rounded-lg border border-slate-700 bg-slate-900 p-8 text-center shadow-2xl">
            <h1 className="text-center text-2xl font-extrabold text-white">ERP نساجی</h1>
            <p className="mt-4 text-sm text-slate-400">در حال اتصال خودکار به بخش مالی...</p>
          </div>
        </div>
      );
    }

    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-950 px-4">
        <div className="w-full max-w-md rounded-lg border border-slate-700 bg-slate-900 p-8 shadow-2xl">
          <h1 className="text-center text-2xl font-extrabold text-white">ERP نساجی</h1>
          <p className="mt-2 text-center text-sm text-slate-400">ورود به بخش مالی و اتصال عملیاتی</p>
          <label className="mt-8 block text-sm text-slate-300">نام کاربری</label>
          <TextInput className="mt-2 w-full p-3" value={username} onChange={e => setUsername(e.target.value)} onKeyDown={e => e.key === 'Enter' && handleLogin()} />
          <label className="mt-4 block text-sm text-slate-300">رمز عبور</label>
          <TextInput className="mt-2 w-full p-3" type="password" value={password} onChange={e => setPassword(e.target.value)} onKeyDown={e => e.key === 'Enter' && handleLogin()} />
          {error && <p className="mt-3 text-sm text-red-300">{error}</p>}
          <button className="mt-6 w-full rounded-md bg-blue-600 p-3 font-bold text-white hover:bg-blue-500" onClick={handleLogin}>ورود</button>
        </div>
      </div>
    );
  }

  if (!workspaceStatus.ready) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-950 px-4 text-slate-100">
        <div className="w-full max-w-md rounded-lg border border-slate-700 bg-slate-900 p-8 text-center shadow-2xl">
          <h1 className="text-2xl font-extrabold">ERP نساجی</h1>
          <p className="mt-4 text-sm text-slate-300">در حال دریافت دفتر مالی شرکت از سرور...</p>
          {workspaceStatus.error && <><p className="mt-4 rounded-md border border-red-800 bg-red-950 p-3 text-sm text-red-200">{workspaceStatus.error}</p><button className="mt-4 rounded-md bg-blue-600 px-5 py-2 font-bold" onClick={() => window.location.reload()}>تلاش مجدد</button></>}
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100">
      <aside className="fixed right-0 top-0 h-screen w-64 overflow-y-auto border-l border-slate-800 bg-slate-900 p-4">
        <h2 className="mb-2 text-center text-xl font-extrabold text-white">ERP نساجی</h2>
        <div className="mb-5 rounded-md border border-slate-800 bg-slate-950/70 p-3 text-center text-xs text-slate-400">
          <div className="font-bold text-slate-200">{sessionProfile?.displayName || sessionProfile?.username || 'Active session'}</div>
          <div className="mt-1">{sessionProfile?.company || 'Textile ERP'}</div>
        </div>
        <nav className="space-y-1">
          {allowedPages.map(page => (
            <button key={page.id} className={`w-full rounded-md px-3 py-3 text-right text-sm transition ${activePage.id === page.id ? 'bg-blue-600 text-white' : 'text-slate-300 hover:bg-slate-800'}`} onClick={() => setCurrentPage(page.id)}>
              {page.label}
            </button>
          ))}
        </nav>
        <button className="mt-8 w-full rounded-md bg-slate-800 p-3 text-sm text-slate-200" onClick={handleLogout}>خروج و ورود کاربر دیگر</button>
      </aside>

      <main className="mr-64 p-7">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-extrabold">{activePage.label}</h1>
            <p className="mt-1 text-sm text-slate-400">Backend: Go روی پورت 8081 | Portal role: {sessionProfile?.portalRole || 'owner'}</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="rounded-full border border-emerald-700 bg-emerald-950 px-4 py-2 text-sm text-emerald-200">اتصال Go فعال</span>
            <span className={`rounded-full border px-4 py-2 text-sm ${workspaceStatus.error ? 'border-red-700 bg-red-950 text-red-200' : workspaceStatus.saving ? 'border-amber-700 bg-amber-950 text-amber-200' : 'border-emerald-700 bg-emerald-950 text-emerald-200'}`}>
              {workspaceStatus.error ? 'خطا در همگام‌سازی' : workspaceStatus.saving ? 'در حال ذخیره...' : `ذخیره شد | نسخه ${workspaceStatus.revision}`}
            </span>
          </div>
        </div>

        <OperationalNotification counts={pendingCounts} loading={operationalState.loading} onGo={setCurrentPage} />

        {financialAlerts.length > 0 && (
          <div className="mb-5 rounded-lg border border-red-800 bg-red-950/70 p-4">
            <div className="mb-3 flex items-center justify-between gap-3"><strong className="text-red-100">هشدارهای مالی نیازمند بررسی</strong><span className="rounded-full bg-red-900 px-3 py-1 text-xs text-red-100">{num(financialAlerts.length)} مورد</span></div>
            <div className="grid gap-2 md:grid-cols-2">{financialAlerts.slice(0, 6).map((item, index) => <div key={`${item.title}-${index}`} className="rounded-md border border-red-900 bg-slate-950 p-3 text-sm"><div className="font-bold text-red-200">{item.title}</div><div className="mt-1 text-slate-300">{item.message}</div></div>)}</div>
          </div>
        )}

        {currentPage === 'dashboard' && <Dashboard finance={safeFinance} />}
        {currentPage === 'financialHealth' && <FinancialHealthPage finance={safeFinance} />}
        {currentPage === 'initialData' && <InitialDataPage finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'operational' && <OperationalPage />}
        {currentPage === 'invoices' && <InvoicePage finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'incomingInvoices' && <IncomingInvoicePage finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'chelleIncomingInvoices' && <IncomingInvoicePage finance={safeFinance} setFinance={updateFinance} onlySource="chelle" />}
        {currentPage === 'yarnOutInvoices' && <YarnOutInvoicePage finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'inventory' && <InventoryPage finance={safeFinance} />}
        {currentPage === 'costs' && <CostsPage finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'receivableDocs' && <DocsPage kind="receivable" finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'payableDocs' && <DocsPage kind="payable" finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'bankCash' && <ProfessionalBankCashPage finance={safeFinance} setFinance={updateFinance} onGo={setCurrentPage} />}
        {currentPage === 'accounting' && <AccountingPage finance={safeFinance} setFinance={updateFinance} revision={workspaceStatus.revision} />}
        {currentPage === 'reports' && <ReportsPage finance={safeFinance} setFinance={updateFinance} />}
        {currentPage === 'taxReports' && <TaxReportPage finance={safeFinance} />}
        {currentPage === 'credit' && <CreditPage finance={safeFinance} />}
        {currentPage === 'advisor' && <AdvisorPage finance={safeFinance} />}
        {currentPage === 'telegramReports' && <TelegramReportsPage />}
        {currentPage === 'mobileApp' && <MobileAppPage />}
      </main>
    </div>
  );
}

function paymentValidationError(payments, total, label) {
  const active = (payments || []).map(p => ({ ...p, amount: Number(p.amount || 0) })).filter(p => p.amount > 0);
  const settled = active.reduce((sum, p) => sum + p.amount, 0);
  if (Math.abs(Number(total || 0) - settled) > Math.max(1, Number(total || 0) * 0.000001)) {
    return `جمع روش‌های تسویه ${label} باید دقیقاً با مبلغ فاکتور برابر باشد.`;
  }
  for (const payment of active) {
    if (payment.type === 'cash' && !payment.accountId) return `برای پرداخت نقدی ${label} بانک یا صندوق را انتخاب کنید.`;
    if (payment.type === 'check' && (!String(payment.checkNo || '').trim() || !payment.dueDate)) return `شماره و تاریخ سررسید چک ${label} الزامی است.`;
    if (payment.type === 'assign_receivable' && !payment.docId) return `چک دریافتی مورد واگذاری در ${label} انتخاب نشده است.`;
  }
  return '';
}

function MobileAppPage() {
  const [pairing, setPairing] = useState(null);
  const [qrImage, setQrImage] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const createPairing = async () => {
    setBusy(true); setError(''); setQrImage('');
    try {
      const mobileOrigin = window.ERP_MOBILE_ORIGIN || window.location.origin;
      const apiBase = new URL(API_BASE, mobileOrigin).toString().replace(/\/$/, '')
        .replace('textile.62-60-204-237.sslip.io', 'textile.62.60.204.237.nip.io');
      const result = await apiJSON('/mobile/pairing', { method: 'POST', body: { api_base: apiBase } });
      setPairing(result);
      setQrImage(await buildQrImageUrl(result.payload));
    } catch (err) {
      setError(err.message || 'ساخت کد اتصال انجام نشد');
    } finally { setBusy(false); }
  };

  return (
    <div className="grid gap-5 lg:grid-cols-2">
      <Card>
        <h3 className="text-xl font-black">اتصال امن حسابیار</h3>
        <p className="mt-3 text-sm leading-7 text-slate-300">این کد تا ۳۰ دقیقه و فقط برای اتصال اولیه یک گوشی معتبر است. پس از اتصال، گوشی بدون اسکن مجدد به همین شرکت متصل می‌ماند.</p>
        <a href={MOBILE_APP_DOWNLOAD_URL} className="mt-5 inline-flex items-center justify-center rounded-md border border-emerald-600 bg-emerald-950 px-4 py-3 text-sm font-bold text-emerald-100 hover:bg-emerald-900">
          دانلود/به‌روزرسانی حسابیار ۱٫۰٫۳
        </a>
        <a href={MOBILE_APP_DOWNLOAD_URL} target="_blank" rel="noreferrer" className="mr-3 mt-5 inline-flex items-center justify-center rounded-md border border-slate-500 bg-slate-800 px-4 py-3 text-sm font-bold text-slate-100 hover:bg-slate-700">
          اگر دانلود نشد، فایل را مستقیم باز کنید
        </a>
        <p className="mt-3 rounded-md border border-amber-700 bg-amber-950 p-3 text-sm leading-7 text-amber-100">
          اگر در گوشی آدرس <span dir="ltr">cooler.62-60-204-237.sslip.io</span> دیده می‌شود، ابتدا نسخه بالا را روی برنامه فعلی نصب کنید؛ سپس از داخل حسابیار، «بیشتر ← همگام‌سازی آنلاین» را باز و QR جدید را اسکن کنید.
        </p>
        <PrimaryButton className="mt-5" onClick={createPairing}>{busy ? 'در حال ساخت...' : 'ساخت QR اتصال گوشی'}</PrimaryButton>
        {error && <div className="mt-4"><ErrorBox message={error} /></div>}
        {pairing && <div className="mt-4 rounded-md border border-amber-700 bg-amber-950 p-3 text-sm text-amber-100">اعتبار کد تا {new Date(pairing.expires_at).toLocaleTimeString('fa-IR')}</div>}
        {pairing && <div className="mt-3 break-all rounded-md border border-emerald-800 bg-emerald-950 p-3 text-xs text-emerald-100">آدرس شبکه داخل QR: {window.ERP_MOBILE_ORIGIN || window.location.origin}</div>}
      </Card>
      <Card>
        {qrImage ? <div className="mx-auto max-w-sm rounded-2xl bg-white p-5"><img src={qrImage} alt="QR اتصال حسابیار" className="h-auto w-full" /></div> : <div className="flex min-h-72 items-center justify-center text-center text-slate-500">پس از ساخت کد، QR اتصال اینجا نمایش داده می‌شود.</div>}
      </Card>
    </div>
  );
}

function TelegramReportsPage() {
  const [settings, setSettings] = useState(null);
  const [pairing, setPairing] = useState(null);
  const [qrImage, setQrImage] = useState('');
  const [history, setHistory] = useState([]);
  const [recipients, setRecipients] = useState([]);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const load = async () => {
    try {
      const [config, deliveries, recipientResult] = await Promise.all([
        apiJSON('/telegram-reports/config'),
        apiJSON('/telegram-reports/history?limit=20'),
        apiJSON('/telegram-reports/recipients'),
      ]);
      setSettings(config);
      setHistory(deliveries.rows || []);
      setRecipients(recipientResult.rows || []);
      setError('');
    } catch (err) {
      setError(err.message || 'دریافت تنظیمات گزارش تلگرام انجام نشد');
    }
  };

  useEffect(() => {

    load();

    const timer = window.setInterval(load, 15000);

    return () => window.clearInterval(timer);

  }, []);

  const createPairing = async () => {
    setBusy('pair'); setError(''); setMessage(''); setQrImage('');
    try {
      const result = await apiJSON('/telegram-reports/pairing', { method: 'POST' });
      setPairing(result);
      setQrImage(await QRCode.toDataURL(result.deep_link, { width: 360, margin: 2, errorCorrectionLevel: 'M' }));
      setMessage('QR ساخته شد؛ آن را با دوربین گوشی اسکن کنید و در تلگرام دکمه Start را بزنید.');
    } catch (err) {
      setError(err.message || 'ساخت QR اتصال انجام نشد');
    } finally { setBusy(''); }
  };

  const save = async () => {
    setBusy('save'); setError(''); setMessage('');
    try {
      const updated = await apiJSON('/telegram-reports/config', {
        method: 'PUT',
        body: {
          enabled: Boolean(settings.enabled),
          alerts_enabled: Boolean(settings.alerts_enabled),
          daily_time: settings.daily_time || '20:00',
          weekly_enabled: Boolean(settings.weekly_enabled),
          weekly_day: Number(settings.weekly_day ?? 4),
          weekly_time: settings.weekly_time || '20:00',
          monthly_enabled: Boolean(settings.monthly_enabled),
          monthly_day: Number(settings.monthly_day || 1),
          monthly_time: settings.monthly_time || '20:00',
          accounting_sla_days: Number(settings.accounting_sla_days || 2),
          timezone: settings.timezone || 'Asia/Tehran',
        },
      });
      setSettings(updated);
      setMessage('تنظیمات ذخیره شد.');
    } catch (err) {
      setError(err.message || 'ذخیره تنظیمات انجام نشد');
    } finally { setBusy(''); }
  };

  const updateRecipient = async (recipient, changes) => {
    setBusy(`recipient-${recipient.id}`); setError(''); setMessage('');
    try {
      await apiJSON(`/telegram-reports/recipients?id=${recipient.id}`, {
        method: 'PUT',
        body: { ...recipient, ...changes },
      });
      await load();
      setMessage('دسترسی گیرنده به‌روزرسانی شد.');
    } catch (err) {
      setError(err.message || 'به‌روزرسانی گیرنده انجام نشد');
    } finally { setBusy(''); }
  };

  const removeRecipient = async (recipient) => {
    if (!window.confirm(`اتصال تلگرام «${recipient.chat_title || 'گیرنده'}» حذف شود؟`)) return;
    setBusy(`recipient-${recipient.id}`); setError(''); setMessage('');
    try {
      await apiJSON(`/telegram-reports/recipients?id=${recipient.id}`, { method: 'DELETE' });
      await load();
      setMessage('گیرنده حذف شد.');
    } catch (err) {
      setError(err.message || 'حذف گیرنده انجام نشد');
    } finally { setBusy(''); }
  };

  const sendTest = async () => {
    setBusy('test'); setError(''); setMessage('');
    try {
      await apiJSON('/telegram-reports/test', { method: 'POST' });
      setMessage('گزارش آزمایشی با موفقیت به تلگرام ارسال شد.');
      await load();
    } catch (err) {
      setError(err.message || 'ارسال آزمایشی انجام نشد');
    } finally { setBusy(''); }
  };

  if (!settings) {
    return <Card><div className="text-center text-slate-400">در حال دریافت تنظیمات تلگرام...</div>{error && <div className="mt-4"><ErrorBox message={error} /></div>}</Card>;
  }

  return (
    <div className="space-y-5">
      <div className="grid gap-5 lg:grid-cols-2">
        <Card>
          <div className="flex items-start justify-between gap-3">
            <div>
              <h3 className="text-xl font-black">گزارش‌های مدیریتی تلگرام</h3>
              <p className="mt-2 text-sm leading-7 text-slate-300">دو گزارش مستقل برای گیرندگان مجاز همین شرکت ارسال می‌شود: گزارش تولید و موجودی، و گزارش عملکرد حسابداری. هر دو گزارش می‌توانند روزانه، هفتگی و ماهانه باشند.</p>
            </div>
            <span className={`rounded-full px-3 py-1 text-xs font-bold ${settings.connected ? 'bg-emerald-950 text-emerald-200' : 'bg-amber-950 text-amber-200'}`}>
              {settings.connected ? `${settings.recipient_count || recipients.length} گیرنده متصل` : 'متصل نشده'}
            </span>
          </div>
          {settings.configured && !settings.available && <div className="mt-4 rounded-md border border-amber-700 bg-amber-950 p-3 text-sm leading-7 text-amber-100">بات روی سرور فعال است و در حال اتصال خودکار مجدد می‌باشد. ساخت QR و اتصال گیرنده در دسترس است؛ ارسال آزمایشی پس از سبزشدن ارتباط انجام می‌شود.</div>}
          {!settings.configured && <div className="mt-4 rounded-md border border-red-700 bg-red-950 p-3 text-sm leading-7 text-red-100">تنظیم محرمانه بات روی سرور کامل نیست و باید توسط مدیر زیرساخت تکمیل شود.</div>}
          <div className="mt-5 grid gap-4 md:grid-cols-2">
            <label className="text-sm text-slate-300">زمان ارسال روزانه
              <TextInput type="time" className="mt-2 w-full" value={settings.daily_time || '20:00'} onChange={e => setSettings(current => ({ ...current, daily_time: e.target.value }))} />
            </label>
            <label className="text-sm text-slate-300">منطقه زمانی
              <SelectInput className="mt-2 w-full" value={settings.timezone || 'Asia/Tehran'} onChange={e => setSettings(current => ({ ...current, timezone: e.target.value }))}>
                <option value="Asia/Tehran">تهران</option>
                <option value="UTC">UTC</option>
              </SelectInput>
            </label>
            <label className="text-sm text-slate-300">روز گزارش هفتگی
              <SelectInput className="mt-2 w-full" value={Number(settings.weekly_day ?? 4)} onChange={e => setSettings(current => ({ ...current, weekly_day: Number(e.target.value) }))}>
                <option value={6}>شنبه</option>
                <option value={0}>یکشنبه</option>
                <option value={1}>دوشنبه</option>
                <option value={2}>سه‌شنبه</option>
                <option value={3}>چهارشنبه</option>
                <option value={4}>پنجشنبه</option>
                <option value={5}>جمعه</option>
              </SelectInput>
            </label>
            <label className="text-sm text-slate-300">زمان گزارش هفتگی
              <TextInput type="time" className="mt-2 w-full" value={settings.weekly_time || '20:00'} onChange={e => setSettings(current => ({ ...current, weekly_time: e.target.value }))} />
            </label>
            <label className="text-sm text-slate-300">روز گزارش ماهانه
              <SelectInput className="mt-2 w-full" value={Number(settings.monthly_day || 1)} onChange={e => setSettings(current => ({ ...current, monthly_day: Number(e.target.value) }))}>
                {Array.from({ length: 28 }, (_, index) => <option key={index + 1} value={index + 1}>روز {index + 1} ماه</option>)}
              </SelectInput>
            </label>
            <label className="text-sm text-slate-300">زمان گزارش ماهانه
              <TextInput type="time" className="mt-2 w-full" value={settings.monthly_time || '20:00'} onChange={e => setSettings(current => ({ ...current, monthly_time: e.target.value }))} />
            </label>
            <label className="text-sm text-slate-300">مهلت استاندارد رسیدگی حسابداری
              <TextInput type="number" min="1" max="30" className="mt-2 w-full" value={settings.accounting_sla_days || 2} onChange={e => setSettings(current => ({ ...current, accounting_sla_days: Number(e.target.value) }))} />
              <span className="mt-1 block text-xs text-slate-500">تعداد روز مجاز از تاریخ سند عملیاتی تا تعیین‌تکلیف مالی</span>
            </label>
          </div>
          <div className="mt-4 rounded-md border border-blue-800 bg-blue-950/60 p-3 text-xs leading-6 text-blue-100">
            گزارش حسابداری، تعداد اسناد رسیدگی‌شده، میانگین و بیشترین تأخیر، درصد انجام در مهلت، اسناد معطل و عملکرد هر حسابدار را نشان می‌دهد. اگر سندی بیشتر از مهلت تعیین‌شده معطل بماند، روزانه یک هشدار مستقل نیز ارسال می‌شود. برای سوابق قدیمی فاصله تاریخ اسناد محاسبه می‌شود؛ از زمان فعال‌شدن این نسخه، ساعت دقیق و کاربر انجام‌دهنده نیز ثبت خواهد شد.
          </div>
          <label className="mt-5 flex items-center gap-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-sm">
            <input type="checkbox" checked={Boolean(settings.enabled)} onChange={e => setSettings(current => ({ ...current, enabled: e.target.checked }))} />
            ارسال خودکار گزارش روزانه فعال باشد
          </label>
          <label className="mt-3 flex items-center gap-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-sm">
            <input type="checkbox" checked={Boolean(settings.weekly_enabled)} onChange={e => setSettings(current => ({ ...current, weekly_enabled: e.target.checked }))} />
            ارسال خودکار گزارش هفتگی فعال باشد
          </label>
          <label className="mt-3 flex items-center gap-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-sm">
            <input type="checkbox" checked={Boolean(settings.monthly_enabled)} onChange={e => setSettings(current => ({ ...current, monthly_enabled: e.target.checked }))} />
            ارسال خودکار گزارش ماهانه فعال باشد
          </label>
          <label className="mt-3 flex items-center gap-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-sm">
            <input type="checkbox" checked={Boolean(settings.alerts_enabled)} onChange={e => setSettings(current => ({ ...current, alerts_enabled: e.target.checked }))} />
            هشدارهای مهم داخل گزارش نمایش داده شود
          </label>
          <div className="mt-5 flex flex-wrap gap-3">
            <PrimaryButton onClick={save}>{busy === 'save' ? 'در حال ذخیره...' : 'ذخیره تنظیمات'}</PrimaryButton>
            <GhostButton onClick={sendTest}>{busy === 'test' ? 'در حال ارسال...' : 'ارسال گزارش آزمایشی'}</GhostButton>
            <GhostButton disabled={!settings.configured || recipients.length >= 5 || busy === 'pair'} onClick={createPairing}>{busy === 'pair' ? 'در حال ساخت...' : settings.connected ? 'افزودن گیرنده جدید با QR' : 'ساخت QR اتصال'}</GhostButton>
          </div>
          {message && <div className="mt-4 rounded-md border border-emerald-800 bg-emerald-950 p-3 text-sm text-emerald-100">{message}</div>}
          {error && <div className="mt-4"><ErrorBox message={error} /></div>}
        </Card>
        <Card>
          {qrImage ? (
            <div className="text-center">
              <div className="mx-auto max-w-sm rounded-2xl bg-white p-5"><img src={qrImage} alt="QR اتصال امن بات تلگرام" className="h-auto w-full" /></div>
              <a href={pairing.deep_link} target="_blank" rel="noreferrer" className="mt-4 inline-block rounded-md bg-blue-600 px-5 py-3 text-sm font-bold text-white">باز کردن مستقیم در تلگرام</a>
              <p className="mt-3 text-xs text-slate-400">این لینک یک‌بارمصرف است و تا {new Date(pairing.expires_at).toLocaleTimeString('fa-IR')} اعتبار دارد.</p>
            </div>
          ) : (
            <div className="flex min-h-72 items-center justify-center text-center text-slate-500">برای اتصال امن، «ساخت QR اتصال» را بزنید. سپس QR را اسکن و Start را انتخاب کنید.</div>
          )}
        </Card>
      </div>
      <Card>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-black">گیرندگان گزارش</h3>
            <p className="mt-2 text-sm leading-7 text-slate-400">هر شخص با کد یک‌بارمصرف خودش متصل می‌شود. حداکثر پنج گیرنده برای هر شرکت قابل ثبت است.</p>
          </div>
          <span className="rounded-full bg-slate-800 px-3 py-1 text-xs text-slate-300">{recipients.length} از ۵</span>
        </div>
        <div className="mt-4 grid gap-3">
          {recipients.length ? recipients.map(recipient => (
            <div key={recipient.id} className="rounded-md border border-slate-700 bg-slate-900 p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="font-bold">{recipient.chat_title || 'گیرنده تلگرام'}</div>
                <div className="flex flex-wrap gap-2">
                  <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={Boolean(recipient.enabled)} onChange={e => updateRecipient(recipient, { enabled: e.target.checked })} /> فعال</label>
                  <GhostButton onClick={() => removeRecipient(recipient)} disabled={busy === `recipient-${recipient.id}`}>حذف اتصال</GhostButton>
                </div>
              </div>
              <div className="mt-4 flex flex-wrap gap-4 text-sm text-slate-300">
                <label className="flex items-center gap-2"><input type="checkbox" checked={Boolean(recipient.receive_daily)} onChange={e => updateRecipient(recipient, { receive_daily: e.target.checked })} /> روزانه</label>
                <label className="flex items-center gap-2"><input type="checkbox" checked={Boolean(recipient.receive_weekly)} onChange={e => updateRecipient(recipient, { receive_weekly: e.target.checked })} /> هفتگی</label>
                <label className="flex items-center gap-2"><input type="checkbox" checked={Boolean(recipient.receive_monthly)} onChange={e => updateRecipient(recipient, { receive_monthly: e.target.checked })} /> ماهانه</label>
                <label className="flex items-center gap-2"><input type="checkbox" checked={Boolean(recipient.receive_alerts)} onChange={e => updateRecipient(recipient, { receive_alerts: e.target.checked })} /> هشدارها</label>
              </div>
            </div>
          )) : <EmptyState />}
        </div>
      </Card>
      <Card>
        <h3 className="mb-4 text-lg font-black">سوابق گزارش‌های خودکار</h3>
        {history.length ? <GenericTable rows={history.map(row => ({
          date: toJalali(row.report_date),
          type: row.report_type === 'daily' ? 'روزانه' : row.report_type === 'weekly' ? 'هفتگی' : row.report_type === 'monthly' ? 'ماهانه' : row.report_type === 'accounting_alert' ? 'هشدار تأخیر حسابداری' : row.report_type === 'alert' ? 'هشدار' : row.report_type,
          status: row.status === 'sent' ? 'ارسال شد' : row.status === 'failed' ? 'ناموفق' : 'در حال ارسال',
          summary: row.summary || '-',
          error: row.error_message || '-',
        }))} /> : <EmptyState />}
      </Card>
    </div>
  );
}

function TeamAccessPage({ sessionProfile }) {
  const canGrantManager = sessionProfile?.portalRole === 'owner';
  const emptyForm = {
    contactName: '',
    username: '',
    password: '',
    accessRole: 'viewer',
    canManageTeam: false,
    allowFinancial: false,
    allowOperational: true,
    trialDays: '30',
    notes: '',
  };
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [flash, setFlash] = useState('');
  const [editingId, setEditingId] = useState(null);
  const [qrBusyId, setQrBusyId] = useState(null);
  const [qrPreview, setQrPreview] = useState(null);
  const [form, setForm] = useState(emptyForm);

  const loadRows = async () => {
    try {
      setLoading(true);
      const data = await portalApi('/api/portal/team');
      setRows(data.items || []);
      setError('');
    } catch (err) {
      setError(err.message || 'Could not load team access');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadRows();
  }, []);

  const stats = useMemo(() => {
    const active = rows.filter(row => row.is_active || row.isActive).length;
    const managers = rows.filter(row => (row.access_role || row.accessRole) === 'manager').length;
    const bothModules = rows.filter(row => Boolean(row.allow_financial || row.allowFinancial) && Boolean(row.allow_operational || row.allowOperational)).length;
    const expiringSoon = rows.filter(row => {
      const value = row.expires_at || row.expiresAt;
      const date = value ? new Date(value) : null;
      return date && !Number.isNaN(date.getTime()) && (date.getTime() - Date.now()) / 86400000 <= 7;
    }).length;
    return { active, managers, bothModules, expiringSoon, total: rows.length };
  }, [rows]);

  const resetForm = () => {
    setEditingId(null);
    setForm(emptyForm);
  };

  const startEdit = row => {
    setEditingId(row.id);
    setForm({
      contactName: row.contact_name || row.contactName || '',
      username: row.username || '',
      password: '',
      accessRole: row.access_role || row.accessRole || 'viewer',
      canManageTeam: Boolean(row.can_manage_team || row.canManageTeam),
      allowFinancial: typeof (row.allow_financial ?? row.allowFinancial) === 'boolean' ? Boolean(row.allow_financial ?? row.allowFinancial) : true,
      allowOperational: typeof (row.allow_operational ?? row.allowOperational) === 'boolean' ? Boolean(row.allow_operational ?? row.allowOperational) : false,
      trialDays: '30',
      notes: row.notes || '',
    });
    setFlash('');
  };

  const save = async event => {
    event.preventDefault();
    try {
      setSaving(true);
      setError('');
      const payload = {
        contactName: form.contactName,
        username: form.username,
        password: form.password,
        accessRole: form.accessRole,
        canManageTeam: canGrantManager ? form.canManageTeam : false,
        allowFinancial: form.allowFinancial,
        allowOperational: form.allowOperational,
        trialDays: Number(form.trialDays || 0),
        notes: form.notes,
      };
      if (!payload.allowFinancial && !payload.allowOperational) {
        throw new Error('حداقل یکی از دو بخش مالی یا عملیاتی باید فعال باشد.');
      }
      const path = editingId ? `/api/portal/team/${editingId}` : '/api/portal/team';
      const method = editingId ? 'PUT' : 'POST';
      await portalApi(path, { method, body: JSON.stringify(payload) });
      setFlash(editingId ? 'تغییرات ذخیره شد.' : 'عضو جدید ساخته شد و لینک آماده است.');
      resetForm();
      await loadRows();
    } catch (err) {
      setError(err.message || 'Could not save team access');
    } finally {
      setSaving(false);
    }
  };

  const removeRow = async row => {
    if (!window.confirm(`عضو ${row.contact_name || row.username} حذف شود؟ لینک ورود و دسترسی فعال او فوراً باطل خواهد شد.`)) return;
    try {
      await portalApi(`/api/portal/team/${row.id}`, { method: 'DELETE' });
      setFlash('عضو حذف و لینک ورود او باطل شد.');
      await loadRows();
    } catch (err) {
      setError(err.message || 'Delete failed');
    }
  };

  const rotateLink = async row => {
    if (!window.confirm(`برای ${row.contact_name || row.username} لینک جدید ساخته شود؟ لینک قبلی فوراً باطل خواهد شد.`)) return;
    try {
      await portalApi(`/api/portal/team/${row.id}/rotate-link`, { method: 'POST' });
      setFlash('لینک جدید ساخته شد و لینک قبلی باطل شد.');
      await loadRows();
    } catch (err) {
      setError(err.message || 'Could not replace access link');
    }
  };

  const toggleRow = async row => {
    try {
      await portalApi(`/api/portal/team/${row.id}/toggle`, { method: 'POST' });
      setFlash(row.is_active || row.isActive ? 'دسترسی غیرفعال شد.' : 'دسترسی دوباره فعال شد.');
      await loadRows();
    } catch (err) {
      setError(err.message || 'Toggle failed');
    }
  };

  const copyLink = async row => {
    try {
      await copyText(row.access_link || row.accessLink || '');
      setFlash(`لینک ${row.contact_name || row.username} کپی شد.`);
    } catch (err) {
      setError(err.message || 'Copy failed');
    }
  };

  const buildQrPayload = async row => {
    const link = row.access_link || row.accessLink || '';
    if (!link) throw new Error('لینک دسترسی برای این کارمند ثبت نشده است.');
    const qrImage = await buildQrImageUrl(link);
    return {
      row,
      link,
      qrImage,
      name: row.contact_name || row.contactName || row.username || 'کارمند',
      role: row.access_role || row.accessRole || 'viewer',
      expiresAt: row.expires_at || row.expiresAt || '',
      username: row.username || '-',
      password: row.password || 'در اولین ورود تعیین می‌شود',
      notes: row.notes || '',
    };
  };

  const printQrCard = payload => {
    const title = `برگه ورود ${payload.name}`;
    const html = `
      <div style="max-width:840px;border:1px solid #d6c5ad;border-radius:20px;padding:32px;background:linear-gradient(135deg,#fff8f1 0%,#f6eadc 100%);box-shadow:0 24px 60px rgba(54,35,24,0.12);">
        <div style="display:flex;justify-content:space-between;align-items:flex-start;gap:24px;flex-wrap:wrap;">
          <div style="flex:1 1 320px;">
            <div style="font-size:12px;color:#8a6d52;letter-spacing:1px;margin-bottom:10px;">TEXTILE ERP ACCESS PASS</div>
            <h2 style="margin:0 0 12px;color:#2f2119;font-size:28px;">${escapePrintText(payload.name)}</h2>
            <div style="display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin-top:18px;">
              <div style="padding:14px;border-radius:14px;background:#fffdf9;border:1px solid #e5d6c3;">
                <div style="font-size:11px;color:#8a6d52;">نقش</div>
                <div style="margin-top:6px;font-size:18px;color:#2f2119;font-weight:700;">${escapePrintText(payload.role)}</div>
              </div>
              <div style="padding:14px;border-radius:14px;background:#fffdf9;border:1px solid #e5d6c3;">
                <div style="font-size:11px;color:#8a6d52;">نام کاربری</div>
                <div style="margin-top:6px;font-size:18px;color:#2f2119;font-weight:700;">${escapePrintText(payload.username)}</div>
              </div>
              <div style="padding:14px;border-radius:14px;background:#fffdf9;border:1px solid #e5d6c3;">
                <div style="font-size:11px;color:#8a6d52;">رمز عبور موقت</div>
                <div dir="ltr" style="margin-top:6px;font-size:18px;color:#2f2119;font-weight:700;">${escapePrintText(payload.password)}</div>
              </div>
              <div style="padding:14px;border-radius:14px;background:#fffdf9;border:1px solid #e5d6c3;">
                <div style="font-size:11px;color:#8a6d52;">انقضای دسترسی</div>
                <div style="margin-top:6px;font-size:18px;color:#2f2119;font-weight:700;">${escapePrintText(toJalali(payload.expiresAt))}</div>
              </div>
              <div style="padding:14px;border-radius:14px;background:#fffdf9;border:1px solid #e5d6c3;">
                <div style="font-size:11px;color:#8a6d52;">راهنما</div>
                <div style="margin-top:6px;font-size:14px;color:#2f2119;font-weight:600;">QR را اسکن کنید یا لینک را در مرورگر باز کنید.</div>
              </div>
            </div>
            <div style="margin-top:18px;padding:16px;border-radius:16px;background:#2f2119;color:#f8ecdd;">
              <div style="font-size:11px;color:#d7bea4;">لینک ورود مستقیم</div>
              <div dir="ltr" style="margin-top:8px;word-break:break-all;font-size:14px;line-height:1.8;">${escapePrintText(payload.link)}</div>
            </div>
            <div style="margin-top:12px;padding:16px;border-radius:16px;background:#176b5b;color:white;">
              <div style="font-size:12px;font-weight:700;">دانلود اپ حسابیار اندروید</div>
              <div dir="ltr" style="margin-top:8px;word-break:break-all;font-size:13px;line-height:1.8;">${escapePrintText(MOBILE_APP_DOWNLOAD_URL)}</div>
            </div>
            ${payload.notes ? `<div style="margin-top:16px;padding:14px;border-radius:14px;background:#f8efe6;border:1px solid #e8d7c5;color:#5f4635;font-size:13px;"><strong>یادداشت:</strong> ${escapePrintText(payload.notes)}</div>` : ''}
          </div>
          <div style="width:280px;flex:0 0 280px;text-align:center;">
            <div style="padding:18px;border-radius:20px;background:#fffdf9;border:1px solid #e5d6c3;">
              <img src="${payload.qrImage}" alt="QR Code" style="width:100%;height:auto;display:block;" />
            </div>
            <div style="margin-top:14px;font-size:13px;color:#8a6d52;">این کد برای ورود سریع کارمند به پنل طراحی شده است.</div>
          </div>
        </div>
      </div>
    `;
    printSection(title, html);
  };

  const showQrPreview = async row => {
    try {
      setQrBusyId(row.id);
      setError('');
      const payload = await buildQrPayload(row);
      setQrPreview(payload);
      setFlash(`پیش‌نمایش QR برای ${payload.name} آماده شد.`);
    } catch (err) {
      setError(err.message || 'QR generation failed');
    } finally {
      setQrBusyId(null);
    }
  };

  const printEmployeeCard = async row => {
    try {
      setQrBusyId(row.id);
      setError('');
      const payload = await buildQrPayload(row);
      setQrPreview(payload);
      printQrCard(payload);
      setFlash(`برگه ورود ${payload.name} برای چاپ آماده شد.`);
    } catch (err) {
      setError(err.message || 'Print preparation failed');
    } finally {
      setQrBusyId(null);
    }
  };

  if (!sessionProfile?.canManageTeam) {
    return <Card><p className="text-sm text-slate-300">دسترسی شما اجازه مدیریت تیم را ندارد.</p></Card>;
  }

  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-5">
        <Field label="کل اعضا" value={num(stats.total)} />
        <Field label="فعال" value={num(stats.active)} tone="text-emerald-300" />
        <Field label="مدیرها" value={num(stats.managers)} tone="text-blue-300" />
        <Field label="دسترسی کامل" value={num(stats.bothModules)} tone="text-violet-300" />
        <Field label="رو به پایان" value={num(stats.expiringSoon)} tone="text-amber-300" />
      </div>

      {qrPreview && (
        <Card className="overflow-hidden border-amber-900/50 bg-gradient-to-br from-[#2f2119] via-[#4a3427] to-[#7b573c]">
          <div className="grid gap-6 xl:grid-cols-[1.15fr,320px]">
            <div className="space-y-4 text-amber-50">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p className="text-xs uppercase tracking-[0.35em] text-amber-200/80">Employee Access Pass</p>
                  <h3 className="mt-2 text-2xl font-black">{qrPreview.name}</h3>
                  <p className="mt-2 text-sm text-amber-100/80">این برگه برای ارسال دیجیتال یا پرینت و تحویل حضوری آماده است.</p>
                </div>
                <div className="rounded-full border border-amber-200/30 bg-white/10 px-3 py-1 text-xs font-bold text-amber-50">
                  {qrPreview.role}
                </div>
              </div>

              <div className="grid gap-3 md:grid-cols-3">
                <div className="rounded-2xl border border-amber-100/15 bg-white/10 p-4">
                  <div className="text-xs text-amber-100/70">نام کاربری</div>
                  <div className="mt-2 text-lg font-bold">{qrPreview.username}</div>
                </div>
                <div className="rounded-2xl border border-amber-100/15 bg-white/10 p-4">
                  <div className="text-xs text-amber-100/70">رمز عبور موقت</div>
                  <div className="mt-2 text-lg font-bold" dir="ltr">{qrPreview.password}</div>
                </div>
                <div className="rounded-2xl border border-amber-100/15 bg-white/10 p-4">
                  <div className="text-xs text-amber-100/70">انقضا</div>
                  <div className="mt-2 text-lg font-bold">{toJalali(qrPreview.expiresAt)}</div>
                </div>
                <div className="rounded-2xl border border-amber-100/15 bg-white/10 p-4">
                  <div className="text-xs text-amber-100/70">وضعیت</div>
                  <div className="mt-2 text-lg font-bold">آماده پرینت</div>
                </div>
              </div>

              <div className="rounded-2xl border border-amber-100/15 bg-black/10 p-4">
                <div className="text-xs text-amber-100/70">لینک مستقیم کارمند</div>
                <div className="mt-2 break-all rounded-xl bg-white/10 p-3 text-sm leading-7 text-amber-50" dir="ltr">
                  {qrPreview.link}
                </div>
              </div>

              {qrPreview.notes ? (
                <div className="rounded-2xl border border-amber-100/15 bg-white/10 p-4 text-sm text-amber-50/90">
                  <span className="font-bold">یادداشت:</span> {qrPreview.notes}
                </div>
              ) : null}

              <div className="flex flex-wrap gap-2">
                <PrimaryButton onClick={() => printQrCard(qrPreview)}>پرینت برگه ورود</PrimaryButton>
                <GhostButton onClick={() => copyText(qrPreview.link).then(() => setFlash(`لینک ${qrPreview.name} کپی شد.`)).catch(err => setError(err.message || 'Copy failed'))}>کپی لینک</GhostButton>
                <GhostButton onClick={() => setQrPreview(null)}>بستن پیش‌نمایش</GhostButton>
              </div>
            </div>

            <div className="flex items-center justify-center">
              <div className="w-full max-w-[280px] rounded-[28px] border border-amber-100/20 bg-[#fff8f1] p-5 shadow-2xl shadow-black/20">
                <img src={qrPreview.qrImage} alt={`QR code for ${qrPreview.name}`} className="h-auto w-full rounded-2xl bg-white p-3" />
                <div className="mt-4 text-center text-sm font-bold text-[#4a3427]">QR ورود مستقیم کارمند</div>
                <div className="mt-1 text-center text-xs leading-6 text-[#7b573c]">قابل اسکن با موبایل و مناسب چاپ روی برگه یا کارت.</div>
              </div>
            </div>
          </div>
        </Card>
      )}

      <div className="grid gap-6 xl:grid-cols-[420px,1fr]">
        <Card>
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h3 className="text-lg font-bold">{editingId ? 'ویرایش دسترسی کارمند' : 'ایجاد دسترسی کارمند'}</h3>
              <p className="mt-1 text-xs text-slate-400">لینک، نام کاربری و رمز موقت ساخته می‌شود؛ کارمند در اولین ورود رمز را تغییر می‌دهد.</p>
            </div>
            {editingId && <GhostButton onClick={resetForm}>انصراف</GhostButton>}
          </div>
          <form className="space-y-3" onSubmit={save}>
            <div>
              <label className="mb-2 block text-sm text-slate-300">نام کارمند</label>
              <TextInput className="w-full" value={form.contactName} onChange={e => setForm({ ...form, contactName: e.target.value })} required />
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <div>
                <label className="mb-2 block text-sm text-slate-300">نام کاربری داخلی</label>
                <TextInput className="w-full" value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} placeholder="اختیاری" />
              </div>
              <div>
                <label className="mb-2 block text-sm text-slate-300">رمز عبور داخلی</label>
                <TextInput className="w-full" type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} placeholder={editingId ? 'برای حفظ رمز خالی بگذارید' : 'اختیاری'} />
              </div>
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <div>
                <label className="mb-2 block text-sm text-slate-300">نقش</label>
                <SelectInput className="w-full" value={form.accessRole} onChange={e => setForm({ ...form, accessRole: e.target.value })}>
                  <option value="viewer">مشاهده‌گر</option>
                  <option value="accountant">حسابدار</option>
                  <option value="manager">مدیر اجرایی</option>
                </SelectInput>
              </div>
              <div>
                <label className="mb-2 block text-sm text-slate-300">روز دسترسی</label>
                <TextInput className="w-full" type="number" min="1" value={form.trialDays} onChange={e => setForm({ ...form, trialDays: e.target.value })} />
              </div>
            </div>
            <div>
              <label className="mb-2 block text-sm text-slate-300">بخش‌های قابل دسترسی</label>
              <SelectInput
                className="w-full"
                value={form.allowFinancial && form.allowOperational ? 'both' : (form.allowOperational ? 'operational' : 'financial')}
                onChange={e => setForm({
                  ...form,
                  allowFinancial: e.target.value === 'financial' || e.target.value === 'both',
                  allowOperational: e.target.value === 'operational' || e.target.value === 'both',
                })}
              >
                <option value="financial">فقط بخش مالی</option>
                <option value="operational">فقط بخش عملیاتی</option>
                <option value="both">بخش مالی و عملیاتی</option>
              </SelectInput>
            </div>
            {canGrantManager && (
              <label className="flex items-center gap-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-sm text-slate-200">
                <input type="checkbox" checked={form.canManageTeam} onChange={e => setForm({ ...form, canManageTeam: e.target.checked })} />
                اجازه ساخت و مدیریت کارمندهای دیگر را هم داشته باشد.
              </label>
            )}
            <div>
              <label className="mb-2 block text-sm text-slate-300">یادداشت</label>
              <TextInput className="w-full" value={form.notes} onChange={e => setForm({ ...form, notes: e.target.value })} placeholder="مثلا واحد فروش یا حسابداری" />
            </div>
            <div className="rounded-md border border-slate-700 bg-slate-900 p-3 text-xs text-slate-400">
              اگر نام کاربری یا رمز عبور را خالی بگذارید، سیستم خودش آن‌ها را می‌سازد و کارمند فقط با لینک وارد می‌شود.
            </div>
            <PrimaryButton className="w-full" type="submit">{saving ? 'در حال ذخیره...' : (editingId ? 'ذخیره تغییرات' : 'ایجاد لینک کارمند')}</PrimaryButton>
          </form>
          {flash && <p className="mt-4 text-sm text-emerald-300">{flash}</p>}
          {error && <ErrorBox message={error} />}
        </Card>

        <Card>
          <div className="mb-4 flex items-center justify-between gap-3">
            <div>
              <h3 className="text-lg font-bold">اعضای تیم و لینک‌های ورود</h3>
              <p className="mt-1 text-xs text-slate-400">برای هر عضو می‌توانید لینک را کپی کنید، QR بسازید و برگه ورود قابل چاپ تحویل دهید.</p>
            </div>
            <GhostButton onClick={loadRows}>بازخوانی</GhostButton>
          </div>
          {loading ? (
            <div className="rounded-md border border-slate-700 bg-slate-900 p-4 text-sm text-slate-300">در حال دریافت اطلاعات تیم...</div>
          ) : (
            <div className="space-y-3">
              {rows.map(row => {
                const isCurrent = row.is_current || row.isCurrent;
                const active = row.is_active || row.isActive;
                return (
                  <div key={row.id} className="rounded-lg border border-slate-700 bg-slate-900 p-4">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <div className="text-base font-bold text-white">{row.contact_name || row.contactName || row.username || 'بدون نام'}</div>
                        <div className="mt-1 text-xs text-slate-400">{row.username || 'نام کاربری پس از ساخت ثبت می‌شود'} | نقش: {row.access_role || row.accessRole || 'viewer'}</div>
                        <div className="mt-1 text-xs text-slate-500">
                          انقضا: {toJalali(row.expires_at || row.expiresAt)} {isCurrent ? '| حساب فعلی شما' : ''}
                        </div>
                        <div className="mt-2 flex flex-wrap gap-2">
                          {Boolean(row.allow_financial || row.allowFinancial) ? <span className="rounded-full bg-emerald-950 px-2 py-1 text-[11px] font-bold text-emerald-200">مالی</span> : null}
                          {Boolean(row.allow_operational || row.allowOperational) ? <span className="rounded-full bg-sky-950 px-2 py-1 text-[11px] font-bold text-sky-200">عملیاتی</span> : null}
                          {!Boolean(row.allow_financial || row.allowFinancial) && !Boolean(row.allow_operational || row.allowOperational) ? (
                            <span className="rounded-full bg-rose-950 px-2 py-1 text-[11px] font-bold text-rose-200">بدون ماژول فعال</span>
                          ) : null}
                        </div>
                      </div>
                      <div className={`rounded-full px-3 py-1 text-xs font-bold ${active ? 'bg-emerald-950 text-emerald-200' : 'bg-red-950 text-red-200'}`}>{active ? 'فعال' : 'غیرفعال'}</div>
                    </div>
                    <div className="mt-4 grid gap-3 lg:grid-cols-[1fr,auto]">
                      <div className="rounded-md border border-slate-700 bg-slate-950 p-3 text-xs text-slate-300" dir="ltr">{row.access_link || row.accessLink}</div>
                      <div className="flex flex-wrap gap-2">
                        <GhostButton onClick={() => copyLink(row)}>کپی لینک</GhostButton>
                        <a href={MOBILE_APP_DOWNLOAD_URL} download className="inline-flex items-center justify-center rounded-md border border-emerald-700 bg-emerald-950 px-3 py-2 text-xs font-bold text-emerald-200 hover:bg-emerald-900">دانلود اپ</a>
                        <GhostButton onClick={() => showQrPreview(row)}>{qrBusyId === row.id ? 'در حال ساخت QR...' : 'نمایش QR'}</GhostButton>
                        <GhostButton onClick={() => printEmployeeCard(row)}>{qrBusyId === row.id ? 'در حال آماده‌سازی...' : 'پرینت برگه'}</GhostButton>
                        {!isCurrent && <GhostButton onClick={() => startEdit(row)}>ویرایش</GhostButton>}
                        {!isCurrent && <GhostButton onClick={() => rotateLink(row)}>تعویض لینک</GhostButton>}
                        {!isCurrent && <GhostButton onClick={() => toggleRow(row)}>{active ? 'غیرفعال' : 'فعال'}</GhostButton>}
                        {!isCurrent && <DangerButton onClick={() => removeRow(row)}>حذف</DangerButton>}
                      </div>
                    </div>
                  </div>
                );
              })}
              {!rows.length && <div className="rounded-md border border-dashed border-slate-700 bg-slate-900 p-6 text-center text-sm text-slate-400">هنوز عضوی برای این شرکت ثبت نشده است.</div>}
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}

function useOperationalData() {

  const [data, setData] = useState({ customers: [], kala: [], yarn: [], invoices: [], yarnIn: [], chelleIn: [], yarnOut: [], expenses: [], miscIncoming: [], spareParts: [] });

  const [loading, setLoading] = useState(true);

  const [error, setError] = useState('');



  const refresh = () => {

    setLoading(true);

    Promise.all([

      apiGetSafe('/operational/customers'),

      apiGetSafe('/operational/kala-items'),

      apiGetSafe('/operational/yarn-items'),

      apiGetSafe('/operational/out-invoices?limit=200'),

      apiGetSafe('/operational/yarn-incoming?limit=200'),

      apiGetSafe('/operational/chelle-incoming?limit=200'),

      apiGetSafe('/operational/yarn-outgoing?limit=200'),

      apiGetSafe('/operational/expenses?limit=200'),

      apiGetSafe('/operational/misc-incoming?limit=200'),

      apiGetSafe('/operational/spare-parts-inventory?limit=200'),

    ])

      .then(([customers, kala, yarn, invoices, yarnIn, chelleIn, yarnOut, expenses, miscIncoming, spareParts]) => setData({

        customers: customers.rows || [],

        kala: kala.rows || [],

        yarn: yarn.rows || [],

        invoices: invoices.rows || [],

        yarnIn: yarnIn.rows || [],

        chelleIn: chelleIn.rows || [],

        yarnOut: yarnOut.rows || [],

        expenses: expenses.rows || [],

        miscIncoming: miscIncoming.rows || [],

        spareParts: spareParts.rows || [],

      }))

      .catch(err => setError(err.message))

      .finally(() => setLoading(false));

  };



  useEffect(() => {

    refresh();

    const id = setInterval(refresh, 15000);

    return () => clearInterval(id);

  }, []);



  return { data, loading, error, refresh };

}



function OperationalNotification({ counts, loading, onGo }) {

  if (!counts.total && !loading) return null;

  return (

    <div className="mb-5 rounded-md border border-amber-700 bg-amber-950 p-4 text-amber-50">

      <div className="flex flex-wrap items-center justify-between gap-3">

        <div>

          <div className="font-bold">اعلان عملياتي: {loading ? 'در حال بررسي...' : `${num(counts.total)} مورد در انتظار تعيين تکليف مالي`}</div>

          <div className="mt-2 flex flex-wrap gap-2 text-xs">

            <span className="rounded-full bg-slate-950 px-3 py-1">فاکتور خروج پارچه: {num(counts.outInvoices)}</span>

            <span className="rounded-full bg-slate-950 px-3 py-1">ورود نخ: {num(counts.yarnIn)}</span>

            <span className="rounded-full bg-slate-950 px-3 py-1">خروج نخ: {num(counts.yarnOut)}</span>

            <span className="rounded-full bg-slate-950 px-3 py-1">ورود چله: {num(counts.chelleIn)}</span>

            <span className="rounded-full bg-slate-950 px-3 py-1">هزینه عملیاتی: {num(counts.expenses)}</span>

            <span className="rounded-full bg-slate-950 px-3 py-1">قطعات: {num(counts.spareParts)}</span>

          </div>

        </div>

        <div className="flex gap-2">

          {counts.outInvoices > 0 && <button className="rounded-md bg-blue-600 px-4 py-2 text-sm font-bold text-white hover:bg-blue-500" onClick={() => onGo('invoices')}>تعيين تکليف فاکتور خروج</button>}

          {(counts.yarnIn + counts.yarnOut + counts.spareParts) > 0 && <button className="rounded-md bg-emerald-600 px-4 py-2 text-sm font-bold text-white hover:bg-emerald-500" onClick={() => onGo('incomingInvoices')}>تعيين تکليف نخ/قطعات</button>}

          {counts.chelleIn > 0 && <button className="rounded-md bg-cyan-600 px-4 py-2 text-sm font-bold text-white hover:bg-cyan-500" onClick={() => onGo('chelleIncomingInvoices')}>تعيين تکليف ورود چله</button>}

          {counts.expenses > 0 && <button className="rounded-md bg-rose-600 px-4 py-2 text-sm font-bold text-white hover:bg-rose-500" onClick={() => onGo('costs')}>بررسی هزینه‌ها</button>}

        </div>

      </div>

    </div>

  );

}



function Dashboard({ finance }) {

  const financeCustomers = [...new Set([...finance.invoices.map(x => x.customer), ...finance.incomingInvoices.map(x => x.customer), ...(finance.yarnOutInvoices || []).map(x => x.customer), ...finance.receivableDocs.map(x => x.customer), ...finance.receivableDocs.map(x => x.assignedTo), ...finance.payableDocs.map(x => x.customer), ...(finance.openingBalances || []).map(x => x.customer)].filter(Boolean))];

  const debts = financeCustomers.map(customer => ({ customer, debt: customerFinance(finance, customer).debt })).filter(x => x.debt > 0);

  const totalDebt = debts.reduce((s, x) => s + x.debt, 0);

  const payablesOpen = finance.payableDocs.filter(x => x.status !== 'paid');

  const receivablesOpen = finance.receivableDocs.filter(x => x.status !== 'cleared' && x.status !== 'assigned');

  const payable = payablesOpen.reduce((s, x) => s + Number(x.amount || 0), 0);

  const cashBalance = finance.accounts.reduce((s, a) => s + accountBalance(a, finance.movements), 0);

  const receivableThis = receivablesOpen.filter(x => sameMonth(x.dueDate, 0));

  const receivableNext = receivablesOpen.filter(x => sameMonthNext(x.dueDate));

  const expectedPayments = customerPaymentSchedule(finance);

  const payableThis = payablesOpen.filter(x => sameMonth(x.dueDate, 0));

  const payableNext = payablesOpen.filter(x => sameMonth(x.dueDate, 1));

  const lowCredit = financeCustomers

    .map(customer => ({ customer, ...customerFinance(finance, customer) }))

    .filter(x => x.debt > 500000000 || (x.debt > 0 && x.checks === 0));



  return (

    <div className="space-y-5">

      <div className="grid grid-cols-4 gap-4">

        <Field label="مانده بدهي مشتريان" value={`${money(totalDebt)} تومان`} tone="text-amber-300" />

        <Field label="اسناد پرداختي باز" value={`${money(payable)} تومان`} tone="text-red-300" />

        <Field label="موجودی بانک و صندوق" value={`${money(cashBalance)} تومان`} tone="text-emerald-300" />

        <Field label="هشدار اعتبار کم" value={`${num(lowCredit.length)} مشتري`} tone={lowCredit.length ? 'text-red-300' : 'text-emerald-300'} />

      </div>

      <div className="grid grid-cols-2 gap-5">

        <Card><h3 className="mb-4 font-bold">چک هاي دريافتي اين ماه</h3><DocsMiniTable rows={receivableThis} /></Card>

        <Card><h3 className="mb-4 font-bold">برنامه پرداخت بدهي مشتريان طبق قرار</h3><GenericTable rows={expectedPayments.map(x => ({ customer: x.customer, debt: x.debt, expected_cash: x.expected_cash, expected_check: x.expected_check, expected_check_date: x.expected_check_date }))} /></Card>

        <Card><h3 className="mb-4 font-bold">اسناد پرداختي اين ماه</h3><DocsMiniTable rows={payableThis} /></Card>

        <Card><h3 className="mb-4 font-bold">اسناد پرداختي ماه بعد</h3><DocsMiniTable rows={payableNext} /></Card>

        <Card><h3 className="mb-4 font-bold">مشتريان داراي هشدار اعتبار</h3><GenericTable rows={lowCredit.map(x => ({ customer: x.customer, debt_amount: x.debt, checks: x.checks, assigned_checks: x.assignedChecks }))} /></Card>

        <Card><h3 className="mb-4 font-bold">فاکتورهاي مالي اخير</h3><GenericTable rows={finance.invoices.slice(0, 8).map(x => ({ invoice_no: x.number, customer: x.customer, total: x.total, paid: paidAmount(x), debt: invoiceDebt(x) }))} /></Card>

      </div>

    </div>

  );

}



function buildFinancialHealth(finance) {

  const salesTotal = finance.invoices.reduce((s, x) => s + Number(x.total || 0), 0);

  const paidTotal = finance.invoices.reduce((s, x) => s + paidAmount(x), 0);

  const openingReceivables = (finance.openingBalances || []).filter(x => x.type === 'receivable').reduce((s, x) => s + Number(x.amount || 0), 0);

  const openingPayables = (finance.openingBalances || []).filter(x => x.type === 'payable').reduce((s, x) => s + Number(x.amount || 0), 0);

  const receivables = finance.invoices.reduce((s, x) => s + invoiceDebt(x), 0) + finance.receivableDocs.filter(x => x.assignedTo).reduce((s, x) => s + Number(x.amount || 0), 0) + openingReceivables;

  const expensesTotal = finance.expenses.reduce((s, x) => s + Number(x.amount || 0), 0);

  const payables = finance.payableDocs.filter(x => x.status !== 'paid').reduce((s, x) => s + Number(x.amount || 0), 0) + openingPayables;

  const inventoryValue = finance.ownedInventory.reduce((s, x) => s + Number(x.amount || 0), 0);

  const cashBalance = finance.accounts.reduce((s, a) => s + accountBalance(a, finance.movements), 0);

  const currentAssets = cashBalance + receivables + inventoryValue;

  const equity = Math.max(currentAssets - payables, 1);

  const netProfit = salesTotal - expensesTotal;

  const budgetProfit = Math.max(salesTotal * 0.22, netProfit + expensesTotal * 0.15, 1);

  const budgetSales = Math.max(salesTotal * 1.08, 1);

  const netProfitMargin = salesTotal ? Math.round((netProfit / salesTotal) * 1000) / 10 : 0;

  const operatingCashFlow = cashBalance + paidTotal - expensesTotal;

  const currentRatio = payables ? Math.round((currentAssets / payables) * 100) / 100 : currentAssets > 0 ? 9.99 : 0;

  const ebitda = netProfit;

  const debtToEquity = Math.round((payables / equity) * 100) / 100;

  const days = 30;

  const dso = Math.round(receivables / Math.max(salesTotal / days, 1));

  const dio = Math.round(inventoryValue / Math.max(expensesTotal / days, 1));

  const dpo = Math.round(payables / Math.max(expensesTotal / days, 1));

  const ccc = dso + dio - dpo;

  const revenueMap = finance.invoices.reduce((acc, row) => {

    const key = row.item || 'نامشخص';

    acc[key] = acc[key] || { item: key, total: 0 };

    acc[key].total += Number(row.total || 0);

    return acc;

  }, {});

  const revenueRows = Object.values(revenueMap).map(x => {

    const cost = salesTotal ? expensesTotal * (x.total / salesTotal) : 0;

    const profit = x.total - cost;

    return { item: x.item, total: Math.round(x.total), cost: Math.round(cost), gross_profit: Math.round(profit), margin_percent: x.total ? Math.round((profit / x.total) * 1000) / 10 : 0 };

  }).sort((a, b) => b.total - a.total);

  const expenseRows = Object.values(finance.expenses.reduce((acc, row) => {

    const key = expenseGroup(row);

    acc[key] = acc[key] || { title: key, amount: 0 };

    acc[key].amount += Number(row.amount || 0);

    return acc;

  }, {})).map(x => ({ ...x, percent: expensesTotal ? Math.round((x.amount / expensesTotal) * 1000) / 10 : 0, variance_percent: Math.round(((x.amount / Math.max(expensesTotal / Math.max(finance.expenses.length || 1, 1), 1)) - 1) * 100) })).sort((a, b) => b.amount - a.amount).slice(0, 10);

  const agingBuckets = [

    { period: 'مطالبات جاري', min: -99999, max: 0, amount: 0, customers: new Set() },

    { period: 'سررسید گذشته ۱ تا ۳۰ روز', min: 1, max: 30, amount: 0, customers: new Set() },

    { period: 'سررسید گذشته ۶۱ تا ۹۰ روز', min: 31, max: 60, amount: 0, customers: new Set() },

    { period: 'سررسید گذشته ۶۱ تا ۹۰ روز', min: 61, max: 90, amount: 0, customers: new Set() },

    { period: 'سررسید گذشته بیش از ۹۰ روز', min: 91, max: 99999, amount: 0, customers: new Set() },

  ];

  finance.invoices.forEach(row => {

    const debt = invoiceDebt(row);

    if (!debt) return;

    const d = new Date(row.date || today());

    const age = Number.isNaN(d.getTime()) ? 0 : Math.floor((new Date() - d) / 86400000);

    const bucket = agingBuckets.find(x => age >= x.min && age <= x.max) || agingBuckets[0];

    bucket.amount += debt;

    if (row.customer) bucket.customers.add(row.customer);

  });

  const agingRows = agingBuckets.map(x => ({ period: x.period, count: x.customers.size, amount: x.amount, percent: receivables ? Math.round((x.amount / receivables) * 1000) / 10 : 0 }));

  const staleInventory = finance.ownedInventory.filter(x => {

    const d = new Date(x.date || today());

    return !Number.isNaN(d.getTime()) && Math.floor((new Date() - d) / 86400000) > 365;

  });

  const staleValue = staleInventory.reduce((s, x) => s + Number(x.amount || 0), 0);

  const varianceRows = [

    { title: 'انحراف حجم فروش', amount: Math.round(salesTotal - budgetSales), variance_percent: Math.round(((salesTotal - budgetSales) / budgetSales) * 1000) / 10 },

    { title: 'انحراف قيمت فروش', amount: Math.round(netProfit - budgetProfit), variance_percent: Math.round(((netProfit - budgetProfit) / budgetProfit) * 1000) / 10 },

    { title: 'انحراف نرخ مواد/هزينه', amount: Math.round(-expensesTotal * 0.12), variance_percent: -12 },

    { title: 'انحراف کارايي', amount: Math.round(-expensesTotal * 0.08), variance_percent: -8 },

  ];

  const biggestVariance = varianceRows.slice().sort((a, b) => Math.abs(b.amount) - Math.abs(a.amount))[0];

  const alerts = [

    currentRatio < 1 && { severity: 'بحراني', message: 'هشدار: ريسک نقدينگي بالا - شرکت در آستانه ناتواني در پرداخت بدهي هاي کوتاه مدت است.' },

    netProfit < budgetProfit * 0.8 && { severity: 'بحراني', message: 'هشدار: شکاف عميق سودآوري - عملکرد واقعي به شدت از برنامه عقب است.' },

    dso > 60 && { severity: 'هشدار', message: 'توجه: وصول مطالبات کند شده و ريسک نقدشوندگي افزايش يافته است.' },

    staleValue > inventoryValue * 0.1 && { severity: 'هشدار', message: 'ارزش موجودي راکد از حد قابل قبول عبور کرده و بايد تعيين تکليف شود.' },

  ].filter(Boolean);

  const status = alerts.some(x => x.severity === 'بحراني') ? 'critical' : alerts.length ? 'warning' : 'healthy';

  const narrative = [

    salesTotal ? `در اين دوره ${money(salesTotal)} تومان درآمد ثبت شده و حاشيه سود خالص ${num(netProfitMargin)} درصد است.` : 'هنوز درآمد کافي براي تحليل سودآوري ثبت نشده است.',

    status === 'critical' ? 'بزرگ ترين ريسک فعلي در محدوده بحراني است و بايد قبل از توسعه فروش يا هزينه جديد کنترل شود.' : status === 'warning' ? 'وضعيت کلي قابل کنترل است اما چند شاخص نيازمند پيگيري مديريتي است.' : 'وضعيت مالي در محدوده سالم و تحت کنترل است.',

    biggestVariance ? `بزرگ ترين عامل انحراف فعلي «${biggestVariance.title}» با اثر ${money(biggestVariance.amount)} تومان است.` : '',

  ].filter(Boolean);

  const recommendations = [

    currentRatio < 1 ? '?. پرداخت هاي غيرضروري را متوقف و وصول فوري مطالبات سررسيد شده را پيگيري کنيد.' : '?. سطح نقدينگي فعلي را با برنامه وصول و پرداخت ماه آينده کنترل کنيد.',

    dso > 60 ? '?. براي مشتريان با بدهی بالای ۶۰ روز، سقف اعتبار و توقف فروش نسيه فعال شود.' : '?. روند وصول مطالبات را در همين سطح نگه داريد و براي فروش هاي جديد تاريخ تسويه قطعي ثبت کنيد.',

    revenueRows.some(x => x.margin_percent < 0) ? '?. قيمت فروش اقلام با حاشيه منفي را اصلاح يا فروش آن ها را متوقف کنيد.' : '?. اقلام سودآور را شناسايي و تمرکز فروش را روي کالاها و خدمات با حاشيه بالاتر ببريد.',

  ];

  return { salesTotal, expensesTotal, receivables, payables, inventoryValue, staleValue, currentAssets, status, alerts, kpis: { netProfitMargin, operatingCashFlow, currentRatio, ebitda, debtToEquity }, revenueRows, expenseRows, cash: { dso, dio, dpo, ccc }, agingRows, varianceRows, narrative, recommendations };

}



function kpiTone(value, good, warn) {

  if (good(value)) return 'text-emerald-300';

  if (warn(value)) return 'text-amber-300';

  return 'text-red-300';

}



function FinancialHealthPage({ finance }) {

  const health = buildFinancialHealth(finance);

  const statusClass = health.status === 'critical' ? 'border-red-700 bg-red-950 text-red-100' : health.status === 'warning' ? 'border-amber-700 bg-amber-950 text-amber-100' : 'border-emerald-700 bg-emerald-950 text-emerald-100';

  const statusText = health.alerts[0]?.message || 'وضعيت مالي در محدوده سالم و تحت کنترل است.';

  const cashRows = [{ title: 'DSO دوره وصول مطالبات', amount: health.cash.dso }, { title: 'DIO گردش موجودي', amount: health.cash.dio }, { title: 'DPO دوره پرداخت بدهي', amount: health.cash.dpo }, { title: 'CCC چرخه تبديل وجه نقد', amount: health.cash.ccc }];

  return (

    <div className="space-y-5">

      <div className={`rounded-lg border p-4 text-sm leading-7 ${statusClass}`}>{statusText}</div>

      <div className="grid grid-cols-5 gap-4">

        <Field label="حاشيه سود خالص" value={`${num(health.kpis.netProfitMargin)}%`} tone={kpiTone(health.kpis.netProfitMargin, x => x >= 20, x => x >= 10)} />

        <Field label="جریان نقد عملیاتی" value={`${money(health.kpis.operatingCashFlow)} تومان`} tone={health.kpis.operatingCashFlow >= 0 ? 'text-emerald-300' : 'text-red-300'} />

        <Field label="نسبت جاري" value={num(health.kpis.currentRatio)} tone={kpiTone(health.kpis.currentRatio, x => x >= 1.5, x => x >= 1)} />

        <Field label="EBITDA" value={`${money(health.kpis.ebitda)} تومان`} tone={health.kpis.ebitda >= 0 ? 'text-emerald-300' : 'text-red-300'} />

        <Field label="نسبت بدهي به حقوق صاحبان" value={num(health.kpis.debtToEquity)} tone={kpiTone(health.kpis.debtToEquity, x => x <= 1, x => x <= 2)} />

      </div>

      <div className="grid grid-cols-2 gap-5">

        <Card><h3 className="mb-4 font-bold">درآمد و سودآوري محصول/خدمت</h3><GenericTable rows={health.revenueRows} /></Card>

        <Card><h3 className="mb-4 font-bold">پارتو هزينه هاي عملياتي</h3><GenericTable rows={health.expenseRows} /></Card>

      </div>

      <div className="grid grid-cols-2 gap-5">

        <Card><h3 className="mb-4 font-bold">پايش سلامت نقدينگي</h3><GenericTable rows={cashRows} /><div className={`mt-4 rounded-md border p-3 text-sm ${health.cash.ccc > 90 ? 'border-red-700 bg-red-950 text-red-100' : 'border-slate-700 bg-slate-900 text-slate-200'}`}>{health.cash.ccc > 90 ? 'کشش منفي نقدينگي: سرمايه مدت زيادي در چرخه عمليات قفل شده است.' : 'چرخه نقدينگي در محدوده قابل کنترل است.'}</div></Card>

        <Card><h3 className="mb-4 font-bold">عمر مطالبات</h3><GenericTable rows={health.agingRows} /><div className="mt-4 rounded-md border border-slate-700 bg-slate-900 p-3 text-sm text-slate-200">{health.agingRows.filter(x => Number(x.percent || 0) >= 20).reduce((s, x) => s + Number(x.percent || 0), 0) > 20 ? 'خطر: بیش از ۲۰ درصد مطالبات در بازه‌های پرریسک قرار دارد.' : 'ترکيب مطالبات از نظر سني قابل کنترل است.'}</div></Card>

      </div>

      <div className="grid grid-cols-2 gap-5">

        <Card><h3 className="mb-4 font-bold">موجودي راکد و منسوخ</h3><div className="grid grid-cols-3 gap-3"><Field label="ارزش کل موجودی" value={`${money(health.inventoryValue)} تومان`} /><Field label="ارزش راکد" value={`${money(health.staleValue)} تومان`} tone={health.staleValue > health.inventoryValue * 0.1 ? 'text-red-300' : 'text-emerald-300'} /><Field label="درصد راکد" value={`${num(health.inventoryValue ? Math.round((health.staleValue / health.inventoryValue) * 1000) / 10 : 0)}%`} /></div></Card>

        <Card><h3 className="mb-4 font-bold">تحليل انحرافات بودجه</h3><GenericTable rows={health.varianceRows} /></Card>

      </div>

      <Card><h3 className="mb-4 font-bold">تفسير خودکار و اقدامات پيشنهادي</h3><div className="space-y-3">{health.narrative.map((x, i) => <div key={i} className="rounded-md border border-slate-700 bg-slate-900 p-3 text-sm leading-7 text-slate-100">{x}</div>)}{health.recommendations.map((x, i) => <div key={`r-${i}`} className="rounded-md border border-blue-800 bg-blue-950 p-3 text-sm leading-7 text-blue-100">{x}</div>)}</div></Card>

      <Card><h3 className="mb-4 font-bold">هشدارهاي هوشمند</h3><GenericTable rows={health.alerts.length ? health.alerts : [{ severity: 'عادي', message: 'هشدار بحراني فعالي وجود ندارد.' }]} /></Card>

    </div>

  );

}



function InitialDataPage({ finance, setFinance }) {

  const { data } = useOperationalData();

  const [balanceForm, setBalanceForm] = useState({ customer: '', type: 'receivable', amount: '', date: today(), description: '' });

  const [checkForm, setCheckForm] = useState({ kind: 'receivable', customer: '', amount: '', checkNo: '', dueDate: today(), dueJalali: '', bank: '', status: 'open' });

  const [accountForm, setAccountForm] = useState({ name: '', type: 'بانک', opening: '' });

  const [inventoryForm, setInventoryForm] = useState({ kindCode: 'yarn', itemName: '', quantity: '', unitPrice: '', amount: '', date: today(), customer: '', description: '' });

  const expenseGroups = finance.smsGroups?.length ? finance.smsGroups : defaultExpenseGroups;
  const [groupName, setGroupName] = useState('');
  const [selectedGroup, setSelectedGroup] = useState(expenseGroups[0]?.name || '');
  const [subgroupName, setSubgroupName] = useState('');

  const customers = [...new Set([

    ...data.customers.map(x => x.name),

    ...finance.invoices.map(x => x.customer),

    ...finance.incomingInvoices.map(x => x.customer),

    ...finance.receivableDocs.map(x => x.customer),

    ...finance.receivableDocs.map(x => x.assignedTo),

    ...finance.payableDocs.map(x => x.customer),

    ...(finance.openingBalances || []).map(x => x.customer),

  ].filter(Boolean))];

  const yarnItems = [...new Set([...data.yarn.map(x => x.name || x.yarn_name), ...finance.ownedInventory.filter(x => x.kindCode === 'yarn').map(x => x.itemName)].filter(Boolean))];

  const fabricItems = [...new Set([...data.kala.map(x => x.name || x.kala_name), ...finance.ownedInventory.filter(x => x.kindCode === 'fabric').map(x => x.itemName)].filter(Boolean))];

  const inventoryItems = inventoryForm.kindCode === 'yarn' ? yarnItems : fabricItems;

  const openingBalances = finance.openingBalances || [];

  const openingDocs = [...finance.receivableDocs, ...finance.payableDocs].filter(x => x.source === 'opening');

  const openingInventory = finance.ownedInventory.filter(x => x.source === 'opening');

  const openingAccounts = finance.accounts.filter(x => x.source === 'opening' || Number(x.opening || 0) !== 0);

  const balanceSummary = openingBalances.reduce((acc, row) => {

    if (row.type === 'receivable') acc.receivable += Number(row.amount || 0);

    if (row.type === 'payable') acc.payable += Number(row.amount || 0);

    return acc;

  }, { receivable: 0, payable: 0 });

  const checkTotal = openingDocs.reduce((s, x) => s + Number(x.amount || 0), 0);

  const inventoryTotal = openingInventory.reduce((s, x) => s + Number(x.amount || 0), 0);



  const addBalance = e => {

    e.preventDefault();

    if (!balanceForm.customer || !Number(balanceForm.amount || 0)) return;

    setFinance(prev => ({ ...prev, openingBalances: [{ id: uid('opb'), ...balanceForm, amount: Number(balanceForm.amount), source: 'opening' }, ...(prev.openingBalances || [])] }));

    setBalanceForm({ customer: '', type: 'receivable', amount: '', date: today(), description: '' });

  };

  const addCheck = e => {

    e.preventDefault();

    if (!checkForm.customer || !checkForm.checkNo || !Number(checkForm.amount || 0)) return;

    const key = checkForm.kind === 'receivable' ? 'receivableDocs' : 'payableDocs';

    const prefix = checkForm.kind === 'receivable' ? 'rch' : 'pch';

    const doc = { id: uid(prefix), customer: checkForm.customer, amount: Number(checkForm.amount), checkNo: checkForm.checkNo, dueDate: checkForm.dueDate, dueJalali: checkForm.dueJalali, bank: checkForm.bank, status: checkForm.status, source: 'opening' };

    setFinance(prev => ({ ...prev, [key]: [doc, ...(prev[key] || [])] }));

    setCheckForm({ kind: 'receivable', customer: '', amount: '', checkNo: '', dueDate: today(), dueJalali: '', bank: '', status: 'open' });

  };

  const addAccount = e => {

    e.preventDefault();

    if (!accountForm.name) return;

    setFinance(prev => ({ ...prev, accounts: [{ id: uid('acc'), ...accountForm, opening: Number(accountForm.opening || 0), source: 'opening' }, ...prev.accounts] }));

    setAccountForm({ name: '', type: 'بانک', opening: '' });

  };

  const addInventory = e => {

    e.preventDefault();

    const quantity = Number(inventoryForm.quantity || 0);

    const unitPrice = Number(inventoryForm.unitPrice || 0);

    const amount = Number(inventoryForm.amount || 0) || Math.round(quantity * unitPrice);

    if (!inventoryForm.itemName || !quantity) return;

    setFinance(prev => ({

      ...prev,

      ownedInventory: [{

        id: uid('opi'),

        source: 'opening',

        date: inventoryForm.date,

        customer: inventoryForm.customer,

        kindCode: inventoryForm.kindCode,

        kind: inventoryForm.kindCode === 'yarn' ? 'نخ افتتاحيه' : 'پارچه افتتاحيه',

        itemName: inventoryForm.itemName,

        quantity,

        unitPrice,

        amount,

        description: inventoryForm.description,

      }, ...prev.ownedInventory],

    }));

    setInventoryForm({ kindCode: 'yarn', itemName: '', quantity: '', unitPrice: '', amount: '', date: today(), customer: '', description: '' });

  };

  const removeOpeningBalance = id => setFinance(prev => ({ ...prev, openingBalances: (prev.openingBalances || []).filter(x => x.id !== id) }));

  const removeOpeningDoc = row => setFinance(prev => ({

    ...prev,

    receivableDocs: prev.receivableDocs.filter(x => x.id !== row.id),

    payableDocs: prev.payableDocs.filter(x => x.id !== row.id),

  }));

  const removeOpeningInventory = id => setFinance(prev => ({ ...prev, ownedInventory: prev.ownedInventory.filter(x => x.id !== id) }));

  const addExpenseGroup = e => {
    e.preventDefault();
    const name = groupName.trim();
    if (!name) return;
    setFinance(prev => ({ ...prev, smsGroups: [...(prev.smsGroups || defaultExpenseGroups), { name, subgroups: [] }].filter((row, index, all) => all.findIndex(x => x.name === row.name) === index) }));
    setSelectedGroup(name); setGroupName('');
  };

  const addExpenseSubgroup = e => {
    e.preventDefault();
    const name = subgroupName.trim();
    if (!selectedGroup || !name) return;
    setFinance(prev => ({ ...prev, smsGroups: (prev.smsGroups || defaultExpenseGroups).map(row => row.name === selectedGroup ? { ...row, subgroups: [...new Set([...(row.subgroups || []), name])] } : row) }));
    setSubgroupName('');
  };

  const removeOpeningData = () => {

    if (!window.confirm('فقط داده هاي افتتاحيه پاک شوند؟ فاکتورهاي مالي و عملياتي باقي مي مانند.')) return;

    setFinance(prev => ({

      ...prev,

      openingBalances: [],

      receivableDocs: prev.receivableDocs.filter(x => x.source !== 'opening'),

      payableDocs: prev.payableDocs.filter(x => x.source !== 'opening'),

      accounts: prev.accounts.filter(x => x.source !== 'opening').map(x => ({ ...x, opening: 0 })),

      ownedInventory: prev.ownedInventory.filter(x => x.source !== 'opening'),

    }));

  };

  const resetAllFinance = () => {

    if (!window.confirm('همه اطلاعات مالي ثبت شده پاک شود؟')) return;

    if (!window.confirm('اين کار برگشت پذير نيست. ادامه مي دهيد؟')) return;

    setFinance(() => emptyFinance());

  };



  return (

    <div className="space-y-5">

      <div className="grid grid-cols-4 gap-4">

        <Field label="طلب افتتاحیه مشتریان" value={`${money(balanceSummary.receivable)} تومان`} tone="text-emerald-300" />

        <Field label="بدهی افتتاحیه به اشخاص" value={`${money(balanceSummary.payable)} تومان`} tone="text-red-300" />

        <Field label="چک‌های افتتاحیه" value={`${money(checkTotal)} تومان`} tone="text-blue-300" />

        <Field label="موجودی افتتاحیه انبار مالی" value={`${money(inventoryTotal)} تومان`} tone="text-amber-300" />

      </div>

      <Card>

        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">

          <div><h3 className="font-bold">ثبت اطلاعات اوليه برنامه</h3><p className="mt-1 text-xs text-slate-400">اين داده ها در داشبورد، گزارشات، اعتبارسنجي، اسناد و موجودي مالي اثر مستقيم دارند.</p></div>

          <div className="flex gap-2"><DangerButton onClick={removeOpeningData}>پاک کردن اطلاعات افتتاحيه</DangerButton><DangerButton onClick={resetAllFinance}>پاک کردن کل اطلاعات مالي</DangerButton></div>

        </div>

      </Card>

      <Card>
        <h3 className="mb-2 font-bold">گروه و زیرگروه هزینه‌ها</h3>
        <p className="mb-4 text-xs text-slate-400">این فهرست با اپ موبایل همان شرکت همگام می‌شود.</p>
        <div className="grid gap-3 md:grid-cols-2">
          <form className="flex gap-2" onSubmit={addExpenseGroup}><TextInput className="flex-1" placeholder="نام گروه جدید" value={groupName} onChange={e => setGroupName(e.target.value)} /><PrimaryButton type="submit">افزودن گروه</PrimaryButton></form>
          <form className="flex gap-2" onSubmit={addExpenseSubgroup}><SelectInput value={selectedGroup} onChange={e => setSelectedGroup(e.target.value)}>{expenseGroups.map(row => <option key={row.name} value={row.name}>{row.name}</option>)}</SelectInput><TextInput className="flex-1" placeholder="نام زیرگروه جدید" value={subgroupName} onChange={e => setSubgroupName(e.target.value)} /><PrimaryButton type="submit">افزودن زیرگروه</PrimaryButton></form>
        </div>
        <div className="mt-4 grid gap-3 md:grid-cols-3">{expenseGroups.map(row => <div key={row.name} className="rounded-md border border-slate-700 bg-slate-900 p-3"><div className="font-bold text-blue-200">{row.name}</div><div className="mt-2 text-xs leading-6 text-slate-300">{(row.subgroups || []).join('، ') || 'بدون زیرگروه'}</div></div>)}</div>
      </Card>

      <div className="grid grid-cols-2 gap-5">

        <Card><h3 className="mb-4 font-bold">مانده افتتاحيه اشخاص</h3><form className="grid grid-cols-2 gap-3" onSubmit={addBalance}><SelectInput value={balanceForm.customer} onChange={e => setBalanceForm({ ...balanceForm, customer: e.target.value })}><option value="">انتخاب مشتري/شخص</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><SelectInput value={balanceForm.type} onChange={e => setBalanceForm({ ...balanceForm, type: e.target.value })}><option value="receivable">طلب از مشتري</option><option value="payable">بدهي به شخص</option></SelectInput><TextInput type="number" placeholder="مبلغ" value={balanceForm.amount} onChange={e => setBalanceForm({ ...balanceForm, amount: e.target.value })} /><DateInput value={balanceForm.date} onChange={e => setBalanceForm({ ...balanceForm, date: e.target.value })} /><TextInput className="col-span-2" placeholder="شرح" value={balanceForm.description} onChange={e => setBalanceForm({ ...balanceForm, description: e.target.value })} /><PrimaryButton className="col-span-2" type="submit">ثبت مانده افتتاحيه</PrimaryButton></form></Card>

        <Card><h3 className="mb-4 font-bold">چک هاي موجود اول دوره</h3><form className="grid grid-cols-2 gap-3" onSubmit={addCheck}><SelectInput value={checkForm.kind} onChange={e => setCheckForm({ ...checkForm, kind: e.target.value })}><option value="receivable">چک دريافتي موجود</option><option value="payable">چک پرداختي باز</option></SelectInput><SelectInput value={checkForm.customer} onChange={e => setCheckForm({ ...checkForm, customer: e.target.value })}><option value="">{checkForm.kind === 'receivable' ? 'دريافت از' : 'پرداخت به'}</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><TextInput type="number" placeholder="مبلغ" value={checkForm.amount} onChange={e => setCheckForm({ ...checkForm, amount: e.target.value })} /><TextInput placeholder="شماره چک" value={checkForm.checkNo} onChange={e => setCheckForm({ ...checkForm, checkNo: e.target.value })} /><TextInput placeholder="بانک" value={checkForm.bank} onChange={e => setCheckForm({ ...checkForm, bank: e.target.value })} /><TextInput placeholder="سررسيد شمسي" value={checkForm.dueJalali} onChange={e => setCheckForm({ ...checkForm, dueJalali: e.target.value })} /><DateInput value={checkForm.dueDate} onChange={e => setCheckForm({ ...checkForm, dueDate: e.target.value })} /><SelectInput value={checkForm.status} onChange={e => setCheckForm({ ...checkForm, status: e.target.value })}><option value="open">باز</option><option value="cleared">وصول شده</option><option value="paid">پرداخت شده</option></SelectInput><PrimaryButton className="col-span-2" type="submit">ثبت چک افتتاحيه</PrimaryButton></form></Card>

        <Card><h3 className="mb-4 font-bold">موجودي اوليه بانک و صندوق</h3><form className="grid grid-cols-2 gap-3" onSubmit={addAccount}><TextInput placeholder="نام حساب" value={accountForm.name} onChange={e => setAccountForm({ ...accountForm, name: e.target.value })} /><SelectInput value={accountForm.type} onChange={e => setAccountForm({ ...accountForm, type: e.target.value })}><option>بانک</option><option>صندوق</option></SelectInput><TextInput type="number" placeholder="موجودی اولیه" value={accountForm.opening} onChange={e => setAccountForm({ ...accountForm, opening: e.target.value })} /><PrimaryButton type="submit">ثبت حساب افتتاحيه</PrimaryButton></form></Card>

        <Card><h3 className="mb-4 font-bold">موجودي اوليه نخ و پارچه</h3><form className="grid grid-cols-2 gap-3" onSubmit={addInventory}><SelectInput value={inventoryForm.kindCode} onChange={e => setInventoryForm({ ...inventoryForm, kindCode: e.target.value, itemName: '' })}><option value="yarn">نخ</option><option value="fabric">پارچه</option></SelectInput><SelectInput value={inventoryForm.itemName} onChange={e => setInventoryForm({ ...inventoryForm, itemName: e.target.value })}><option value="">انتخاب نوع</option>{inventoryItems.map(x => <option key={x} value={x}>{x}</option>)}</SelectInput><TextInput type="number" placeholder="مقدار/وزن" value={inventoryForm.quantity} onChange={e => setInventoryForm({ ...inventoryForm, quantity: e.target.value, amount: '' })} /><TextInput type="number" placeholder="نرخ واحد" value={inventoryForm.unitPrice} onChange={e => setInventoryForm({ ...inventoryForm, unitPrice: e.target.value, amount: '' })} /><TextInput type="number" placeholder="مبلغ کل" value={inventoryForm.amount || (Number(inventoryForm.quantity || 0) * Number(inventoryForm.unitPrice || 0) || '')} onChange={e => setInventoryForm({ ...inventoryForm, amount: e.target.value })} /><DateInput value={inventoryForm.date} onChange={e => setInventoryForm({ ...inventoryForm, date: e.target.value })} /><SelectInput value={inventoryForm.customer} onChange={e => setInventoryForm({ ...inventoryForm, customer: e.target.value })}><option value="">مالک/طرف حساب اختياري</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><TextInput placeholder="شرح" value={inventoryForm.description} onChange={e => setInventoryForm({ ...inventoryForm, description: e.target.value })} /><PrimaryButton className="col-span-2" type="submit">ثبت موجودي افتتاحيه</PrimaryButton></form></Card>

      </div>

      <div className="grid grid-cols-2 gap-5">

        <Card><h3 className="mb-4 font-bold">ليست مانده هاي افتتاحيه</h3><OpeningBalanceTable rows={openingBalances} onDelete={removeOpeningBalance} /></Card>

        <Card><h3 className="mb-4 font-bold">ليست چک هاي افتتاحيه</h3><OpeningDocsTable rows={openingDocs} onDelete={removeOpeningDoc} /></Card>

        <Card><h3 className="mb-4 font-bold">حساب هاي داراي موجودي اوليه</h3><GenericTable rows={openingAccounts.map(x => ({ name: x.name, type: x.type, opening_balance: x.opening }))} /></Card>

        <Card><h3 className="mb-4 font-bold">موجودي افتتاحيه نخ و پارچه</h3><OpeningInventoryTable rows={openingInventory} onDelete={removeOpeningInventory} /></Card>

      </div>

    </div>

  );

}



function OperationalPage() {

  const [activeTab, setActiveTab] = useState('outInvoices');

  const [rows, setRows] = useState([]);

  const [loading, setLoading] = useState(false);

  const [error, setError] = useState('');

  const tab = useMemo(() => tabs.find(t => t.id === activeTab), [activeTab]);



  const loadRows = () => {

    setLoading(true);

    setError('');

    apiGet(tab.path).then(data => setRows(data.rows || [])).catch(err => setError(err.message)).finally(() => setLoading(false));

  };



  useEffect(() => {

    loadRows();

    const id = setInterval(loadRows, 15000);

    return () => clearInterval(id);

  }, [tab]);



  return (

    <div className="space-y-4">

      <Card><div className="flex flex-wrap gap-2">{tabs.map(t => <button key={t.id} className={`rounded-md px-3 py-2 text-sm ${activeTab === t.id ? 'bg-blue-600 text-white' : 'bg-slate-900 text-slate-300 hover:bg-slate-700'}`} onClick={() => setActiveTab(t.id)}>{t.label}</button>)}<GhostButton onClick={loadRows}>تازه سازي</GhostButton></div></Card>

      <Card>

        <div className="mb-4 flex items-center justify-between"><h3 className="font-bold">{tab.label}</h3><span className="text-sm text-slate-400">{loading ? 'در حال دريافت...' : `${num(rows.length)} رکورد`}</span></div>

        {error && <ErrorBox message={error} />}

        {!error && activeTab === 'outInvoices' && <InvoiceTable rows={rows} />}

        {!error && activeTab === 'expenses' && <GenericTable rows={rows.map(row => ({ source: 'عملیاتی', date: row.date, group: row.title || 'سایر', subgroup: row.weaver_name || 'سایر', amount: row.amount, description: row.description || '' }))} />}

        {!error && activeTab !== 'outInvoices' && activeTab !== 'expenses' && <GenericTable rows={rows} />}

      </Card>

    </div>

  );

}



function newPaymentLine(type = 'credit') {

  return {

    id: uid('pay'),

    type,

    amount: '',

    accountId: '',

    trackingNo: '',

    checkNo: '',

    bankName: '',

    dueJalali: '',

    dueDate: '',

    itemName: '',

    quantity: '',

    unitPrice: '',

    description: '',

  };

}





function YarnOutInvoicePage({ finance, setFinance }) {

  const L = {

    title: 'خروج نخ و تعيين تکليف مالي',

    pending: 'خروجي‌هاي نخ عملياتي در انتظار',

    date: 'تاريخ کمکي',

    selectCustomer: 'انتخاب مشتري/شخص',

    selectYarn: 'انتخاب نخ',

    quantity: 'وزن/مقدار خروجي',

    unitPrice: 'نرخ واحد',

    amount: 'مبلغ',

    desc: 'شرح',

    save: 'ثبت خروج نخ و اعمال در انبار',

    editSave: 'ثبت ويرايش خروج نخ',

    reset: 'لغو ويرايش',

    print: 'چاپ گزارش',

    list: 'ليست خروجي‌هاي نخ تعيين تکليف شده',

    noPriceHelp: 'در حالت مرجوعي نخ اماني/کارمزدي، نرخ و مبلغ لازم نيست و فقط موجودي نخ کسر مي‌شود.',

    priceHelp: 'در حالت تهاتر يا فروش، نرخ و مبلغ در حساب شخص و گزارش‌ها اعمال مي‌شود.',

    stock: 'نوع موجودي مبدا',

  };

  const modes = [

    { id: 'return_customer', label: 'مرجوعي مانده نخ اماني/کارمزدي', needsPrice: false },

    { id: 'barter', label: 'خروج جهت تهاتر', needsPrice: true },

    { id: 'sale', label: 'خروج جهت فروش', needsPrice: true },

  ];

  const stockTypes = [

    { id: 'amanat', label: 'اماني/کارمزدي مشتري' },

    { id: 'owned', label: 'ملکي' },

    { id: 'barter_owned', label: 'مالکي حاصل از تهاتر' },

  ];

  const { data, loading, error } = useOperationalData();

  const [editingId, setEditingId] = useState('');

  const [form, setForm] = useState({ date: today(), operationalDate: '', customer: '', itemName: '', quantity: '', unitPrice: '', costUnitPrice: '', amount: '', outMode: 'return_customer', stockType: 'amanat', source_type: 'manual', sourceId: '', description: '' });

  const rows = finance.yarnOutInvoices || [];

  const settledSources = new Set(rows.map(x => `${x.source_type || 'manual'}:${x.sourceId || x.id}`));

  const pendingYarnOut = (data.yarnOut || []).filter(x => !settledSources.has(`operational_yarn_out:${x.id}`));

  const customers = [...new Set([...(data.customers || []).map(x => x.name), ...(data.yarnOut || []).map(x => x.customer_name), ...(finance.invoices || []).map(x => x.customer), ...(finance.incomingInvoices || []).map(x => x.customer), ...(finance.yarnOutInvoices || []).map(x => x.customer)].filter(Boolean))];

  const yarns = [...new Set([...(data.yarn || []).map(x => x.name), ...(data.yarnIn || []).map(x => x.yarn_name), ...(data.yarnOut || []).map(x => x.yarn_name), ...(finance.ownedInventory || []).filter(x => x.kindCode === 'yarn').map(x => x.itemName)].filter(Boolean))];

  const mode = modes.find(x => x.id === form.outMode) || modes[0];

  const amount = mode.needsPrice ? (Number(form.amount || 0) || Number(form.quantity || 0) * Number(form.unitPrice || 0)) : 0;

  const averageYarnCost = itemName => {
    const receipts = (finance.ownedInventory || []).filter(x => x.kindCode === 'yarn' && x.itemName === itemName && Number(x.quantity || 0) > 0 && Number(x.amount || 0) > 0);
    const quantity = receipts.reduce((sum, x) => sum + Number(x.quantity || 0), 0);
    return quantity ? Math.round(receipts.reduce((sum, x) => sum + Number(x.amount || 0), 0) / quantity) : 0;
  };

  const shouldRecognizeYarnCost = mode.needsPrice && form.stockType !== 'amanat';

  const costAmount = shouldRecognizeYarnCost ? Number(form.quantity || 0) * Number(form.costUnitPrice || 0) : 0;

  const resetForm = () => { setEditingId(''); setForm({ date: today(), operationalDate: '', customer: '', itemName: '', quantity: '', unitPrice: '', costUnitPrice: '', amount: '', outMode: 'return_customer', stockType: 'amanat', source_type: 'manual', sourceId: '', description: '' }); };

  const beginManualInvoice = () => resetForm();

  const selectOperational = row => {

    setEditingId('');

    setForm({ date: today(), operationalDate: row.date || '', customer: row.customer_name || '', itemName: row.yarn_name || '', quantity: Math.abs(Number(row.weight || 0)) || '', unitPrice: '', costUnitPrice: '', amount: '', outMode: 'return_customer', stockType: 'amanat', source_type: 'operational_yarn_out', sourceId: row.id, description: `خروج نخ عملياتي | همبافت ${row.doc_no || '-'} | تاريخ ${row.date || '-'}` });

  };

  const edit = row => { setEditingId(row.id); setForm({ date: row.date || today(), operationalDate: row.operationalDate || '', customer: row.customer || '', itemName: row.itemName || '', quantity: Math.abs(Number(row.quantity || 0)) || '', unitPrice: row.unitPrice || '', costUnitPrice: row.costUnitPrice || '', amount: row.amount || '', outMode: row.outMode || 'return_customer', stockType: row.stockType || 'amanat', source_type: row.source_type || 'manual', sourceId: row.sourceId || '', description: row.description || '' }); };

  const remove = id => setFinance(prev => ({ ...prev, yarnOutInvoices: (prev.yarnOutInvoices || []).filter(x => x.id !== id), ownedInventory: (prev.ownedInventory || []).filter(x => x.sourceYarnOutInvoice !== id) }));

  const save = e => {

    e.preventDefault();

    const quantity = Math.abs(Number(form.quantity || 0));

    if (!form.customer || !form.itemName || !quantity) return;

    if (mode.needsPrice && !amount) return;

    if (shouldRecognizeYarnCost && !costAmount) { window.alert('برای خروج نخ ملکی، بهای تمام‌شده واحد الزامی است.'); return; }

    const invoice = { ...form, id: editingId || shortId('YOUT'), quantity, unitPrice: Number(form.unitPrice || 0), costUnitPrice: Number(form.costUnitPrice || 0), costAmount, amount, outMode: form.outMode, outModeLabel: mode.label, stockTypeLabel: stockTypes.find(x => x.id === form.stockType)?.label || form.stockType };

    setFinance(prev => {

      const sourceId = invoice.id;

      const yarnOutInvoices = [invoice, ...(prev.yarnOutInvoices || []).filter(x => x.id !== sourceId)];

      const ownedInventory = (prev.ownedInventory || []).filter(x => x.sourceYarnOutInvoice !== sourceId);

      ownedInventory.unshift({ id: uid('yout-stock'), sourceYarnOutInvoice: sourceId, customer: invoice.customer, kindCode: 'yarn', kind: `خروج نخ - ${invoice.stockTypeLabel} - ${mode.label}`, itemName: invoice.itemName, quantity: -quantity, unitPrice: Number(invoice.costUnitPrice || 0), amount: -Number(invoice.costAmount || 0), date: invoice.date || today() });

      return { ...prev, yarnOutInvoices, ownedInventory };

    });

    resetForm();

  };

  const printYarnOut = () => {

    const body = rows.map(x => '<tr><td>' + toJalali(x.date) + '</td><td>' + (x.customer || '') + '</td><td>' + (x.itemName || '') + '</td><td>' + num(x.quantity) + '</td><td>' + (x.outModeLabel || '') + '</td><td>' + money(x.unitPrice) + '</td><td>' + money(x.amount) + '</td></tr>').join('');

    printSection(L.list, '<table><thead><tr><th>تاريخ</th><th>شخص</th><th>نخ</th><th>مقدار</th><th>نوع</th><th>نرخ</th><th>مبلغ</th></tr></thead><tbody>' + body + '</tbody></table>');

  };

  return (

    <div className="space-y-5">

      <div className="grid grid-cols-4 gap-4"><Field label="خروجي در انتظار" value={num(pendingYarnOut.length)} /><Field label="خروج ثبت شده" value={num(rows.length)} /><Field label="وزن خروجي" value={`${num(rows.reduce((s, x) => s + Number(x.quantity || 0), 0))} کيلو`} tone="text-amber-300" /><Field label="هزینه مالی" value={`${money(rows.reduce((s, x) => s + Number(x.amount || 0), 0))} تومان`} tone="text-emerald-300" /></div>

      <Card><div className="flex flex-wrap items-center justify-between gap-4"><div><h3 className="font-bold">منبع فاکتور خروج نخ</h3><p className="mt-2 text-sm text-slate-400">یک خروج عملیاتی را انتخاب کنید یا بدون وابستگی به عملیات، فاکتور خروج نخ جدید صادر کنید.</p></div><PrimaryButton onClick={beginManualInvoice}>صدور فاکتور خروج نخ جدید</PrimaryButton></div><div className={`mt-3 rounded-md border p-3 text-sm ${form.source_type === 'manual' ? 'border-emerald-700 bg-emerald-950 text-emerald-100' : 'border-blue-700 bg-blue-950 text-blue-100'}`}>{form.source_type === 'manual' ? 'حالت صدور مستقل فعال است؛ نام شخص و نخ را می‌توانید تایپ یا از پیشنهادها انتخاب کنید.' : 'فاکتور بر اساس خروج انتخاب‌شده از بخش عملیاتی تکمیل می‌شود.'}</div></Card>

      <div className="grid grid-cols-[2fr_0.7fr] gap-4">

        <Card><h3 className="mb-4 font-bold">{form.source_type === 'manual' ? 'صدور فاکتور خروج نخ مستقل' : L.title}</h3><form className="space-y-4" onSubmit={save}><div className="grid grid-cols-3 gap-3"><label className="text-sm text-slate-300"><span className="mb-2 block">{L.date}</span><DateInput className="w-full" value={form.date} onChange={e => setForm({ ...form, date: e.target.value })} /></label>{form.source_type === 'manual' ? <label className="text-sm text-slate-300">شخص / مشتری<TextInput className="mt-2 w-full" list="manual-yarn-out-customers" placeholder={L.selectCustomer} value={form.customer} onChange={e => setForm({ ...form, customer: e.target.value })} /><datalist id="manual-yarn-out-customers">{customers.map(c => <option key={c} value={c} />)}</datalist></label> : <SelectInput value={form.customer} onChange={e => setForm({ ...form, customer: e.target.value })}><option value="">{L.selectCustomer}</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput>}{form.source_type === 'manual' ? <label className="text-sm text-slate-300">نوع نخ<TextInput className="mt-2 w-full" list="manual-yarn-out-items" placeholder={L.selectYarn} value={form.itemName} onChange={e => { const itemName = e.target.value; setForm({ ...form, itemName, costUnitPrice: averageYarnCost(itemName) || '' }); }} /><datalist id="manual-yarn-out-items">{yarns.map(y => <option key={y} value={y} />)}</datalist></label> : <SelectInput value={form.itemName} onChange={e => { const itemName = e.target.value; setForm({ ...form, itemName, costUnitPrice: averageYarnCost(itemName) || '' }); }}><option value="">{L.selectYarn}</option>{yarns.map(y => <option key={y} value={y}>{y}</option>)}</SelectInput>}<SelectInput value={form.outMode} onChange={e => setForm({ ...form, outMode: e.target.value, unitPrice: '', amount: '' })}>{modes.map(m => <option key={m.id} value={m.id}>{m.label}</option>)}</SelectInput><SelectInput value={form.stockType} onChange={e => setForm({ ...form, stockType: e.target.value, costUnitPrice: e.target.value === 'amanat' ? '' : (form.costUnitPrice || averageYarnCost(form.itemName) || '') })}>{stockTypes.map(s => <option key={s.id} value={s.id}>{s.label}</option>)}</SelectInput><TextInput type="number" placeholder={L.quantity} value={form.quantity} onChange={e => setForm({ ...form, quantity: e.target.value, amount: '' })} />{mode.needsPrice && <><TextInput type="number" placeholder={L.unitPrice} value={form.unitPrice} onChange={e => setForm({ ...form, unitPrice: e.target.value, amount: '' })} /><TextInput type="number" placeholder={L.amount} value={amount || ''} onChange={e => setForm({ ...form, amount: e.target.value })} />{form.stockType !== 'amanat' && <TextInput type="number" min="0" placeholder="بهای تمام‌شده واحد" value={form.costUnitPrice} onChange={e => setForm({ ...form, costUnitPrice: e.target.value })} />}</>}<TextInput className="col-span-3" placeholder={L.desc} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></div><div className={`rounded-md border p-3 text-sm ${mode.needsPrice ? 'border-amber-700 bg-amber-950 text-amber-100' : 'border-emerald-700 bg-emerald-950 text-emerald-100'}`}>{mode.needsPrice ? `${L.priceHelp}${shouldRecognizeYarnCost ? ' بهای تمام‌شده خروج نیز جداگانه در موجودی و هزینه فروش ثبت می‌شود.' : ''}` : L.noPriceHelp}</div><div className="grid grid-cols-4 gap-3"><Field label="مقدار کسر از انبار" value={`${num(Math.abs(Number(form.quantity || 0)))} کيلو`} tone="text-red-300" /><Field label="نرخ فروش/تهاتر" value={mode.needsPrice ? money(form.unitPrice) : '-'} /><Field label="مبلغ فروش/تهاتر" value={mode.needsPrice ? money(amount) + ' تومان' : 'بدون اثر ريالي'} /><Field label="بهای تمام‌شده خروج" value={shouldRecognizeYarnCost ? money(costAmount) + ' تومان' : '-'} tone="text-amber-300" /></div><PrimaryButton className="w-full" type="submit">{editingId ? L.editSave : L.save}</PrimaryButton>{editingId && <GhostButton onClick={resetForm}>{L.reset}</GhostButton>}</form></Card>

        <Card><div className="mb-4 flex items-center justify-between"><h3 className="font-bold">{L.pending}</h3><span className="text-xs text-slate-400">{loading ? 'در حال دريافت...' : num(pendingYarnOut.length) + ' مورد'}</span></div>{error && <ErrorBox message={error} />}<div className="max-h-[620px] space-y-2 overflow-auto">{pendingYarnOut.length ? pendingYarnOut.map(row => <button key={row.id} type="button" className="w-full rounded-md border border-slate-700 bg-slate-900 p-3 text-right text-sm hover:border-amber-500" onClick={() => selectOperational(row)}><div className="flex items-center justify-between gap-2"><span className="font-bold text-amber-200">{row.yarn_name || '-'}</span><span className="rounded-full bg-slate-950 px-2 py-1 text-xs text-amber-200">{num(row.weight)} کيلو</span></div><div className="mt-1 text-xs text-slate-400">{row.customer_name || '-'} | {row.doc_no || '-'}</div><div className="mt-1 text-xs text-slate-500">{toJalali(row.date)}</div></button>) : <EmptyState />}</div></Card>

      </div>

      <Card><div className="mb-4 flex items-center justify-between"><h3 className="font-bold">{L.list}</h3><div className="flex gap-2"><PrimaryButton onClick={printYarnOut}>{L.print}</PrimaryButton><PrimaryButton onClick={() => exportExcel(L.list, rows, [['date','تاریخ'],['customer','شخص'],['itemName','نخ'],['quantity','مقدار'],['outModeLabel','نوع خروج'],['unitPrice','نرخ'],['amount','مبلغ']])}>خروجی اکسل</PrimaryButton></div></div><YarnOutInvoiceTable rows={rows} onEdit={edit} onDelete={remove} /></Card>

    </div>

  );

}



function YarnOutInvoiceTable({ rows, onEdit, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full text-right text-sm"><thead><tr className="border-b border-slate-700 text-slate-300"><th className="p-3">تاريخ</th><th>شخص</th><th>نخ</th><th>مقدار</th><th>نوع</th><th>مبلغ</th><th>عمليات</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{toJalali(row.date)}</td><td>{row.customer}</td><td>{row.itemName}</td><td>{num(row.quantity)}</td><td>{row.outModeLabel}</td><td>{money(row.amount)}</td><td className="space-x-2 space-x-reverse"><GhostButton onClick={() => onEdit(row)}>ويرايش</GhostButton><DangerButton onClick={() => onDelete(row.id)}>حذف</DangerButton></td></tr>)}</tbody></table></div>;

}



function IncomingInvoicePage({ finance, setFinance, onlySource = '' }) {

  const { data, loading, error } = useOperationalData();

  const [editingId, setEditingId] = useState('');

  const [payments, setPayments] = useState([newPaymentLine('credit')]);

  const [form, setForm] = useState({ date: today(), operationalDate: '', customer: '', inventoryType: 'yarn', itemName: '', quantity: '', unitPrice: '', subtotal: '', taxable: false, taxRate: '', source_type: 'manual', sourceId: '', description: '', nonFinancial: false });

  const customers = [...new Set([
    ...data.customers.map(x => x.name),
    ...data.chelleIn.map(x => x.warper),
    ...finance.invoices.map(x => x.customer),
    ...finance.incomingInvoices.map(x => x.customer),
    ...finance.payableDocs.map(x => x.customer),
    ...finance.receivableDocs.map(x => x.assignedTo),
  ].filter(Boolean))];

  const rows = onlySource === 'chelle'
    ? (finance.incomingInvoices || []).filter(x => x.source_type === 'operational_chelle_in')
    : (finance.incomingInvoices || []).filter(x => x.source_type !== 'operational_chelle_in');

  const allRows = finance.incomingInvoices || [];

  const settledSources = new Set(allRows.map(x => `${x.source_type || 'manual'}:${x.sourceId || x.id}`));

  const pendingYarnIn = data.yarnIn.filter(x => !settledSources.has(`operational_yarn_in:${x.id}`));

  const pendingChelleIn = data.chelleIn.filter(x => !settledSources.has(`operational_chelle_in:${x.id}`));

  const pendingSpareParts = data.spareParts.filter(x => !settledSources.has(`operational_spare_part:${x.id}`));

  const pendingOperationalAll = [

    ...pendingYarnIn.map(x => ({ ...x, pendingType: 'operational_yarn_in', title: x.yarn_name, subtitle: `${x.customer_name || '-'} | همبافت ${x.doc_no || '-'}`, quantityLabel: `${num(x.weight)} کيلو`, actionLabel: 'ورود نخ' })),

    ...pendingChelleIn.map(x => ({ ...x, pendingType: 'operational_chelle_in', title: x.doc_no || x.hambaft || x.yarn_name, subtitle: `چله پیچ ${x.warper || '-'} | صاحب نخ ${x.customer_name || '-'} | همبافت ${x.hambaft || '-'}`, quantityLabel: `${num(x.weight)} کيلو`, actionLabel: 'ورود چله' })),

    ...pendingSpareParts.map(x => ({ ...x, pendingType: 'operational_spare_part', title: x.part_name, subtitle: `${x.vendor_name || '-'} | ${x.part_number || '-'}`, quantityLabel: `${num(x.quantity)} عدد`, actionLabel: 'قطعه' })),

  ];

  const pendingOperational = onlySource === 'chelle'
    ? pendingOperationalAll.filter(x => x.pendingType === 'operational_chelle_in')
    : pendingOperationalAll.filter(x => x.pendingType !== 'operational_chelle_in');

  const sourceItems = form.inventoryType === 'yarn' ? data.yarn : form.inventoryType === 'fabric' ? data.kala : form.inventoryType === 'spare_part' ? data.spareParts.map(x => ({ id: x.id, name: x.part_name || x.part_number })) : [];

  const subtotal = Number(form.subtotal || 0) || (Number(form.quantity || 0) * Number(form.unitPrice || 0));

  const taxRate = form.taxable && !form.nonFinancial ? Number(form.taxRate || finance.accountingSettings?.defaultVatRate || 0) : 0;

  const taxAmount = Math.round(subtotal * taxRate / 100);

  const amount = subtotal + taxAmount;

  const paid = payments.reduce((s, p) => s + Number(p.amount || 0), 0);

  const openReceivableDocs = finance.receivableDocs.filter(x => x.status === 'open');

  const updatePayment = (id, patch) => setPayments(prev => prev.map(p => p.id === id ? { ...p, ...patch } : p));

  const removePayment = id => setPayments(prev => prev.filter(p => p.id !== id));

  const resetForm = () => {

    setEditingId('');

    setPayments([newPaymentLine('credit')]);

    setForm({ date: today(), operationalDate: '', customer: '', inventoryType: 'yarn', itemName: '', quantity: '', unitPrice: '', subtotal: '', taxable: false, taxRate: '', source_type: 'manual', sourceId: '', description: '', nonFinancial: false });

  };

  const beginManualInvoice = () => resetForm();

  const selectOperational = row => {

    setEditingId('');

    setPayments([newPaymentLine('credit')]);

    if (row.pendingType === 'operational_yarn_in' || row.type === 'incoming') {

      setForm({ date: today(), operationalDate: row.date || '', customer: row.customer_name || '', inventoryType: 'yarn', itemName: row.yarn_name || '', quantity: Math.abs(Number(row.weight || 0)) || '', unitPrice: '', subtotal: '', taxable: false, taxRate: '', source_type: 'operational_yarn_in', sourceId: row.id, description: `ورود نخ عملياتي | همبافت ${row.doc_no || '-'} | تاريخ ${row.date || '-'}`, nonFinancial: false });

      return;

    }

    if (row.pendingType === 'operational_chelle_in') {

      setForm({ date: today(), operationalDate: row.date || '', customer: row.warper || row.customer_name || '', inventoryType: 'yarn', itemName: row.yarn_name || row.hambaft || '', quantity: Math.abs(Number(row.weight || 0)) || '', unitPrice: '', subtotal: '', taxable: false, taxRate: '', source_type: 'operational_chelle_in', sourceId: row.id, description: `ورود چله عملياتي | شماره ${row.doc_no || '-'} | چله پیچ ${row.warper || '-'} | صاحب نخ ${row.customer_name || '-'} | همبافت ${row.hambaft || '-'} | تاريخ ${row.date || '-'}`, nonFinancial: false });

      return;

    }

    if (row.pendingType === 'operational_spare_part') {

      setForm({ date: today(), operationalDate: row.date || '', customer: row.vendor_name || 'تأمین‌کننده قطعات', inventoryType: 'spare_part', itemName: row.part_name || row.part_number || '', quantity: Math.abs(Number(row.quantity || 0)) || '', unitPrice: '', subtotal: '', taxable: false, taxRate: '', source_type: 'operational_spare_part', sourceId: row.id, description: `ورود قطعه عملیاتی | شماره قطعه ${row.part_number || '-'} | وضعیت ${row.condition_status || '-'} | تاریخ ${row.date || '-'}`, nonFinancial: false });

      return;

    }

  };

  const save = e => {

    e.preventDefault();

    if (!form.customer || !amount) return;

    const cleanPayments = form.nonFinancial ? [] : payments.map(p => ({ ...p, amount: Number(p.amount || 0), quantity: Number(p.quantity || 0), unitPrice: Number(p.unitPrice || 0) })).filter(p => p.amount > 0);

    const validationError = form.nonFinancial ? '' : paymentValidationError(cleanPayments, amount, 'فاکتور ورود');

    if (validationError) { window.alert(validationError); return; }

    const invoice = { ...form, id: editingId || shortId('IN'), subtotal, taxAmount: form.nonFinancial ? 0 : taxAmount, taxRate: form.nonFinancial ? 0 : taxRate, taxable: !form.nonFinancial && !!form.taxable, amount, quantity: Number(form.quantity || 0), unitPrice: Number(form.unitPrice || 0), payments: cleanPayments, nonFinancial: !!form.nonFinancial };

    setFinance(prev => {

      const sourceId = invoice.id;

      let movements = prev.movements.filter(x => x.sourceIncomingInvoice !== sourceId);

      let payableDocs = prev.payableDocs.filter(x => x.sourceIncomingInvoice !== sourceId);

      let receivableDocs = prev.receivableDocs.map(x => x.assignedIncomingInvoice === sourceId ? { ...x, status: 'open', assignedTo: '', assignedIncomingInvoice: '' } : x);

      let ownedInventory = (prev.ownedInventory || []).filter(x => x.sourceIncomingInvoice !== sourceId);

      if (invoice.inventoryType === 'yarn' || invoice.inventoryType === 'fabric' || invoice.inventoryType === 'spare_part') {

        ownedInventory.unshift({

          id: uid('in-stock'),

          sourceIncomingInvoice: sourceId,

          customer: invoice.customer,

          kindCode: invoice.inventoryType === 'yarn' ? 'yarn' : invoice.inventoryType === 'fabric' ? 'fabric' : 'spare_part',

          kind: invoice.inventoryType === 'yarn' ? 'ورود نخ از فاکتور ورود' : invoice.inventoryType === 'fabric' ? 'ورود پارچه از فاکتور ورود' : 'ورود قطعه از انبار عملیاتی',

          itemName: invoice.itemName,

          quantity: Number(invoice.quantity || 0),

          unitPrice: Number(invoice.unitPrice || 0),

          amount: Number(invoice.subtotal || invoice.amount || 0),

          date: invoice.date || today(),

        });

      }

      cleanPayments.forEach(p => {

        if (p.type === 'cash') movements.unshift({ id: uid('mov'), accountId: p.accountId, date: form.date || today(), direction: 'out', transactionType: 'supplier_payment', amount: p.amount, trackingNo: p.trackingNo, sourceIncomingInvoice: sourceId, payer: invoice.customer, customer: invoice.customer, counterpartyConfirmed: true, counterpartySource: 'incoming_invoice', description: 'پرداخت نقدي فاکتور ورود ' + sourceId });

        if (p.type === 'check') payableDocs.unshift({ id: uid('pch'), customer: invoice.customer, amount: p.amount, checkNo: p.checkNo, bank: p.bankName, dueDate: p.dueDate, dueJalali: p.dueJalali, status: 'open', sourceIncomingInvoice: sourceId });

        if (p.type === 'assign_receivable') receivableDocs = receivableDocs.map(doc => doc.id === p.docId ? { ...doc, status: 'assigned', assignedTo: invoice.customer, assignedAt: today(), assignedIncomingInvoice: sourceId } : doc);

      });

      return { ...prev, incomingInvoices: [invoice, ...prev.incomingInvoices.filter(x => x.id !== sourceId)], movements, payableDocs, receivableDocs, ownedInventory };

    });

    resetForm();

  };

  const edit = row => {

    setEditingId(row.id);

    setPayments((row.payments || [newPaymentLine('credit')]).map(p => ({ ...newPaymentLine(p.type), ...p, id: uid('pay') })));

    setForm({ date: row.date || today(), operationalDate: row.operationalDate || '', customer: row.customer || '', inventoryType: row.inventoryType || 'yarn', itemName: row.itemName || '', quantity: row.quantity || '', unitPrice: row.unitPrice || '', subtotal: row.subtotal ?? Math.max(0, Number(row.amount || 0) - Number(row.taxAmount || 0)), taxable: !!row.taxable, taxRate: row.taxRate || '', source_type: row.source_type || 'manual', sourceId: row.sourceId || '', description: row.description || '', nonFinancial: !!row.nonFinancial });

  };

  const remove = id => setFinance(prev => ({ ...prev, incomingInvoices: prev.incomingInvoices.filter(x => x.id !== id), movements: prev.movements.filter(x => x.sourceIncomingInvoice !== id), payableDocs: prev.payableDocs.filter(x => x.sourceIncomingInvoice !== id), receivableDocs: prev.receivableDocs.map(x => x.assignedIncomingInvoice === id ? { ...x, status: 'open', assignedTo: '', assignedIncomingInvoice: '' } : x), ownedInventory: (prev.ownedInventory || []).filter(x => x.sourceIncomingInvoice !== id) }));

  const printIncoming = () => {

    const body = rows.map(x => '<tr><td>' + (toJalali(x.date) || '') + '</td><td>' + (x.customer || '') + '</td><td>' + (x.itemName || '') + '</td><td>' + money(x.subtotal ?? (Number(x.amount || 0) - Number(x.taxAmount || 0))) + '</td><td>' + money(x.taxAmount || 0) + '</td><td>' + money(x.amount) + '</td><td>' + (x.payments || []).map(p => p.type).join(', ') + '</td></tr>').join('');

    printSection('گزارش فاکتور ورود', '<table><thead><tr><th>تاريخ</th><th>شخص</th><th>کالا/قطعه</th><th>قبل مالیات</th><th>مالیات</th><th>جمع</th><th>تسويه</th></tr></thead><tbody>' + body + '</tbody></table>');

  };

  return (

    <div className="space-y-5">

      <div className="grid grid-cols-3 gap-4"><Field label={onlySource === 'chelle' ? 'تعداد فاکتور ورود چله' : 'تعداد فاکتور ورود'} value={num(rows.length)} /><Field label="جمع مبلغ ورودی" value={money(rows.reduce((sum, x) => sum + Number(x.amount || 0), 0)) + ' تومان'} tone="text-emerald-300" /><Field label={onlySource === 'chelle' ? 'چله‌های در انتظار مالی' : 'موجودی قطعات عملیاتی'} value={onlySource === 'chelle' ? num(pendingOperational.length) + ' مورد' : num(data.spareParts.length) + ' رکورد'} /></div>

      {!onlySource && <Card><div className="flex flex-wrap items-center justify-between gap-4"><div><h3 className="font-bold">منبع فاکتور ورود نخ</h3><p className="mt-2 text-sm text-slate-400">یک ورود عملیاتی را انتخاب کنید یا بدون وابستگی به عملیات، فاکتور ورود نخ جدید صادر کنید.</p></div><PrimaryButton onClick={beginManualInvoice}>صدور فاکتور ورود نخ جدید</PrimaryButton></div><div className={`mt-3 rounded-md border p-3 text-sm ${form.source_type === 'manual' ? 'border-emerald-700 bg-emerald-950 text-emerald-100' : 'border-blue-700 bg-blue-950 text-blue-100'}`}>{form.source_type === 'manual' ? 'حالت صدور مستقل فعال است؛ نام شخص و کالا را می‌توانید تایپ یا از پیشنهادها انتخاب کنید.' : 'فاکتور بر اساس ورود انتخاب‌شده از بخش عملیاتی تکمیل می‌شود.'}</div></Card>}

      <div className="grid grid-cols-[2fr_0.65fr] gap-4">

        <Card>
          <h3 className="mb-4 font-bold">{onlySource === 'chelle' ? 'ثبت فاکتور ورود چله و تسویه' : form.source_type === 'manual' ? 'صدور فاکتور ورود نخ مستقل' : 'ثبت فاکتور ورود و تسويه'}</h3>
          <form className="space-y-4" onSubmit={save}>
            <div className="grid grid-cols-3 gap-3">
              <label className="text-sm text-slate-300"><span className="mb-2 block">تاريخ کمکي</span><DateInput className="w-full" value={form.date} onChange={e => setForm({ ...form, date: e.target.value })} /></label>
              {!onlySource && form.source_type === 'manual' ? <label className="text-sm text-slate-300">شخص / فروشنده<TextInput className="mt-2 w-full" list="manual-yarn-in-customers" placeholder="انتخاب یا درج شخص" value={form.customer} onChange={e => setForm({ ...form, customer: e.target.value })} /><datalist id="manual-yarn-in-customers">{customers.map(c => <option key={c} value={c} />)}</datalist></label> : <SelectInput value={form.customer} onChange={e => setForm({ ...form, customer: e.target.value })}><option value="">انتخاب مشتري/شخص</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput>}
              <SelectInput value={form.inventoryType} onChange={e => setForm({ ...form, inventoryType: e.target.value, itemName: '' })}><option value="yarn">نخ</option><option value="fabric">پارچه</option><option value="spare_part">قطعه یدکی</option><option value="other">ساير</option></SelectInput>
              {!onlySource && form.source_type === 'manual' && form.inventoryType === 'yarn'
                ? <label className="text-sm text-slate-300">نوع نخ<TextInput className="mt-2 w-full" list="manual-yarn-in-items" placeholder="انتخاب یا درج نوع نخ" value={form.itemName} onChange={e => setForm({ ...form, itemName: e.target.value })} /><datalist id="manual-yarn-in-items">{sourceItems.map(x => <option key={x.id || x.name} value={x.name} />)}</datalist></label>
                : form.inventoryType === 'other'
                ? <TextInput placeholder="شرح ساير" value={form.itemName} onChange={e => setForm({ ...form, itemName: e.target.value })} />
                : <SelectInput value={form.itemName} onChange={e => setForm({ ...form, itemName: e.target.value })}><option value="">انتخاب نوع</option>{form.itemName && !sourceItems.some(x => x.name === form.itemName) && <option value={form.itemName}>{form.itemName}</option>}{sourceItems.map(x => <option key={x.id || x.name} value={x.name}>{x.name}</option>)}</SelectInput>}
              <TextInput type="number" placeholder="مقدار" value={form.quantity} onChange={e => setForm({ ...form, quantity: e.target.value, subtotal: '' })} />
              <TextInput type="number" placeholder="نرخ واحد" value={form.unitPrice} onChange={e => setForm({ ...form, unitPrice: e.target.value, subtotal: '' })} />
              <TextInput type="number" placeholder="مبلغ قبل از مالیات" value={subtotal || ''} onChange={e => setForm({ ...form, subtotal: e.target.value })} />
              <TextInput className="col-span-2" placeholder="شرح" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} />
              <TextInput type="number" min="0" max="100" placeholder="نرخ مالیات (درصد)" value={form.taxRate || finance.accountingSettings?.defaultVatRate || ''} disabled={!form.taxable || form.nonFinancial} onChange={e => setForm({ ...form, taxRate: e.target.value })} />
              <label className="col-span-3 flex items-center gap-3 rounded-md border border-blue-700 bg-blue-950 p-3 text-sm text-blue-100"><input type="checkbox" disabled={form.nonFinancial} checked={!!form.taxable && !form.nonFinancial} onChange={e => setForm({ ...form, taxable: e.target.checked })} /><span>خرید مشمول مالیات بر ارزش افزوده است؛ مبلغ {money(taxAmount)} تومان به اعتبار مالیاتی خرید ثبت می‌شود.</span></label>
              <label className="col-span-3 flex items-center gap-3 rounded-md border border-amber-700 bg-amber-950 p-3 text-sm text-amber-100"><input type="checkbox" checked={!!form.nonFinancial} onChange={e => setForm({ ...form, nonFinancial: e.target.checked, taxable: e.target.checked ? false : form.taxable })} /><span>نخ/کالاي اماني کارمزدي؛ مبلغ فقط براي ارزش کالايي و اعتبارسنجي ثبت شود و در صورتحساب ريالي مشتري اثر نگذارد.</span></label>
            </div>
            {form.nonFinancial
              ? <div className="rounded-md border border-emerald-700 bg-emerald-950 p-4 text-sm text-emerald-100">اين فاکتور بدون اثر ريالي ثبت مي‌شود؛ چک، نقد، بدهکاري يا بستانکاري براي مشتري ايجاد نمي‌شود، اما مقدار و ارزش کالا در حساب کالايي و اعتبارسنجي لحاظ مي‌شود.</div>
              : <div className="rounded-md border border-slate-700 bg-slate-900 p-4"><div className="mb-3 flex items-center justify-between"><h4 className="font-bold">رديف هاي تسويه فاکتور ورود</h4><PrimaryButton onClick={() => setPayments(prev => [...prev, newPaymentLine('credit')])}>افزودن رديف</PrimaryButton></div><div className="space-y-3">{payments.map(p => <IncomingPaymentLine key={p.id} payment={p} accounts={finance.accounts} receivableDocs={openReceivableDocs} onChange={patch => updatePayment(p.id, patch)} onRemove={() => removePayment(p.id)} />)}</div></div>}
            <div className="grid grid-cols-5 gap-3"><Field label="مبلغ قبل مالیات" value={money(subtotal) + ' تومان'} /><Field label="مالیات/عوارض" value={money(form.nonFinancial ? 0 : taxAmount) + ' تومان'} tone="text-blue-300" /><Field label="جمع فاکتور" value={money(form.nonFinancial ? subtotal : amount) + ' تومان'} /><Field label="جمع تسويه" value={money(form.nonFinancial ? 0 : paid) + ' تومان'} tone={form.nonFinancial || paid === amount ? 'text-emerald-300' : 'text-amber-300'} /><Field label={form.nonFinancial ? 'اثر ريالي' : 'مانده'} value={form.nonFinancial ? 'بدون اثر در صورتحساب' : money(amount - paid) + ' تومان'} tone={form.nonFinancial || amount - paid === 0 ? 'text-emerald-300' : 'text-red-300'} /></div>
            <PrimaryButton className="w-full" type="submit">{onlySource === 'chelle' ? 'ثبت فاکتور ورود چله و اعمال مالی' : 'ثبت فاکتور ورود و اعمال مالي'}</PrimaryButton>
          </form>
        </Card>

        <Card><div className="mb-4 flex items-center justify-between"><h3 className="font-bold">عمليات در انتظار تعيين تکليف</h3><span className="text-xs text-slate-400">{loading ? 'در حال دريافت...' : num(pendingOperational.length) + ' مورد'}</span></div>{error && <ErrorBox message={error} />}<div className="max-h-[620px] space-y-2 overflow-auto">{pendingOperational.length ? pendingOperational.map(row => <button key={`${row.pendingType}-${row.id}`} type="button" className="w-full rounded-md border border-slate-700 bg-slate-900 p-3 text-right text-sm hover:border-blue-500" onClick={() => selectOperational(row)}><div className="flex items-center justify-between gap-2"><span className="font-bold text-blue-200">{row.actionLabel}: {row.title || '-'}</span><span className="rounded-full bg-slate-950 px-2 py-1 text-xs text-amber-200">{row.quantityLabel}</span></div><div className="mt-1 text-xs text-slate-400">{row.subtitle}</div><div className="mt-1 text-xs text-slate-500">تاريخ: {row.date || '-'}</div></button>) : <EmptyState />}</div></Card>

      </div>

      <Card><div className="mb-4 flex items-center justify-between"><h3 className="font-bold">{onlySource === 'chelle' ? 'لیست فاکتورهای ورود چله ثبت شده' : 'ليست فاکتورهاي ورود ثبت شده'}</h3><div className="flex gap-2"><PrimaryButton onClick={printIncoming}>چاپ گزارش</PrimaryButton><PrimaryButton onClick={() => exportExcel('گزارش فاکتور ورود', rows.map(row => ({ ...row, settlement: (row.payments || []).map(payment => payment.type).join('، ') })), [['date','تاریخ'],['customer','شخص'],['itemName','کالا یا قطعه'],['subtotal','قبل مالیات'],['taxAmount','مالیات'],['amount','جمع'],['settlement','تسویه']])}>خروجی اکسل</PrimaryButton></div></div><IncomingInvoiceTable rows={rows} onEdit={edit} onDelete={remove} /></Card>

    </div>

  );

}



function InvoicePage({ finance, setFinance }) {

  const { data } = useOperationalData();

  const [rows, setRows] = useState([]);

  const [selected, setSelected] = useState(null);

  const [manualMode, setManualMode] = useState(false);

  const [editingNumber, setEditingNumber] = useState('');

  const [pricingMode, setPricingMode] = useState('commission');

  const [basis, setBasis] = useState('weight');

  const [unitPrice, setUnitPrice] = useState(250000);

  const [costUnitPrice, setCostUnitPrice] = useState(0);

  const [termCashPercent, setTermCashPercent] = useState(30);

  const [termCheckPercent, setTermCheckPercent] = useState(70);

  const [termCheckMonths, setTermCheckMonths] = useState(3);

  const [taxable, setTaxable] = useState(false);

  const [payments, setPayments] = useState([newPaymentLine('credit')]);



  useEffect(() => {

    apiGet('/operational/out-invoices?limit=50').then(result => {

      const nextRows = result.rows || [];

      setRows(nextRows);

      const registered = new Set(finance.invoices.map(x => String(x.number)));

      setSelected(nextRows.find(x => !registered.has(String(x.shom_f_khor))) || nextRows[0] || null);

    }).catch(() => {});

  }, [finance.invoices]);



  const quantity = basis === 'weight' ? selected?.w_salon : selected?.metr_salon;

  const subtotal = Number(quantity || 0) * Number(unitPrice || 0);

  const taxRate = taxable ? Number(finance.accountingSettings?.defaultVatRate || 0) : 0;

  const taxAmount = Math.round(subtotal * taxRate / 100);

  const costAmount = pricingMode === 'sale' ? Number(quantity || 0) * Number(costUnitPrice || 0) : 0;

  const total = subtotal + taxAmount;

  const paid = payments.reduce((s, p) => s + Number(p.amount || 0), 0);

  const remaining = total - paid;

  const registeredNumbers = new Set(finance.invoices.map(x => String(x.number)));

  const selectableRows = rows.filter(row => !registeredNumbers.has(String(row.shom_f_khor)) || String(row.shom_f_khor) === String(editingNumber));

  const customerNames = [...new Set((data.customers || []).map(row => row.name || row.mosh_name).filter(Boolean))];

  const itemNames = [...new Set((data.kala || []).map(row => row.name || row.kala_name).filter(Boolean))];

  const updateSelected = patch => setSelected(current => ({ ...(current || {}), ...patch }));

  const beginManualInvoice = () => {

    setManualMode(true);

    setEditingNumber('');

    setSelected({ id_f_khor: '', shom_f_khor: shortId('FIN'), tarikh_f_khor: today(), mosh_f_khor: '', kala_name: '', metr_salon: '', w_salon: '', piece_count: '' });

    setPricingMode('sale');

    setBasis('weight');

    setUnitPrice(0);

    setCostUnitPrice(0);

    setTaxable(false);

    setPayments([newPaymentLine('credit')]);

  };



  const updatePayment = (id, patch) => setPayments(prev => prev.map(p => p.id === id ? { ...p, ...patch } : p));

  const removePayment = id => setPayments(prev => prev.filter(p => p.id !== id));

  const editInvoice = invoice => {

    setManualMode(Boolean(invoice.sourceType === 'manual' || !invoice.operationalId));

    setEditingNumber(invoice.number);

    setSelected({

      id_f_khor: invoice.operationalId,

      shom_f_khor: invoice.number,

      tarikh_f_khor: invoice.operationalDate || invoice.date,

      mosh_f_khor: invoice.customer,

      kala_name: invoice.item,

      metr_salon: invoice.meter,

      w_salon: invoice.weight,

      piece_count: invoice.pieceCount || '',

    });

    setPricingMode(invoice.pricingMode || 'commission');

    setBasis(invoice.basis || 'weight');

    setUnitPrice(invoice.unitPrice || 0);

    setCostUnitPrice(invoice.costUnitPrice || 0);

    setTermCashPercent(invoice.paymentTerms?.cashPercent ?? 30);

    setTermCheckPercent(invoice.paymentTerms?.checkPercent ?? 70);

    setTermCheckMonths(invoice.paymentTerms?.checkMonths ?? 3);

    setTaxable(Boolean(invoice.taxable));

    setPayments((invoice.payments || []).map(p => ({ ...newPaymentLine(p.type), ...p, id: uid('pay') }))); 

  };



  const saveSettlement = () => {

    if (!selected) return;

    if (!String(selected.shom_f_khor || '').trim() || !String(selected.mosh_f_khor || '').trim() || !String(selected.kala_name || '').trim()) {

      window.alert('شماره فاکتور، شخص و نام کالا الزامی است.');

      return;

    }

    if (!(Number(quantity || 0) > 0) || !(Number(total || 0) > 0)) {

      window.alert('مقدار و نرخ واحد باید بیشتر از صفر باشند.');

      return;

    }

    if (!editingNumber && finance.invoices.some(row => String(row.number) === String(selected.shom_f_khor))) {

      window.alert('این شماره فاکتور قبلاً ثبت شده است.');

      return;

    }

    const cleanPayments = payments

      .map(p => ({ ...p, amount: Number(p.amount || 0), quantity: Number(p.quantity || 0), unitPrice: Number(p.unitPrice || 0) }))

      .filter(p => p.amount > 0);

    const validationError = paymentValidationError(cleanPayments, total, 'فاکتور فروش');

    if (validationError) { window.alert(validationError); return; }

    const invoice = {

      id: uid('finv'),

      operationalId: manualMode ? null : selected.id_f_khor,

      sourceType: manualMode ? 'manual' : 'operational',

      number: selected.shom_f_khor,

      date: manualMode ? (selected.tarikh_f_khor || today()) : today(),

      operationalDate: selected.tarikh_f_khor,

      customer: selected.mosh_f_khor,

      item: selected.kala_name,

      pricingMode,

      basis,

      quantity,

      unitPrice: Number(unitPrice || 0),

      costUnitPrice: Number(costUnitPrice || 0),

      costAmount,

      total,

      subtotal,

      taxable,

      taxRate,

      taxAmount,

      payments: cleanPayments,

      paymentTerms: { cashPercent: Number(termCashPercent || 0), checkPercent: Number(termCheckPercent || 0), checkMonths: Number(termCheckMonths || 0) },

      meter: selected.metr_salon,

      weight: selected.w_salon,

    };



    setFinance(prev => {

      const receivableDocs = prev.receivableDocs.filter(x => String(x.sourceInvoice) !== String(invoice.number));

      const movements = prev.movements.filter(x => String(x.sourceInvoice) !== String(invoice.number));

      const ownedInventory = (prev.ownedInventory || []).filter(x => String(x.sourceInvoice) !== String(invoice.number));



      cleanPayments.forEach(p => {

        if (p.type === 'cash') {

          movements.unshift({ id: uid('mov'), accountId: p.accountId, date: today(), direction: 'in', transactionType: 'customer_receipt', amount: p.amount, trackingNo: p.trackingNo, sourceInvoice: invoice.number, description: `دريافت نقدي فاکتور ${invoice.number}`, payer: invoice.customer, customer: invoice.customer, counterpartyConfirmed: true, counterpartySource: 'sales_invoice' });

        }

        if (p.type === 'check') {

          receivableDocs.unshift({ id: uid('rch'), customer: invoice.customer, amount: p.amount, checkNo: p.checkNo, bank: p.bankName, dueDate: p.dueDate, dueJalali: p.dueJalali, status: 'open', sourceInvoice: invoice.number });

        }

        if (p.type === 'barter_yarn' || p.type === 'barter_fabric') {

          ownedInventory.unshift({

            id: uid('own'),

            sourceInvoice: invoice.number,

            customer: invoice.customer,

            kindCode: p.type === 'barter_yarn' ? 'yarn' : 'fabric',

            kind: p.type === 'barter_yarn' ? 'نخ مالکي کارمزدي' : 'پارچه مالکي تهاتري',

            itemName: p.itemName,

            quantity: p.quantity,

            unitPrice: p.unitPrice,

            amount: p.amount,

            date: today(),

          });

        }

      });



      return {

        ...prev,

        invoices: [invoice, ...prev.invoices.filter(x => x.number !== invoice.number)],

        receivableDocs,

        movements,

        ownedInventory,

      };

    });



    setEditingNumber('');

    setManualMode(false);

    setSelected(selectableRows.find(row => String(row.shom_f_khor) !== String(invoice.number)) || null);

    setTermCashPercent(30);

    setTermCheckPercent(70);

    setTermCheckMonths(3);

    setCostUnitPrice(0);

    setTaxable(false);

    setPayments([newPaymentLine('credit')]);

  };

  const removeInvoice = invoice => {

    if (!window.confirm(`فاکتور مالی شماره ${invoice.number} حذف شود؟`)) return;

    setFinance(prev => ({

      ...prev,

      invoices: prev.invoices.filter(row => row.id !== invoice.id),

      receivableDocs: prev.receivableDocs.filter(row => String(row.sourceInvoice) !== String(invoice.number)),

      movements: prev.movements.filter(row => String(row.sourceInvoice) !== String(invoice.number)),

      ownedInventory: (prev.ownedInventory || []).filter(row => String(row.sourceInvoice) !== String(invoice.number)),

    }));

    if (String(editingNumber) === String(invoice.number)) {

      setEditingNumber('');

      setManualMode(false);

      setSelected(null);

    }

  };

  const printInvoice = invoice => {

    const paymentsText = (invoice.payments || []).map(row => `${paymentTypes.find(type => type.id === row.type)?.label || row.type}: ${money(row.amount)} تومان`).join('<br>') || '-';

    printSection(`فاکتور مالی شماره ${invoice.number}`, `<table><tbody><tr><th>تاریخ</th><td>${toJalali(invoice.date)}</td><th>شخص</th><td>${invoice.customer || '-'}</td></tr><tr><th>کالا</th><td>${invoice.item || '-'}</td><th>مقدار</th><td>${num(invoice.quantity)}</td></tr><tr><th>نرخ واحد</th><td>${money(invoice.unitPrice)}</td><th>مبلغ کل</th><td>${money(invoice.total)}</td></tr><tr><th>روش تسویه</th><td colspan="3">${paymentsText}</td></tr></tbody></table>`);

  };



  return (

    <div className="space-y-5">

      <div className="grid grid-cols-[0.55fr_1.7fr] gap-5">

        <Card>

        <div className="mb-4 space-y-3">

          <h3 className="font-bold">منبع فاکتور مالی</h3>

          <PrimaryButton className="w-full" onClick={beginManualInvoice}>صدور فاکتور جدید مستقل</PrimaryButton>

          <p className="text-xs leading-6 text-slate-400">یا یکی از فاکتورهای خروج عملیاتی زیر را برای تعیین قیمت و تسویه انتخاب کنید.</p>

        </div>

        <div className="max-h-[620px] space-y-2 overflow-auto pl-1">

          {selectableRows.map(row => (

            <button key={row.id_f_khor} className={`w-full rounded-md border p-3 text-right ${!manualMode && selected?.id_f_khor === row.id_f_khor ? 'border-blue-500 bg-blue-950' : 'border-slate-700 bg-slate-900 hover:bg-slate-800'}`} onClick={() => { setManualMode(false); setEditingNumber(''); setSelected(row); }}>

              <div className="flex justify-between gap-3"><strong>شماره {row.shom_f_khor}</strong><span className="text-sm text-slate-400">{row.tarikh_f_khor}</span></div>

              <div className="mt-2 text-sm text-slate-300">{row.mosh_f_khor}</div>

              <div className="mt-2 grid grid-cols-3 gap-2 text-xs text-slate-400"><span>کالا: {row.kala_name}</span><span>متر: {num(row.metr_salon)}</span><span>وزن: {num(row.w_salon)}</span></div>

            </button>

          ))}

          {!selectableRows.length && <EmptyState />}

        </div>

      </Card>



      <Card>

        <h3 className="mb-4 font-bold">ثبت و تعيين تکليف فاکتور</h3>

        {selected ? (

          <div className="space-y-4">

            {manualMode ? <div className="grid grid-cols-3 gap-3">

              <label className="text-sm text-slate-300">شماره فاکتور<TextInput className="mt-2 w-full" value={selected.shom_f_khor || ''} onChange={e => updateSelected({ shom_f_khor: e.target.value })} /></label>

              <label className="text-sm text-slate-300">تاریخ<DateInput className="mt-2 w-full" value={selected.tarikh_f_khor || today()} onChange={e => updateSelected({ tarikh_f_khor: e.target.value })} /></label>

              <label className="text-sm text-slate-300">شخص / مشتری<TextInput className="mt-2 w-full" list="manual-financial-customers" value={selected.mosh_f_khor || ''} onChange={e => updateSelected({ mosh_f_khor: e.target.value })} /><datalist id="manual-financial-customers">{customerNames.map(name => <option key={name} value={name} />)}</datalist></label>

              <label className="text-sm text-slate-300">کالا یا خدمت<TextInput className="mt-2 w-full" list="manual-financial-items" value={selected.kala_name || ''} onChange={e => updateSelected({ kala_name: e.target.value })} /><datalist id="manual-financial-items">{itemNames.map(name => <option key={name} value={name} />)}</datalist></label>

              <label className="text-sm text-slate-300">وزن<TextInput className="mt-2 w-full" type="number" min="0" step="0.001" value={selected.w_salon || ''} onChange={e => updateSelected({ w_salon: Number(e.target.value || 0) })} /></label>

              <label className="text-sm text-slate-300">متراژ<TextInput className="mt-2 w-full" type="number" min="0" step="0.01" value={selected.metr_salon || ''} onChange={e => updateSelected({ metr_salon: Number(e.target.value || 0) })} /></label>

            </div> : <div className="grid grid-cols-3 gap-3">

              <Field label="شماره فاکتور" value={selected.shom_f_khor} />

              <Field label="مشتري" value={selected.mosh_f_khor} />

              <Field label="کالا" value={selected.kala_name} />

              <Field label="تاريخ شمسي" value={selected.tarikh_f_khor} />

              <Field label="متراژ" value={num(selected.metr_salon)} />

              <Field label="وزن" value={num(selected.w_salon)} />

            </div>}

            <div className="grid grid-cols-6 gap-3">

              <label className="text-sm text-slate-300">نوع فاکتور<SelectInput className="mt-2 w-full" value={pricingMode} onChange={e => setPricingMode(e.target.value)}><option value="commission">اجرت بافت</option><option value="sale">فروش</option></SelectInput></label>

              <label className="text-sm text-slate-300">مبناي قيمت<SelectInput className="mt-2 w-full" value={basis} onChange={e => setBasis(e.target.value)}><option value="weight">وزن</option><option value="meter">متر</option></SelectInput></label>

              <label className="text-sm text-slate-300">نرخ واحد<TextInput className="mt-2 w-full" type="number" value={unitPrice} onChange={e => setUnitPrice(e.target.value)} /></label>

              {pricingMode === 'sale' && <label className="text-sm text-slate-300">بهای تمام‌شده واحد<TextInput className="mt-2 w-full" type="number" min="0" value={costUnitPrice} onChange={e => setCostUnitPrice(e.target.value)} /></label>}

              <label className="text-sm text-slate-300">درصد نقد<TextInput className="mt-2 w-full" type="number" min="0" max="100" value={termCashPercent} onChange={e => { const v = Number(e.target.value || 0); setTermCashPercent(v); setTermCheckPercent(Math.max(0, 100 - v)); }} /></label>

              <label className="text-sm text-slate-300">درصد چک<TextInput className="mt-2 w-full" type="number" min="0" max="100" value={termCheckPercent} onChange={e => setTermCheckPercent(Number(e.target.value || 0))} /></label>

              <label className="text-sm text-slate-300">چک چند ماهه<TextInput className="mt-2 w-full" type="number" min="0" value={termCheckMonths} onChange={e => setTermCheckMonths(Number(e.target.value || 0))} /></label>

            </div>

            <label className="flex items-center gap-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-sm"><input type="checkbox" checked={taxable} onChange={e => setTaxable(e.target.checked)} /><span>این فاکتور مشمول مالیات است؛ نرخ {num(finance.accountingSettings?.defaultVatRate || 0)}٪ و مبلغ مالیات {money(taxAmount)} تومان</span></label>



            <div className="rounded-md border border-slate-700 bg-slate-900 p-4">

              <div className="mb-3 flex items-center justify-between">

                <h4 className="font-bold">رديف هاي تعيين تکليف و تسويه</h4>

                <PrimaryButton onClick={() => setPayments(prev => [...prev, newPaymentLine('credit')])}>افزودن رديف</PrimaryButton>

              </div>

              <div className="space-y-3">

                {payments.map(p => (

                  <PaymentLine key={p.id} payment={p} accounts={finance.accounts} yarnItems={data.yarn} fabricItems={data.kala} onChange={patch => updatePayment(p.id, patch)} onRemove={() => removePayment(p.id)} />

                ))}

              </div>

            </div>



            <div className="grid grid-cols-3 gap-3">

              <Field label="مبلغ فاکتور" value={`${money(total)} تومان`} />

              <Field label="جمع تعیین تکلیف" value={`${money(paid)} تومان`} tone={paid === total ? 'text-emerald-300' : 'text-amber-300'} />

              <Field label="مانده تعيين نشده" value={`${money(remaining)} تومان`} tone={remaining === 0 ? 'text-emerald-300' : 'text-red-300'} />

            </div>

            <PrimaryButton className="w-full" onClick={saveSettlement}>ثبت فاکتور و اعمال اسناد، بانک و انبار مالکي</PrimaryButton>

          </div>

        ) : <p className="text-slate-400">فاکتور عملياتي براي انتخاب وجود ندارد.</p>}

      </Card>

      </div>

      <Card>

        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">

          <h3 className="font-bold">فاکتورهاي مالي ثبت شده</h3>

          <div className="flex gap-2">

            <PrimaryButton onClick={() => printSection('فاکتورهای مالی', `<table><thead><tr><th>شماره</th><th>تاریخ</th><th>شخص</th><th>کالا</th><th>مبلغ</th><th>مانده</th></tr></thead><tbody>${finance.invoices.map(row => `<tr><td>${row.number}</td><td>${toJalali(row.date)}</td><td>${row.customer}</td><td>${row.item}</td><td>${money(row.total)}</td><td>${money(invoiceDebt(row))}</td></tr>`).join('')}</tbody></table>`)}>چاپ گزارش</PrimaryButton>

            <PrimaryButton onClick={() => exportExcel('فاکتورهای مالی', finance.invoices.map(row => ({ ...row, debt: invoiceDebt(row), paid: paidAmount(row) })), [['number','شماره'],['date','تاریخ'],['customer','شخص'],['item','کالا'],['quantity','مقدار'],['unitPrice','نرخ واحد'],['total','مبلغ'],['paid','تسویه'],['debt','مانده']])}>خروجی اکسل</PrimaryButton>

          </div>

        </div>

        <FinancialInvoiceTable rows={finance.invoices} onEdit={editInvoice} onPrint={printInvoice} onDelete={removeInvoice} />

      </Card>

    </div>

  );

}



function IncomingPaymentLine({ payment, accounts, receivableDocs, onChange, onRemove }) {

  const type = payment.type;

  const typedNo = String(payment.checkNo || '').trim();

  const matchingDocs = type === 'assign_receivable' && typedNo

    ? receivableDocs.filter(x => String(x.checkNo || '').includes(typedNo))

    : [];

  const selectedDoc = receivableDocs.find(x => x.id === payment.docId) || (matchingDocs.length === 1 ? matchingDocs[0] : null);

  const changeType = nextType => onChange({ type: nextType, amount: '', docId: '', checkNo: '', bankName: '', dueDate: '', dueJalali: '', accountId: '', trackingNo: '' });

  const chooseDoc = doc => onChange({ docId: doc.id, checkNo: doc.checkNo || '', amount: doc.amount || '', bankName: doc.bank || '', dueDate: doc.dueDate || '', dueJalali: doc.dueJalali || '' });

  return <div className="rounded-md border border-slate-700 bg-slate-950 p-3"><div className="grid grid-cols-5 gap-2"><SelectInput value={type} onChange={e => changeType(e.target.value)}><option value="credit">نسيه/بستانکاري شخص</option><option value="cash">نقدي از بانک/صندوق</option><option value="check">چک پرداختي جديد</option><option value="assign_receivable">واگذاري چک دريافتي</option></SelectInput><TextInput type="number" placeholder="مبلغ" value={payment.amount} onChange={e => onChange({ amount: e.target.value })} readOnly={type === 'assign_receivable'} />{type === 'cash' && <SelectInput value={payment.accountId} onChange={e => onChange({ accountId: e.target.value })}><option value="">انتخاب بانک/صندوق</option>{accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput>}{type === 'cash' && <TextInput placeholder="شماره رهگيري" value={payment.trackingNo} onChange={e => onChange({ trackingNo: e.target.value })} />}{type === 'check' && <TextInput placeholder="شماره چک" value={payment.checkNo} onChange={e => onChange({ checkNo: e.target.value })} />}{type === 'check' && <TextInput placeholder="نام بانک" value={payment.bankName} onChange={e => onChange({ bankName: e.target.value })} />}{type === 'check' && <TextInput placeholder="سررسيد شمسي" value={payment.dueJalali} onChange={e => onChange({ dueJalali: e.target.value })} />}{type === 'check' && <DateInput value={payment.dueDate} onChange={e => onChange({ dueDate: e.target.value })} />}{type === 'assign_receivable' && <TextInput placeholder="شماره چک دریافتی را وارد کنید" value={payment.checkNo} onChange={e => onChange({ checkNo: e.target.value, docId: '', amount: '' })} />}{type === 'assign_receivable' && <div className="col-span-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-xs text-slate-200">{selectedDoc ? <div className="grid grid-cols-5 gap-2"><span>از: {selectedDoc.customer || '-'}</span><span>مبلغ: {money(selectedDoc.amount)}</span><span>بانک: {selectedDoc.bank || '-'}</span><span>سررسيد: {selectedDoc.dueJalali || toJalali(selectedDoc.dueDate) || '-'}</span><GhostButton onClick={() => chooseDoc(selectedDoc)}>انتخاب اين چک</GhostButton></div> : typedNo ? <span className="text-amber-200">چکي با اين شماره پيدا نشد يا چند مورد مشابه وجود دارد. از ليست زير انتخاب کنيد.</span> : <span>شماره چک را وارد کنيد تا مشخصات نمايش داده شود.</span>}{matchingDocs.length > 1 && <div className="mt-2 space-y-2">{matchingDocs.map(doc => <button key={doc.id} type="button" className="w-full rounded-md border border-slate-600 bg-slate-950 p-2 text-right hover:border-blue-500" onClick={() => chooseDoc(doc)}>شماره {doc.checkNo} | {doc.customer || '-'} | {money(doc.amount)} | {doc.bank || '-'}</button>)}</div>}</div>}<GhostButton onClick={onRemove}>حذف رديف</GhostButton></div></div>;

}



function PaymentLine({ payment, accounts, yarnItems, fabricItems, onChange, onRemove }) {

  const isCash = payment.type === 'cash';

  const isCheck = payment.type === 'check';

  const isBarterYarn = payment.type === 'barter_yarn';

  const isBarterFabric = payment.type === 'barter_fabric';

  const items = isBarterYarn ? yarnItems : fabricItems;

  const updateBarterCalc = patch => {

    const next = { ...payment, ...patch };

    const shouldCalc = next.type === 'barter_yarn' || next.type === 'barter_fabric';

    const quantity = Number(next.quantity || 0);

    const unitPrice = Number(next.unitPrice || 0);

    onChange(shouldCalc ? { ...patch, amount: quantity && unitPrice ? quantity * unitPrice : '' } : patch);

  };



  return (

    <div className="rounded-md border border-slate-700 bg-slate-950 p-3">

      <div className="grid grid-cols-5 gap-2">

        <SelectInput value={payment.type} onChange={e => onChange({ type: e.target.value, amount: '', itemName: '', quantity: '', unitPrice: '' })}>{paymentTypes.map(t => <option key={t.id} value={t.id}>{t.label}</option>)}</SelectInput>

        <TextInput type="number" placeholder="مبلغ" value={payment.amount} onChange={e => onChange({ amount: e.target.value })} readOnly={isBarterYarn || isBarterFabric} />

        {isCash && <SelectInput value={payment.accountId} onChange={e => onChange({ accountId: e.target.value })}><option value="">محل واريز</option>{accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput>}

        {isCash && <TextInput placeholder="شماره رهگيري" value={payment.trackingNo} onChange={e => onChange({ trackingNo: e.target.value })} />}

        {isCheck && <TextInput placeholder="شماره چک" value={payment.checkNo} onChange={e => onChange({ checkNo: e.target.value })} />}

        {isCheck && <TextInput placeholder="نام بانک" value={payment.bankName} onChange={e => onChange({ bankName: e.target.value })} />}

        {isCheck && <TextInput placeholder="سررسيد شمسي مثل 1405/04/30" value={payment.dueJalali} onChange={e => onChange({ dueJalali: e.target.value })} />}

        {isCheck && <DateInput value={payment.dueDate} onChange={e => onChange({ dueDate: e.target.value })} title="تاريخ کمکي براي گزارش ماهانه" />}

        {(isBarterYarn || isBarterFabric) && <SelectInput value={payment.itemName} onChange={e => onChange({ itemName: e.target.value })}><option value="">انتخاب {isBarterYarn ? 'نخ' : 'پارچه'}</option>{items.map(x => <option key={x.id} value={x.name}>{x.name}</option>)}</SelectInput>}

        {(isBarterYarn || isBarterFabric) && <TextInput type="number" placeholder={isBarterYarn ? 'وزن/مقدار' : 'متراژ/مقدار'} value={payment.quantity} onChange={e => updateBarterCalc({ quantity: e.target.value })} />}

        {(isBarterYarn || isBarterFabric) && <TextInput type="number" placeholder="نرخ واحد" value={payment.unitPrice} onChange={e => updateBarterCalc({ unitPrice: e.target.value })} />}

        <TextInput className="col-span-3" placeholder="توضيحات" value={payment.description} onChange={e => onChange({ description: e.target.value })} />

        <DangerButton onClick={onRemove}>حذف</DangerButton>

      </div>

    </div>

  );

}



function InventoryPage({ finance }) {

  const { data, loading, error } = useOperationalData();

  const [kindFilter, setKindFilter] = useState('summary');

  const [itemFilter, setItemFilter] = useState('all');

  const inWeight = data.yarnIn.reduce((s, x) => s + Math.abs(Number(x.weight || 0)), 0);

  const outWeight = data.yarnOut.reduce((s, x) => s + Math.abs(Number(x.weight || 0)), 0);

  const barterYarn = finance.ownedInventory.filter(x => x.kindCode === 'yarn' || x.kind?.includes('نخ')).reduce((s, x) => s + Number(x.quantity || 0), 0);

  const partsQty = data.spareParts.reduce((s, x) => s + Number(x.quantity || 0), 0);

  const baseRows = Object.values([...data.yarnIn, ...data.yarnOut].reduce((acc, row) => {

    const key = row.yarn_name || 'نامشخص';

    acc[key] = acc[key] || { yarn: key, in: 0, out: 0 };

    if (row.type === 'incoming') acc[key].in += Math.abs(Number(row.weight || 0));

    else acc[key].out += Math.abs(Number(row.weight || 0));

    return acc;

  }, {})).map(x => {

    const ownedRows = finance.ownedInventory.filter(i => i.itemName === x.yarn && (i.kindCode === 'yarn' || i.kind?.includes('نخ')));

    const owned = ownedRows.reduce((s, i) => s + Number(i.quantity || 0), 0);

    const value = ownedRows.reduce((s, i) => s + Number(i.amount || 0), 0);

    return { yarn_name: x.yarn, incoming_weight: x.in, outgoing_weight: x.out, balance_weight: x.in - x.out, owned_barter_weight: owned, owned_barter_value: value };

  });

  const ownedRows = finance.ownedInventory.filter(x => (kindFilter === 'owned-yarn' ? x.kindCode === 'yarn' : kindFilter === 'fabric' ? x.kindCode === 'fabric' : true) && (itemFilter === 'all' || x.itemName === itemFilter));

  const stockRows = ownedRows.map(x => ({ date: x.date, source: x.sourceYarnOutInvoice || x.sourceIncomingInvoice || x.sourceInvoice || '-', itemName: x.itemName, kind: x.kind, customer: x.customer, quantity: x.quantity, unit_price: x.unitPrice, amount: x.amount }));

  const sparePartRows = data.spareParts.map(x => ({ date: x.date, part_name: x.part_name, part_number: x.part_number, quantity: x.quantity, condition_status: x.condition_status, vendor_name: x.vendor_name, description: x.description }));

  const yarnIncomingRows = data.yarnIn

    .filter(x => itemFilter === 'all' || x.yarn_name === itemFilter)

    .map(x => ({ date: x.date, doc_no: x.doc_no, customer_name: x.customer_name, yarn_name: x.yarn_name, incoming_weight: x.weight, source: 'ورود نخ عملياتي' }));

  const yarnOutgoingRows = data.yarnOut

    .filter(x => itemFilter === 'all' || x.yarn_name === itemFilter)

    .map(x => ({ date: x.date, doc_no: x.doc_no, customer_name: x.customer_name, yarn_name: x.yarn_name, outgoing_weight: x.weight, source: 'خروج نخ عملياتي' }));

  const summaryRows = baseRows.filter(x => itemFilter === 'all' || x.yarn_name === itemFilter);

  const rows = kindFilter === 'incoming-yarn'

    ? yarnIncomingRows

    : kindFilter === 'outgoing-yarn'

      ? yarnOutgoingRows

      : kindFilter === 'owned-yarn' || kindFilter === 'fabric'

        ? stockRows

        : kindFilter === 'parts'

          ? sparePartRows.filter(x => itemFilter === 'all' || x.part_name === itemFilter)

          : summaryRows;

  const itemOptions = kindFilter === 'fabric'

    ? [...new Set(finance.ownedInventory.filter(x => x.kindCode === 'fabric').map(x => x.itemName).filter(Boolean))]

    : kindFilter === 'parts'

      ? [...new Set(sparePartRows.map(x => x.part_name).filter(Boolean))]

    : kindFilter === 'owned-yarn'

      ? [...new Set(finance.ownedInventory.filter(x => x.kindCode === 'yarn').map(x => x.itemName).filter(Boolean))]

      : [...new Set([...data.yarnIn, ...data.yarnOut].map(x => x.yarn_name).filter(Boolean))];

  const printInventory = () => {

    const cols = Object.keys(rows[0] || {});

    const html = `<table><thead><tr>${cols.map(k => `<th>${columnLabels[k] || k}</th>`).join('')}</tr></thead><tbody>${rows.map(row => `<tr>${cols.map(k => `<td>${k.toLowerCase().includes('date') || k === 'تاريخ' ? toJalali(row[k]) : row[k] ?? ''}</td>`).join('')}</tr>`).join('')}</tbody></table>`;

    printSection('گزارش انبار نخ و پارچه', html);

  };



  return (

    <div className="space-y-5">

      <div className="rounded-md border border-blue-800 bg-blue-950 p-3 text-sm text-blue-100">اين تب با هر بار ورود/رفرش از API عملياتي به روز مي شود. تهاترهاي نخ و پارچه ثبت شده در فاکتور مالي هم به موجودي مالکي اضافه مي شوند.</div>

      <div className="grid grid-cols-4 gap-4">

        <Field label="ورود نخ عملیاتی" value={`${num(inWeight)} کيلو`} tone="text-emerald-300" />

        <Field label="خروج نخ عملیاتی" value={`${num(outWeight)} کيلو`} tone="text-amber-300" />

        <Field label="مانده عملیاتی" value={`${num(inWeight - outWeight)} کيلو`} />

        <Field label="نخ مالکی از تهاتر" value={`${num(barterYarn)} مقدار`} tone="text-blue-300" />

        <Field label="موجودی قطعات عملیاتی" value={`${num(partsQty)} عدد`} tone="text-violet-300" />

      </div>

      <Card><div className="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">گزارش انبار نخ، پارچه و قطعات</h3><div className="flex gap-2"><SelectInput value={kindFilter} onChange={e => { setKindFilter(e.target.value); setItemFilter('all'); }}><option value="summary">خلاصه نخ عملياتي</option><option value="incoming-yarn">ريز ورود نخ عملياتي</option><option value="outgoing-yarn">ريز خروج نخ عملياتي</option><option value="owned-yarn">نخ مالکي/تهاتر</option><option value="fabric">پارچه مالکي/تهاتر</option><option value="parts">قطعات</option></SelectInput><SelectInput value={itemFilter} onChange={e => setItemFilter(e.target.value)}><option value="all">همه اقلام</option>{itemOptions.map(x => <option key={x} value={x}>{x}</option>)}</SelectInput><PrimaryButton onClick={printInventory}>چاپ</PrimaryButton><PrimaryButton onClick={() => exportExcel('گزارش انبار نخ، پارچه و قطعات', rows)}>خروجی اکسل</PrimaryButton></div>{loading && <span className="text-sm text-slate-400">در حال دريافت...</span>}</div>{error ? <ErrorBox message={error} /> : <GenericTable rows={rows} />}</Card>

      <Card><h3 className="mb-4 font-bold">آخرين ورود نخ عملياتي</h3><GenericTable rows={yarnIncomingRows.slice(0, 20)} /></Card>

      <Card><h3 className="mb-4 font-bold">موجودي مالي نخ و پارچه حاصل از تهاتر و فاکتور ورود</h3><GenericTable rows={finance.ownedInventory.map(x => ({ date: x.date, source: x.sourceIncomingInvoice || x.sourceInvoice || '-', customer: x.customer, kind: x.kind, itemName: x.itemName, quantity: x.quantity, unit_price: x.unitPrice, amount: x.amount }))} /></Card>

    </div>

  );

}



function CostsPage({ finance, setFinance }) {

  const { data, loading, error } = useOperationalData();

  const [term, setTerm] = useState(() => {
    try {
      const trace = localStorage.getItem('textile-expense-trace-filter') || '';
      if (trace) localStorage.removeItem('textile-expense-trace-filter');
      return trace;
    } catch {
      return '';
    }
  });

  const [categoryFilter, setCategoryFilter] = useState('all');

  const [subgroupFilter, setSubgroupFilter] = useState('all');

  const [sourceFilter, setSourceFilter] = useState('all');

  const [accountFilter, setAccountFilter] = useState('all');

  const [editingId, setEditingId] = useState('');

  const [formMessage, setFormMessage] = useState({ type: '', text: '' });

  const groups = finance.smsGroups?.length ? finance.smsGroups : defaultExpenseGroups;
  const counterparties = [...new Set([
    ...data.customers.map(x => x.name),
    ...finance.invoices.map(x => x.customer),
    ...finance.incomingInvoices.map(x => x.customer),
    ...(finance.yarnOutInvoices || []).map(x => x.customer),
    ...(finance.openingBalances || []).map(x => x.customer),
    ...finance.movements.flatMap(x => [confirmedMovementCounterparty(x), x.counterpartyCandidate]),
  ].filter(Boolean))];
  const emptyExpenseForm = () => ({ date: today(), operationalDate: '', group: groups[0]?.name || '', subgroup: groups[0]?.subgroups?.[0] || '', amount: '', description: '', accountId: finance.accounts[0]?.id || '', payer: '', source_type: 'manual', sourceId: '', documentNo: '', expenseTraceId: '' });

  const [form, setForm] = useState(emptyExpenseForm);

  const settledOperational = new Set(finance.expenses.map(x => `${x.source_type || 'manual'}:${x.sourceId || x.id}`));

  const operational = data.expenses
    .filter(x => !settledOperational.has(`operational_expense:${x.id}`))
    .map(mapOperationalExpense);

  const operationalById = new Map(data.expenses.map(x => [String(x.id), x]));

  const financial = finance.expenses.map(x => {
    const operationalSource = x.source_type === 'operational_expense' ? operationalById.get(String(x.sourceId)) : null;
    const documentNo = x.documentNo || operationalSource?.shomare_sanad || operationalSource?.doc_no || '';
    const traceId = expenseTraceId({ ...x, documentNo });
    return {
      ...x,
      date: x.date || operationalSource?.date || operationalSource?.tarikh || '',
      group: operationalSource?.title || operationalSource?.onvan_hazine || expenseGroup(x),
      subgroup: operationalSource?.weaver_name || expenseSubgroup(x),
      amount: Number(x.amount ?? operationalSource?.mablagh ?? 0),
      description: operationalSource?.description ?? operationalSource?.tozih ?? x.description ?? '',
      documentNo,
      expenseTraceId: traceId,
      source: expenseSourceLabel(x),
      financialRecord: true,
    };
  });

  const allExpenseRows = [...financial, ...operational];

  const groupOptions = [...new Set(allExpenseRows.map(x => x.group).filter(Boolean))];

  const subgroupOptions = [...new Set(allExpenseRows.filter(x => categoryFilter === 'all' || x.group === categoryFilter).map(x => x.subgroup).filter(Boolean))];

  const sourceOptions = [...new Set(allExpenseRows.map(x => x.source).filter(Boolean))];

  const rows = allExpenseRows.filter(x => matchesExpenseFilters(x, { term, group: categoryFilter, subgroup: subgroupFilter, source: sourceFilter, accountId: accountFilter }));

  const total = rows.reduce((s, x) => s + Number(x.amount || 0), 0);

  const printCosts = () => {

    const html = `<p>منبع: ${sourceFilter === 'all' ? 'همه' : sourceFilter} | گروه: ${categoryFilter === 'all' ? 'همه' : categoryFilter} | زیرگروه: ${subgroupFilter === 'all' ? 'همه' : subgroupFilter} | حساب: ${accountFilter === 'all' ? 'همه' : finance.accounts.find(a => a.id === accountFilter)?.name || '-'}</p><table><thead><tr><th>منبع</th><th>تاریخ</th><th>شناسه سند</th><th>گروه</th><th>زیرگروه</th><th>مبلغ</th><th>توضیحات</th></tr></thead><tbody>${rows.map(x => `<tr><td>${x.source}</td><td>${toJalali(x.date) || ''}</td><td>${expenseTraceId(x) || '-'}</td><td>${expenseGroup(x)}</td><td>${expenseSubgroup(x)}</td><td>${money(x.amount)}</td><td>${x.description || ''}</td></tr>`).join('')}</tbody></table>`;

    printSection('گزارش هزينه ها', html);

  };



  const saveExpense = e => {

    e.preventDefault();

    setFormMessage({ type: '', text: '' });

    const missing = [];

    if (!form.date) missing.push('تاریخ');

    if (!form.group) missing.push('گروه');

    if (!form.subgroup) missing.push('زیرگروه');

    if (!Number(form.amount || 0) || Number(form.amount) <= 0) missing.push('مبلغ معتبر');

    if (!form.accountId) missing.push('بانک یا صندوق پرداخت‌کننده');

    if (missing.length) {

      setFormMessage({ type: 'error', text: `این موارد را کامل کنید: ${missing.join('، ')}` });

      return;

    }

    setFinance(prev => {

      const previousExpense = editingId ? prev.expenses.find(x => x.id === editingId) : null;

      const partyName = String(form.payer || '').trim();
      const expenseId = editingId || uid('exp');
      const documentNo = String(form.documentNo || '').trim();
      const traceId = expenseTraceId({ ...form, id: expenseId, documentNo });

      const expense = {
        id: expenseId, ...form, documentNo, expenseTraceId: traceId, payer: partyName, customer: partyName,
        amount: Number(form.amount), counterpartyConfirmed: Boolean(partyName),
        counterpartySource: partyName ? 'expense_form' : '', source: 'مالی',
      };

      const expenses = editingId ? prev.expenses.map(x => x.id === editingId ? expense : x) : [expense, ...prev.expenses];

      const movementBase = {
        id: uid('mov'), accountId: form.accountId, date: form.date, direction: 'out',
        transactionType: 'expense', amount: Number(form.amount),
        description: `هزینه: ${form.group} / ${form.subgroup} | سند ${traceId}`,
        group: form.group, subgroup: form.subgroup, sourceExpense: expense.id, sourceExpenseTraceId: traceId, documentNo,
      };

      const movement = partyName
        ? { ...confirmMovementCounterparty(movementBase, partyName, 'expense_form'), counterpartyConfirmedAt: today() }
        : movementBase;

      const isExpenseMovement = x => x.sourceExpense === editingId || (
        previousExpense && !x.sourceExpense && x.direction === 'out' && x.accountId === previousExpense.accountId &&
        Number(x.amount || 0) === Number(previousExpense.amount || 0) && x.date === previousExpense.date &&
        String(x.description || '').includes(previousExpense.subgroup || previousExpense.title || '')
      );

      const movements = editingId
        ? (prev.movements.some(isExpenseMovement)
          ? prev.movements.map(x => isExpenseMovement(x) ? { ...movement, id: x.id } : x)
          : [movement, ...prev.movements])
        : [movement, ...prev.movements];

      return { ...prev, expenses, movements };

    });

    const wasEditing = Boolean(editingId);

    setEditingId('');

    setForm(emptyExpenseForm());

    setFormMessage({ type: 'success', text: wasEditing ? 'ویرایش هزینه ثبت و برای ذخیره در سرور ارسال شد.' : 'هزینه ثبت و برای ذخیره در سرور ارسال شد.' });

  };



  const editExpense = row => {

    if (!row.financialRecord) {

      setFormMessage({ type: 'error', text: 'این ردیف عملیاتی هنوز مالی نشده است؛ ابتدا «ثبت در مالی» را بزنید.' });

      return;

    }

    setEditingId(row.id);

    const linkedMovement = finance.movements.find(movement => movement.sourceExpense === row.id);
    const partyRow = linkedMovement || row;
    const existingPayer = confirmedMovementCounterparty(partyRow) || String(partyRow.payer || partyRow.customer || '').trim();
    const traceId = expenseTraceId(row);
    setForm({ date: row.date, operationalDate: row.operationalDate || '', group: expenseGroup(row), subgroup: expenseSubgroup(row), amount: row.amount, description: row.description || '', accountId: row.accountId || finance.accounts[0]?.id || '', payer: existingPayer, source_type: row.source_type || 'manual', sourceId: row.sourceId || '', documentNo: row.documentNo || '', expenseTraceId: traceId });

    setFormMessage({ type: 'success', text: 'اطلاعات هزینه برای ویرایش در فرم قرار گرفت.' });

    window.scrollTo({ top: 0, behavior: 'smooth' });

  };

  const importOperationalExpense = row => {
    if (row.settled) return;
    const documentNo = row.documentNo || row.shomare_sanad || row.doc_no || '';
    const traceId = expenseTraceId({ ...row, source_type: 'operational_expense', sourceId: row.id, documentNo });
    setEditingId('');
    setForm({
      date: row.date || today(), operationalDate: row.date || '', group: row.group || row.title || row.onvan_hazine || 'سایر', subgroup: row.subgroup || row.weaver_name || 'سایر', amount: Number(row.amount ?? row.mablagh ?? 0),
      description: row.description || '',
      accountId: finance.accounts[0]?.id || '', payer: '', source_type: 'operational_expense', sourceId: row.id, documentNo, expenseTraceId: traceId,
    });
    setFormMessage({ type: 'success', text: `اطلاعات هزینه عملیاتی با شناسه ${traceId || '-'} در فرم قرار گرفت؛ حساب پرداخت‌کننده را انتخاب و ثبت را بزنید.` });
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const deleteExpense = id => {
    if (!window.confirm('این هزینه و گردش مالی وابسته به آن حذف شود؟')) return;
    setFinance(prev => {
    const expense = prev.expenses.find(x => x.id === id);
    return {
      ...prev,
      expenses: prev.expenses.filter(x => x.id !== id),
      movements: prev.movements.filter(x => x.sourceExpense !== id && !(
        expense && !x.sourceExpense && x.direction === 'out' && x.accountId === expense.accountId &&
        Number(x.amount || 0) === Number(expense.amount || 0) && x.date === expense.date &&
        String(x.description || '').includes(expense.subgroup || expense.title || '')
      )),
    };
    });
    setFormMessage({ type: 'success', text: 'هزینه حذف و برای ذخیره در سرور ارسال شد.' });
  };



  return (

    <div className="space-y-5">

      <div className="grid grid-cols-4 gap-4"><Field label="تعداد هزينه" value={num(rows.length)} /><Field label="جمع هزينه" value={`${money(total)} تومان`} tone="text-red-300" /><Field label="هزینه عملیاتی" value={num(operational.length)} /><Field label="هزینه مالی" value={num(financial.length)} /></div>

      <Card>

        <h3 className="mb-4 font-bold">{editingId ? 'ويرايش هزينه مالي' : 'ثبت هزينه مالي جديد'}</h3>

        <form className="grid grid-cols-5 gap-3" onSubmit={saveExpense}>

          <DateInput value={form.date} onChange={e => setForm({ ...form, date: e.target.value })} />

          <SelectInput value={form.group} onChange={e => { const next = groups.find(row => row.name === e.target.value); setForm({ ...form, group: e.target.value, subgroup: next?.subgroups?.[0] || '' }); }}><option value="">انتخاب گروه</option>{form.group && !groups.some(row => row.name === form.group) && <option value={form.group}>{form.group}</option>}{groups.map(row => <option key={row.name} value={row.name}>{row.name}</option>)}</SelectInput>

          <SelectInput value={form.subgroup} onChange={e => setForm({ ...form, subgroup: e.target.value })}><option value="">انتخاب زيرگروه</option>{form.subgroup && !(groups.find(row => row.name === form.group)?.subgroups || []).includes(form.subgroup) && <option value={form.subgroup}>{form.subgroup}</option>}{(groups.find(row => row.name === form.group)?.subgroups || []).map(name => <option key={name} value={name}>{name}</option>)}</SelectInput>

          <TextInput type="number" placeholder="مبلغ" value={form.amount} onChange={e => setForm({ ...form, amount: e.target.value })} />

          <SelectInput value={form.accountId} onChange={e => setForm({ ...form, accountId: e.target.value })}>{finance.accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput>

          <TextInput placeholder="شناسه سند هزینه" value={form.expenseTraceId || form.documentNo} onChange={e => setForm({ ...form, expenseTraceId: e.target.value, documentNo: e.target.value })} />

          <SelectInput value={form.payer} onChange={e => setForm({ ...form, payer: e.target.value })}><option value="">طرف حساب اختیاری</option>{counterparties.map(name => <option key={name} value={name}>{name}</option>)}</SelectInput>

          <PrimaryButton type="submit">{editingId ? 'ذخيره ويرايش' : 'ثبت هزينه'}</PrimaryButton>

          <TextInput className="col-span-5" placeholder="توضيحات" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} />

          {form.source_type === 'operational_expense' && <div className="col-span-5 rounded-md border border-amber-700 bg-amber-950 p-3 text-xs text-amber-100">این هزینه از بخش عملیاتی دریافت شده است. حساب پرداخت‌کننده را انتخاب و ثبت مالی را تایید کنید؛ طرف حساب اختیاری است و شناسه سند برای ردیابی بین هزینه و بانک/صندوق نگه داشته می‌شود.</div>}

          {editingId && <GhostButton onClick={() => { setEditingId(''); setForm(emptyExpenseForm()); }}>انصراف</GhostButton>}

        </form>

        {formMessage.text && <div role="status" className={`mt-4 rounded-md border p-3 text-sm ${formMessage.type === 'error' ? 'border-red-800 bg-red-950 text-red-100' : 'border-emerald-800 bg-emerald-950 text-emerald-100'}`}>{formMessage.text}</div>}

      </Card>

      <Card><div className="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">ليست هزينه ها</h3><div className="flex flex-wrap gap-2"><SelectInput value={sourceFilter} onChange={e => setSourceFilter(e.target.value)}><option value="all">همه منابع</option>{sourceOptions.map(name => <option key={name} value={name}>{name}</option>)}</SelectInput><SelectInput value={categoryFilter} onChange={e => { setCategoryFilter(e.target.value); setSubgroupFilter('all'); }}><option value="all">همه گروه‌ها</option>{groupOptions.map(name => <option key={name} value={name}>{name}</option>)}</SelectInput><SelectInput value={subgroupFilter} onChange={e => setSubgroupFilter(e.target.value)}><option value="all">همه زیرگروه‌ها</option>{subgroupOptions.map(name => <option key={name} value={name}>{name}</option>)}</SelectInput><SelectInput value={accountFilter} onChange={e => setAccountFilter(e.target.value)}><option value="all">همه بانک/صندوق</option>{finance.accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput><TextInput placeholder="جستجو در گروه، زیرگروه یا شناسه سند" value={term} onChange={e => setTerm(e.target.value)} />{term && <GhostButton onClick={() => setTerm('')}>پاک کردن جستجو</GhostButton>}<PrimaryButton onClick={printCosts}>چاپ</PrimaryButton><PrimaryButton onClick={() => exportExcel('گزارش هزینه‌ها', rows.map(row => ({ ...row, trace_id: expenseTraceId(row), group_name: expenseGroup(row), subgroup_name: expenseSubgroup(row) })), [['source','منبع'],['date','تاریخ'],['trace_id','شناسه سند'],['group_name','گروه'],['subgroup_name','زیرگروه'],['amount','مبلغ'],['description','توضیحات']], { label: 'جمع کل', amount: total })}>خروجی اکسل</PrimaryButton></div></div>{loading && <p className="text-sm text-slate-400">در حال دريافت...</p>}{error ? <ErrorBox message={error} /> : <ExpensesTable rows={rows} onEdit={editExpense} onDelete={deleteExpense} onImport={importOperationalExpense} highlightTrace={term} />}</Card>

    </div>

  );

}



function DocsPage({ kind, finance, setFinance }) {

  const { data } = useOperationalData();

  const isReceivable = kind === 'receivable';

  const key = isReceivable ? 'receivableDocs' : 'payableDocs';

  const emptyDocForm = () => ({ customer: '', amount: '', checkNo: '', sayadId: '', dueDate: today(), dueJalali: '', bank: '', status: 'open' });

  const [form, setForm] = useState(emptyDocForm);

  const [editingId, setEditingId] = useState('');

  const [actionMessage, setActionMessage] = useState('');

  const [checkbook, setCheckbook] = useState({ bank: '', fromNo: '', toNo: '', title: '' });

  const [editingCheckbookId, setEditingCheckbookId] = useState('');

  const [checkbookMessage, setCheckbookMessage] = useState({ type: '', text: '' });

  const [assignForm, setAssignForm] = useState({ checkNo: '', assignedTo: '' });

  const [filters, setFilters] = useState({ status: 'all', customer: 'all', assignedTo: 'all', fromDate: '', toDate: '' });

  const customers = [...new Set([...data.customers.map(x => x.name), ...finance.invoices.map(x => x.customer), ...finance.incomingInvoices.map(x => x.customer), ...(finance.yarnOutInvoices || []).map(x => x.customer), ...finance.receivableDocs.map(x => x.customer), ...finance.receivableDocs.map(x => x.assignedTo), ...finance.payableDocs.map(x => x.customer), ...(finance.openingBalances || []).map(x => x.customer)].filter(Boolean))];

  const rows = finance[key] || [];

  const assignedCustomers = [...new Set(rows.map(x => x.assignedTo).filter(Boolean))];

  const filteredRows = rows.filter(row => {

    const byStatus = filters.status === 'all' || row.status === filters.status;

    const byCustomer = filters.customer === 'all' || row.customer === filters.customer;

    const byAssigned = filters.assignedTo === 'all' || row.assignedTo === filters.assignedTo;

    const byFrom = !filters.fromDate || row.dueDate >= filters.fromDate;

    const byTo = !filters.toDate || row.dueDate <= filters.toDate;

    return byStatus && byCustomer && byAssigned && byFrom && byTo;

  });

  const matchingChecks = isReceivable && assignForm.checkNo ? finance.receivableDocs.filter(doc => String(doc.checkNo || '').includes(String(assignForm.checkNo))) : [];

  const totalOpen = rows.filter(x => x.status !== 'cleared' && x.status !== 'paid').reduce((s, x) => s + Number(x.amount || 0), 0);

  const matchingCheckbook = !isReceivable && form.bank && form.checkNo ? (finance.checkbooks || []).find(book => book.bank === form.bank && Number(form.checkNo) >= Number(book.fromNo) && Number(form.checkNo) <= Number(book.toNo)) : null;

  const saveCheckbook = e => {

    e.preventDefault();

    const current = editingCheckbookId ? (finance.checkbooks || []).find(book => book.id === editingCheckbookId) : null;

    const validation = validateCheckbookUpdate(current, checkbook, finance.payableDocs || []);

    if (!validation.valid) { setCheckbookMessage({ type: 'error', text: validation.message }); return; }

    setFinance(prev => ({

      ...prev,

      checkbooks: current

        ? (prev.checkbooks || []).map(book => book.id === current.id ? { ...book, ...checkbook, id: book.id } : book)

        : [{ id: uid('book'), ...checkbook }, ...(prev.checkbooks || [])],

    }));


    setCheckbookMessage({ type: 'success', text: current ? 'ویرایش دسته‌چک ذخیره شد.' : 'دسته‌چک جدید ثبت شد.' });

    setCheckbook({ bank: '', fromNo: '', toNo: '', title: '' });

    setEditingCheckbookId('');

  };

  const editCheckbook = book => {

    setEditingCheckbookId(book.id);

    setCheckbook({ title: book.title || '', bank: book.bank || '', fromNo: book.fromNo || '', toNo: book.toNo || '' });

    setCheckbookMessage({ type: '', text: '' });

  };

  const cancelCheckbookEdit = () => {

    setEditingCheckbookId('');

    setCheckbook({ bank: '', fromNo: '', toNo: '', title: '' });

    setCheckbookMessage({ type: '', text: '' });

  };

  const deleteCheckbook = book => {

    const issued = issuedChecksForCheckbook(book, finance.payableDocs || []);

    if (issued.length) {

      setCheckbookMessage({ type: 'error', text: `این دسته‌چک دارای ${issued.length} چک صادرشده است و قابل حذف نیست.` });

      return;

    }

    if (!window.confirm(`دسته‌چک «${book.title || book.bank}» حذف شود؟`)) return;

    setFinance(prev => ({ ...prev, checkbooks: (prev.checkbooks || []).filter(row => row.id !== book.id) }));

    if (editingCheckbookId === book.id) cancelCheckbookEdit();

    setCheckbookMessage({ type: 'success', text: 'دسته‌چک استفاده‌نشده حذف شد.' });

  };

  const save = e => {

    e.preventDefault();

    if (!form.customer || !Number(form.amount || 0)) return;

    if (!String(form.checkNo || '').trim() || !form.dueDate || !String(form.bank || '').trim()) { window.alert('شماره چک، بانک و تاریخ سررسید الزامی است.'); return; }

    if (!isReceivable && !isValidSayadId(form.sayadId)) { window.alert('شناسه صیادی باید دقیقاً ۱۶ رقم باشد.'); return; }

    if (!isReceivable && form.checkNo && !matchingCheckbook && !editingId) { alert('شماره چک در محدوده دسته چک تعريف شده براي اين بانک نيست.'); return; }

    const duplicate = rows.some(row => row.id !== editingId && String(row.checkNo || '').trim() === String(form.checkNo || '').trim() && String(row.bank || '').trim() === String(form.bank || '').trim());

    if (duplicate && !window.confirm('چکی با همین شماره و بانک وجود دارد. از ثبت تکراری مطمئن هستید؟')) return;

    const linkedDocument = editingId ? rows.find(row => row.id === editingId) : null;

    if ((linkedDocument?.sourceInvoice || linkedDocument?.sourceIncomingInvoice) && Number(linkedDocument.amount || 0) !== Number(form.amount || 0)) {

      window.alert('مبلغ این چک به فاکتور وابسته است. برای تغییر مبلغ، تسویه همان فاکتور را ویرایش کنید؛ شماره، بانک و سررسید را می‌توانید از اینجا اصلاح کنید.');

      return;

    }

    setFinance(prev => {

      const current = editingId ? (prev[key] || []).find(row => row.id === editingId) : null;

      const saved = {

        ...(current || {}), ...form, id: editingId || uid(isReceivable ? 'rch' : 'pch'),

        receivedAt: current?.receivedAt || (isReceivable ? today() : ''),

        issuedAt: current?.issuedAt || (isReceivable ? '' : today()),

        checkbookId: matchingCheckbook?.id || current?.checkbookId || '', sayadId: normalizeSayadId(form.sayadId), amount: Number(form.amount),

        updatedAt: today(),

      };

      const documents = editingId ? (prev[key] || []).map(row => row.id === editingId ? saved : row) : [saved, ...(prev[key] || [])];

      const updatePayment = payment => payment.type === 'check' && String(payment.checkNo || '') === String(current?.checkNo || '')

        ? { ...payment, checkNo: form.checkNo, bankName: form.bank, dueDate: form.dueDate, dueJalali: form.dueJalali }

        : payment;

      const invoices = current?.sourceInvoice

        ? (prev.invoices || []).map(invoice => String(invoice.number) === String(current.sourceInvoice) ? { ...invoice, payments: (invoice.payments || []).map(updatePayment) } : invoice)

        : prev.invoices;

      const incomingInvoices = current?.sourceIncomingInvoice

        ? (prev.incomingInvoices || []).map(invoice => invoice.id === current.sourceIncomingInvoice ? { ...invoice, payments: (invoice.payments || []).map(updatePayment) } : invoice)

        : prev.incomingInvoices;

      return { ...prev, [key]: documents, invoices, incomingInvoices };

    });

    setActionMessage(editingId ? 'ویرایش چک ثبت شد.' : 'چک جدید ثبت شد.');

    setEditingId('');

    setForm(emptyDocForm());

  };

  const setStatus = (id, status) => {

    const isClosed = status === 'cleared' || status === 'paid';

    const clearingAccountId = finance.accounts.find(account => account.type === 'بانک')?.id || finance.accounts[0]?.id || '';

    if (isClosed && !clearingAccountId) { window.alert('ابتدا در بانک و صندوق یک حساب تعریف کنید.'); return; }

    const selected = rows.find(row => row.id === id);

    if (!selected) return;

    const isReturn = status === 'returned' || status === 'bounced';

    const label = status === 'returned' ? 'مرجوع' : status === 'bounced' ? 'برگشت‌خورده' : statusLabel(status);

    if (!window.confirm(`وضعیت چک شماره ${selected.checkNo} به «${label}» تغییر کند؟`)) return;

    const reason = isReturn ? window.prompt('علت عملیات را ثبت کنید:', selected.operationReason || '') : '';

    if (isReturn && reason === null) return;

    setFinance(prev => {

      const sourceAssignment = selected.assignedIncomingInvoice || '';

      const documents = (prev[key] || []).map(row => {

        if (row.id !== id) return row;

        return {

          ...row, status, clearingAccountId: isClosed ? clearingAccountId : row.clearingAccountId,

          ...(status === 'cleared' ? { clearedAt: today() } : {}),

          ...(status === 'paid' ? { paidAt: today() } : {}),

          ...(status === 'returned' ? { returnedAt: today() } : {}),

          ...(status === 'bounced' ? { bouncedAt: today() } : {}),

          ...(isReturn ? {

            operationReason: reason || '', previousAssignedTo: row.assignedTo || '',

            previousAssignedIncomingInvoice: row.assignedIncomingInvoice || '', assignedTo: '', assignedIncomingInvoice: '',

          } : {}),

          lastOperation: status, lastOperationAt: today(),

        };

      });

      const incomingInvoices = isReturn && sourceAssignment

        ? (prev.incomingInvoices || []).map(invoice => invoice.id === sourceAssignment

          ? { ...invoice, payments: (invoice.payments || []).map(payment => payment.docId === id ? { ...payment, type: 'credit', docId: '', note: `چک ${label} شد` } : payment) }

          : invoice)

        : prev.incomingInvoices;

      return { ...prev, [key]: documents, incomingInvoices };

    });

    setActionMessage(`عملیات «${label}» برای چک ثبت شد.`);

  };

  const editCheck = row => {

    setEditingId(row.id);

    setForm({ customer: row.customer || '', amount: row.amount || '', checkNo: row.checkNo || '', sayadId: row.sayadId || '', dueDate: row.dueDate || today(), dueJalali: row.dueJalali || '', bank: row.bank || '', status: row.status || 'open' });

    setActionMessage('اطلاعات چک برای ویرایش در فرم بالا قرار گرفت.');

    window.scrollTo({ top: 0, behavior: 'smooth' });

  };

  const remove = id => setFinance(prev => ({ ...prev, [key]: prev[key].filter(x => x.id !== id) }));

  const assignCheck = e => {

    e.preventDefault();

    if (!assignForm.checkNo || !assignForm.assignedTo) return;

    setFinance(prev => ({

      ...prev,

      receivableDocs: prev.receivableDocs.map(doc => String(doc.checkNo) === String(assignForm.checkNo) && (!assignForm.docId || doc.id === assignForm.docId)

        ? { ...doc, status: 'assigned', assignedTo: assignForm.assignedTo, assignedAt: today() }

        : doc),

    }));

    setAssignForm({ checkNo: '', assignedTo: '' });

  };

  const printDocs = () => {

    const optionalHead = isReceivable ? '<th>واگذار شده به</th>' : '<th>شناسه صیادی</th>';

    const html = `<table><thead><tr><th>شخص</th>${optionalHead}<th>مبلغ</th><th>شماره</th><th>سررسيد</th><th>بانک</th><th>وضعيت</th></tr></thead><tbody>${filteredRows.map(row => `<tr><td>${row.customer || ''}</td><td>${isReceivable ? (row.assignedTo || '') : (row.sayadId || 'ثبت نشده')}</td><td>${money(row.amount)}</td><td>${row.checkNo || ''}</td><td>${row.dueJalali || toJalali(row.dueDate) || ''}</td><td>${row.bank || ''}</td><td>${statusLabel(row.status)}</td></tr>`).join('')}</tbody></table>`;

    printSection(isReceivable ? 'گزارش اسناد دريافتي' : 'گزارش اسناد پرداختي', html);

  };

  return (

    <div className="space-y-5">

      <div className="grid grid-cols-3 gap-4"><Field label="تعداد سند" value={num(rows.length)} /><Field label="مبلغ باز" value={`${money(totalOpen)} تومان`} tone={isReceivable ? 'text-blue-300' : 'text-red-300'} /><Field label="سررسيد اين ماه" value={`${money(rows.filter(x => sameMonth(x.dueDate, 0)).reduce((s, x) => s + Number(x.amount || 0), 0))} تومان`} /></div>

      {!isReceivable && <Card><h3 className="mb-4 font-bold">{editingCheckbookId ? 'ویرایش دسته‌چک' : 'تعریف دسته‌چک'}</h3><form className="grid grid-cols-5 gap-3" onSubmit={saveCheckbook}><TextInput placeholder="عنوان دسته‌چک" value={checkbook.title} onChange={e => setCheckbook({ ...checkbook, title: e.target.value })} /><TextInput placeholder="بانک" value={checkbook.bank} onChange={e => setCheckbook({ ...checkbook, bank: e.target.value })} /><TextInput type="number" placeholder="از شماره" value={checkbook.fromNo} onChange={e => setCheckbook({ ...checkbook, fromNo: e.target.value })} /><TextInput type="number" placeholder="تا شماره" value={checkbook.toNo} onChange={e => setCheckbook({ ...checkbook, toNo: e.target.value })} /><div className="flex gap-2"><PrimaryButton className="flex-1" type="submit">{editingCheckbookId ? 'ذخیره ویرایش' : 'ثبت دسته‌چک'}</PrimaryButton>{editingCheckbookId && <GhostButton onClick={cancelCheckbookEdit}>انصراف</GhostButton>}</div></form>{checkbookMessage.text && <div role="status" className={`mt-3 rounded-md border p-3 text-sm ${checkbookMessage.type === 'error' ? 'border-red-800 bg-red-950 text-red-100' : 'border-emerald-800 bg-emerald-950 text-emerald-100'}`}>{checkbookMessage.text}</div>}<div className="mt-4"><CheckbooksTable rows={finance.checkbooks || []} payableDocs={finance.payableDocs || []} onEdit={editCheckbook} onDelete={deleteCheckbook} /></div></Card>}

      <Card><h3 className="mb-4 font-bold">{editingId ? 'ویرایش سند ثبت‌شده' : isReceivable ? 'ثبت سند دريافتي' : 'ثبت سند پرداختي'}</h3><form className="grid gap-3 xl:grid-cols-8" onSubmit={save}><SelectInput value={form.customer} onChange={e => setForm({ ...form, customer: e.target.value })}><option value="">{isReceivable ? 'دريافت از' : 'پرداخت به'}</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><TextInput type="number" placeholder="مبلغ" value={form.amount} onChange={e => setForm({ ...form, amount: e.target.value })} /><TextInput placeholder="شماره چک/سند" value={form.checkNo} onChange={e => setForm({ ...form, checkNo: e.target.value })} />{!isReceivable && <TextInput inputMode="numeric" maxLength={19} placeholder="شناسه ۱۶ رقمی صیادی" value={form.sayadId} onChange={e => setForm({ ...form, sayadId: e.target.value })} />}<TextInput placeholder="سررسيد شمسي" value={form.dueJalali} onChange={e => setForm({ ...form, dueJalali: e.target.value })} /><label className="text-sm text-slate-300"><span className="mb-2 block">تاريخ کمکي</span><DateInput className="w-full" value={form.dueDate} onChange={e => setForm({ ...form, dueDate: e.target.value })} /></label>{isReceivable ? <TextInput placeholder="بانک" value={form.bank} onChange={e => setForm({ ...form, bank: e.target.value })} /> : <SelectInput value={form.bank} onChange={e => setForm({ ...form, bank: e.target.value })}><option value="">انتخاب بانک دسته چک</option>{[...new Set((finance.checkbooks || []).map(x => x.bank).filter(Boolean))].map(b => <option key={b} value={b}>{b}</option>)}</SelectInput>}<PrimaryButton type="submit">{editingId ? 'ذخیره ویرایش' : 'ثبت'}</PrimaryButton>{editingId && <GhostButton onClick={() => { setEditingId(''); setForm(emptyDocForm()); }}>لغو ویرایش</GhostButton>}</form>{actionMessage && <div className="mt-3 rounded-md border border-emerald-800 bg-emerald-950 p-3 text-sm text-emerald-100">{actionMessage}</div>}{!isReceivable && form.checkNo && form.bank && <div className={`mt-3 rounded-md border p-3 text-xs ${matchingCheckbook || editingId ? 'border-emerald-700 bg-emerald-950 text-emerald-100' : 'border-red-700 bg-red-950 text-red-100'}`}>{matchingCheckbook ? `شماره چک در دسته چک ${matchingCheckbook.title || matchingCheckbook.bank} معتبر است.` : editingId ? 'شماره قبلی هنگام ویرایش حفظ می‌شود.' : 'اين شماره چک در محدوده دسته چک هاي تعريف شده اين بانک نيست.'}</div>}</Card>

      {isReceivable && <Card><h3 className="mb-4 font-bold">واگذاري سند دريافتي به شخص ثالث</h3><form className="grid grid-cols-4 gap-3" onSubmit={assignCheck}><TextInput placeholder="شماره چک" value={assignForm.checkNo} onChange={e => setAssignForm({ ...assignForm, checkNo: e.target.value, docId: '' })} /><SelectInput value={assignForm.assignedTo} onChange={e => setAssignForm({ ...assignForm, assignedTo: e.target.value })}><option value="">واگذاري به</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><PrimaryButton type="submit">ثبت واگذاري</PrimaryButton><div className="rounded-md bg-slate-900 px-3 py-2 text-xs text-slate-400">اگر شماره تکراري باشد، ابتدا چک صحيح را از ليست پايين انتخاب کنيد.</div></form>{matchingChecks.length > 0 && <div className="mt-4 rounded-md border border-slate-700 bg-slate-950 p-3"><h4 className="mb-3 text-sm font-bold">مشخصات چک هاي مطابق</h4><div className="space-y-2">{matchingChecks.map(doc => <button key={doc.id} type="button" className={`w-full rounded-md border p-3 text-right text-sm ${assignForm.docId === doc.id ? 'border-blue-500 bg-blue-950' : 'border-slate-700 bg-slate-900'}`} onClick={() => setAssignForm({ ...assignForm, docId: doc.id, checkNo: doc.checkNo })}><div className="grid grid-cols-5 gap-2"><span>از: {doc.customer}</span><span>مبلغ: {money(doc.amount)}</span><span>بانک: {doc.bank || '-'}</span><span>سررسيد: {doc.dueJalali || toJalali(doc.dueDate) || '-'}</span><span>وضعيت: {doc.status}</span></div></button>)}</div></div>}</Card>}

      <Card><div className="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">ليست و گزارش اسناد</h3><div className="flex flex-wrap gap-2"><SelectInput value={filters.status} onChange={e => setFilters({ ...filters, status: e.target.value })}><option value="all">همه وضعيت‌ها</option><option value="open">باز</option><option value="cleared">وصول شد</option><option value="assigned">واگذار شد</option><option value="paid">پرداخت شد</option><option value="returned">مرجوع شده</option><option value="bounced">برگشت‌خورده</option></SelectInput><SelectInput value={filters.customer} onChange={e => setFilters({ ...filters, customer: e.target.value })}><option value="all">همه اشخاص</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput>{isReceivable && <SelectInput value={filters.assignedTo} onChange={e => setFilters({ ...filters, assignedTo: e.target.value })}><option value="all">همه واگذارگيرنده‌ها</option>{assignedCustomers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput>}<DateInput value={filters.fromDate} onChange={e => setFilters({ ...filters, fromDate: e.target.value })} /><DateInput value={filters.toDate} onChange={e => setFilters({ ...filters, toDate: e.target.value })} /><PrimaryButton onClick={printDocs}>چاپ گزارش</PrimaryButton><PrimaryButton onClick={() => exportExcel(isReceivable ? 'اسناد دریافتنی' : 'اسناد پرداختنی', filteredRows.map(row => ({ ...row, status_text: statusLabel(row.status) })), isReceivable ? [['customer','شخص'],['assignedTo','واگذار شده به'],['amount','مبلغ'],['checkNo','شماره'],['dueJalali','سررسید شمسی'],['bank','بانک'],['status_text','وضعیت']] : [['customer','شخص'],['sayadId','شناسه صیادی'],['amount','مبلغ'],['checkNo','شماره'],['dueJalali','سررسید شمسی'],['bank','بانک'],['status_text','وضعیت']])}>خروجی اکسل</PrimaryButton></div></div><DocsTable rows={filteredRows} isReceivable={isReceivable} onStatus={setStatus} onEdit={editCheck} onDelete={remove} /></Card>

    </div>

  );

}



function CheckbooksTable({ rows, payableDocs, onEdit, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">عنوان</th><th className="p-3 text-right">بانک</th><th className="p-3 text-right">از شماره</th><th className="p-3 text-right">تا شماره</th><th className="p-3 text-right">چک صادرشده</th><th className="p-3 text-right">عملیات</th></tr></thead><tbody>{rows.map(book => { const issuedCount = issuedChecksForCheckbook(book, payableDocs).length; return <tr key={book.id} className="border-b border-slate-800"><td className="p-3 font-bold text-blue-200">{book.title || '-'}</td><td className="p-3">{book.bank}</td><td className="p-3">{book.fromNo}</td><td className="p-3">{book.toNo}</td><td className="p-3">{issuedCount}</td><td className="p-3"><div className="flex flex-wrap gap-2"><GhostButton onClick={() => onEdit(book)}>ویرایش</GhostButton><DangerButton disabled={issuedCount > 0} onClick={() => onDelete(book)}>حذف</DangerButton>{issuedCount > 0 && <span className="self-center text-xs text-amber-200">به‌علت صدور چک قابل حذف نیست</span>}</div></td></tr>; })}</tbody></table></div>;

}



function BankCashPage({ finance, setFinance }) {

  const { data } = useOperationalData();

  const [account, setAccount] = useState({ name: '', type: 'بانک', opening: 0 });

  const [movement, setMovement] = useState({ accountId: finance.accounts[0]?.id || '', counterAccountId: '', date: today(), direction: 'in', transactionType: 'customer_receipt', amount: '', payer: '', trackingNo: '', description: '' });

  const [filters, setFilters] = useState({ accountId: 'all', payer: 'all', direction: 'all' });

  const customers = [...new Set([...data.customers.map(x => x.name), ...finance.invoices.map(x => x.customer), ...finance.incomingInvoices.map(x => x.customer), ...(finance.yarnOutInvoices || []).map(x => x.customer), ...finance.receivableDocs.map(x => x.customer), ...finance.receivableDocs.map(x => x.assignedTo), ...finance.payableDocs.map(x => x.customer), ...(finance.openingBalances || []).map(x => x.customer)].filter(Boolean))];

  const movementRows = finance.movements.map(m => ({ ...m, account: finance.accounts.find(a => a.id === m.accountId)?.name || '-', payer: confirmedMovementCounterparty(m) || (m.transactionType === 'transfer' ? '-' : 'تأیید نشده'), directionLabel: m.direction === 'in' ? 'واريز' : 'برداشت' }));

  const filteredRows = movementRows.filter(m => (filters.accountId === 'all' || m.accountId === filters.accountId) && (filters.payer === 'all' || m.payer === filters.payer) && (filters.direction === 'all' || m.direction === filters.direction));

  const addAccount = e => { e.preventDefault(); if (!account.name) return; setFinance(prev => ({ ...prev, accounts: [{ id: uid('acc'), ...account, opening: Number(account.opening || 0) }, ...prev.accounts] })); setAccount({ name: '', type: 'بانک', opening: 0 }); };

  const addMovement = e => {
    e.preventDefault();
    if (!movement.accountId || !Number(movement.amount || 0)) return;
    if (movementNeedsCounterparty(movement) && !movement.payer) { window.alert('انتخاب و تأیید طرف حساب برای ثبت گردش الزامی است.'); return; }
    if (movement.transactionType === 'transfer' && (!movement.counterAccountId || movement.counterAccountId === movement.accountId)) { window.alert('برای انتقال، حساب مقصد متفاوت را انتخاب کنید.'); return; }
    const base = { id: uid('mov'), ...movement, amount: Number(movement.amount) };
    const next = movementNeedsCounterparty(base) ? confirmMovementCounterparty(base, movement.payer, 'bank_form') : base;
    setFinance(prev => ({ ...prev, movements: [next, ...prev.movements] }));
    setMovement({ ...movement, amount: '', payer: '', trackingNo: '', description: '' });
  };

  const printMovements = () => { const body = filteredRows.map(m => '<tr><td>' + (toJalali(m.date) || '') + '</td><td>' + m.account + '</td><td>' + m.payer + '</td><td>' + m.directionLabel + '</td><td>' + money(m.amount) + '</td><td>' + (m.trackingNo || '') + '</td><td>' + (m.description || '') + '</td></tr>').join(''); printSection('گزارش گردش بانک و صندوق', '<table><thead><tr><th>تاريخ</th><th>حساب</th><th>پرداخت کننده</th><th>نوع گردش</th><th>مبلغ</th><th>شماره رهگيري</th><th>شرح</th></tr></thead><tbody>' + body + '</tbody></table>'); };

  return (

    <div className="space-y-5">

      <div className="grid grid-cols-4 gap-4">{finance.accounts.map(a => <Field key={a.id} label={a.type + ': ' + a.name} value={money(accountBalance(a, finance.movements)) + ' تومان'} tone="text-emerald-300" />)}</div>

      <div className="space-y-5">

        <Card><h3 className="mb-4 font-bold">تعريف بانک يا صندوق</h3><form className="grid grid-cols-4 gap-3" onSubmit={addAccount}><TextInput placeholder="نام حساب" value={account.name} onChange={e => setAccount({ ...account, name: e.target.value })} /><SelectInput value={account.type} onChange={e => setAccount({ ...account, type: e.target.value })}><option>بانک</option><option>صندوق</option></SelectInput><TextInput type="number" placeholder="موجودی اولیه" value={account.opening} onChange={e => setAccount({ ...account, opening: e.target.value })} /><PrimaryButton type="submit">ثبت حساب</PrimaryButton></form></Card>

        <Card><h3 className="mb-4 font-bold">ثبت گردش نقدي</h3><form className="grid grid-cols-6 gap-3" onSubmit={addMovement}><SelectInput value={movement.accountId} onChange={e => setMovement({ ...movement, accountId: e.target.value })}>{finance.accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput><label className="text-sm text-slate-300"><span className="mb-2 block">تاريخ کمکي</span><DateInput className="w-full" value={movement.date} onChange={e => setMovement({ ...movement, date: e.target.value })} /></label><SelectInput value={movement.direction} onChange={e => setMovement({ ...movement, direction: e.target.value })}><option value="in">واريز</option><option value="out">برداشت</option></SelectInput><SelectInput value={movement.payer} onChange={e => setMovement({ ...movement, payer: e.target.value })}><option value="">نام پرداخت کننده</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><TextInput type="number" placeholder="مبلغ" value={movement.amount} onChange={e => setMovement({ ...movement, amount: e.target.value })} /><PrimaryButton type="submit">ثبت</PrimaryButton><TextInput placeholder="شماره رهگيري" value={movement.trackingNo} onChange={e => setMovement({ ...movement, trackingNo: e.target.value })} /><TextInput className="col-span-5" placeholder="شرح" value={movement.description} onChange={e => setMovement({ ...movement, description: e.target.value })} /></form></Card>

      </div>

      <Card><div className="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">گردش بانک و صندوق</h3><div className="flex flex-wrap gap-2"><SelectInput value={filters.accountId} onChange={e => setFilters({ ...filters, accountId: e.target.value })}><option value="all">همه حساب ها</option>{finance.accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput><SelectInput value={filters.payer} onChange={e => setFilters({ ...filters, payer: e.target.value })}><option value="all">همه پرداخت کنندگان</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><SelectInput value={filters.direction} onChange={e => setFilters({ ...filters, direction: e.target.value })}><option value="all">همه گردش ها</option><option value="in">واريز</option><option value="out">برداشت</option></SelectInput><PrimaryButton onClick={printMovements}>چاپ گزارش</PrimaryButton><PrimaryButton onClick={() => exportExcel('گردش بانک و صندوق', filteredRows.map(m => ({ date: m.date, account: m.account, payer: m.payer, direction: m.directionLabel, amount: m.amount, trackingNo: m.trackingNo, description: m.description })))}>خروجی اکسل</PrimaryButton></div></div><GenericTable rows={filteredRows.map(m => ({ date: m.date, account: m.account, payer: m.payer, direction: m.directionLabel, amount: m.amount, trackingNo: m.trackingNo, description: m.description }))} /></Card>

    </div>

  );

}



const typedTransactionLabels = {
  CUSTOMER_RECEIPT: 'وصول مشتری', DIRECT_EXPENSE: 'هزینه مستقیم', SUPPLIER_PAYMENT: 'پرداخت تأمین‌کننده',
  PAYROLL_PAYMENT: 'پرداخت حقوق', INTERNAL_TRANSFER: 'انتقال داخلی', PETTY_CASH_FUNDING: 'شارژ تنخواه',
  PETTY_CASH_RETURN: 'برگشت تنخواه', LOAN_RECEIPT: 'دریافت وام', LOAN_REPAYMENT: 'بازپرداخت وام',
  OWNER_DEPOSIT: 'واریز مالک', OWNER_WITHDRAWAL: 'برداشت مالک', ASSET_PURCHASE: 'خرید دارایی',
  BANK_FEE: 'کارمزد بانکی', CHECK_RECEIPT: 'دریافت چک', CHECK_PAYMENT: 'پرداخت چک',
  REFUND: 'بازگشت وجه', OTHER_RECEIPT: 'سایر دریافت', OTHER_PAYMENT: 'سایر پرداخت',
};
const typedSourceLabels = { HESABYAR: 'حسابیار', ERP_MANUAL: 'ثبت دستی', IMPORT: 'انتقال داده', SYSTEM: 'سیستم' };
const typedLedgerPartyRequired = new Set(['CUSTOMER_RECEIPT', 'SUPPLIER_PAYMENT', 'PAYROLL_PAYMENT', 'PETTY_CASH_FUNDING', 'PETTY_CASH_RETURN', 'CHECK_RECEIPT', 'CHECK_PAYMENT']);
const typedLedgerCounterpartyLabel = row => row.party_name || (row.transaction_type === 'INTERNAL_TRANSFER' ? '-' : typedLedgerPartyRequired.has(row.transaction_type) ? 'در انتظار تطبیق' : 'بدون طرف حساب اجباری');

function ProfessionalBankCashPage({ finance, setFinance, onGo }) {
  const { data } = useOperationalData();
  const [account, setAccount] = useState({ name: '', type: 'بانک', opening: 0 });
  const [movement, setMovement] = useState({ accountId: finance.accounts[0]?.id || '', counterAccountId: '', date: today(), direction: 'in', transactionType: 'customer_receipt', amount: '', payer: '', trackingNo: '', description: '' });
  const [filters, setFilters] = useState({ accountId: 'all', reconciled: 'all', fromDate: '', toDate: '', traceId: '' });
  const [pendingCounterparties, setPendingCounterparties] = useState({});
  const [typedLedger, setTypedLedger] = useState([]);
  const [typedLedgerError, setTypedLedgerError] = useState('');
  useEffect(() => {
    let cancelled = false;
    apiJSON('/v1/financial/transactions?limit=100')
      .then(payload => { if (!cancelled) setTypedLedger(payload.transactions || []); })
      .catch(error => { if (!cancelled) setTypedLedgerError(error.message || String(error)); });
    return () => { cancelled = true; };
  }, []);
  const customers = [...new Set([
    ...data.customers.map(x => x.name),
    ...finance.invoices.map(x => x.customer),
    ...finance.incomingInvoices.map(x => x.customer),
    ...(finance.yarnOutInvoices || []).map(x => x.customer),
    ...(finance.openingBalances || []).map(x => x.customer),
    ...finance.movements.flatMap(x => [confirmedMovementCounterparty(x), x.counterpartyCandidate, x.payer, x.customer]),
  ].filter(Boolean))];

  const addAccount = e => {
    e.preventDefault();
    if (!account.name.trim()) return;
    setFinance(prev => ({ ...prev, accounts: [{ id: uid('acc'), ...account, opening: Number(account.opening || 0) }, ...prev.accounts] }));
    setAccount({ name: '', type: 'بانک', opening: 0 });
  };

  const addMovement = e => {
    e.preventDefault();
    if (!movement.accountId || !Number(movement.amount || 0)) return;
    if (movementNeedsCounterparty(movement) && !movement.payer) { window.alert('انتخاب و تأیید طرف حساب برای ثبت گردش الزامی است.'); return; }
    if (movement.transactionType === 'transfer' && (!movement.counterAccountId || movement.counterAccountId === movement.accountId)) { window.alert('حساب مقصد انتقال باید انتخاب و با حساب مبدأ متفاوت باشد.'); return; }
    const base = { id: uid('mov'), ...movement, amount: Number(movement.amount), reconciled: false };
    const next = movementNeedsCounterparty(base)
      ? { ...confirmMovementCounterparty(base, movement.payer, 'bank_form'), counterpartyConfirmedAt: today() }
      : base;
    setFinance(prev => ({ ...prev, movements: [next, ...prev.movements] }));
    setMovement(prev => ({ ...prev, amount: '', payer: '', trackingNo: '', description: '', counterAccountId: '' }));
  };

  const confirmCounterparty = id => {
    const target = finance.movements.find(row => row.id === id);
    const selected = String(pendingCounterparties[id] || target?.counterpartyCandidate || '').trim();
    if (!selected) { window.alert('ابتدا طرف حساب را انتخاب کنید.'); return; }
    setFinance(prev => ({ ...prev, movements: prev.movements.map(row => row.id === id ? { ...confirmMovementCounterparty(row, selected), counterpartyConfirmedAt: today() } : row) }));
    setPendingCounterparties(prev => { const next = { ...prev }; delete next[id]; return next; });
  };
  const reopenCounterparty = id => setFinance(prev => ({ ...prev, movements: prev.movements.map(row => row.id === id ? { ...row, counterpartyCandidate: confirmedMovementCounterparty(row), payer: '', customer: '', counterpartyConfirmed: false, counterpartyConfirmedAt: '', reconciled: false, reconciledAt: '' } : row) }));
  const setReconciled = id => {
    const target = finance.movements.find(row => row.id === id);
    if (target && movementNeedsCounterparty(target) && !confirmedMovementCounterparty(target)) { window.alert('قبل از تطبیق، طرف حساب را انتخاب و تأیید کنید.'); return; }
    setFinance(prev => ({ ...prev, movements: prev.movements.map(row => row.id === id ? { ...row, reconciled: !row.reconciled, reconciledAt: !row.reconciled ? today() : '' } : row) }));
  };
  const expensesById = new Map((finance.expenses || []).map(row => [String(row.id), row]));
  const movementExpenseTrace = row => {
    const linkedExpense = row.sourceExpense ? expensesById.get(String(row.sourceExpense)) : null;
    return linkedExpense ? expenseTraceId(linkedExpense) : linkedExpenseTraceId(row);
  };
  const goToExpense = row => {
    const traceId = movementExpenseTrace(row);
    if (!traceId) return;
    try { localStorage.setItem('textile-expense-trace-filter', traceId); } catch {}
    if (onGo) onGo('costs');
  };
  const rows = finance.movements
    .map(row => ({ ...row, confirmedCounterparty: confirmedMovementCounterparty(row), accountName: finance.accounts.find(a => a.id === row.accountId)?.name || '-', counterAccountName: finance.accounts.find(a => a.id === row.counterAccountId)?.name || '-', expenseTraceId: movementExpenseTrace(row) }))
    .filter(row => (
      (filters.accountId === 'all' || row.accountId === filters.accountId)
      && (filters.reconciled === 'all' || String(Boolean(row.reconciled)) === filters.reconciled)
      && (!filters.traceId || String(row.expenseTraceId || '').includes(String(filters.traceId)) || String(row.sourceExpense || '').includes(String(filters.traceId)))
      && isDateWithinInclusiveRange(jalaliSortKey(row.date), jalaliSortKey(filters.fromDate), jalaliSortKey(filters.toDate))
    ));
  const typeLabels = { customer_receipt: 'دریافت از مشتری', supplier_payment: 'پرداخت به فروشنده', transfer: 'انتقال بین حساب‌ها', expense: 'پرداخت هزینه', other_income: 'سایر درآمد', capital: 'آورده/برداشت سرمایه' };
  const movementTypeLabel = row => typedTransactionLabels[row.typedType] || typeLabels[row.transactionType] || row.direction;
  const counterpartyText = row => movementCounterpartyLabel(row);
  const pendingCounterpartyValue = row => pendingCounterparties[row.id] || row.counterpartyCandidate || '';
  const changeType = transactionType => setMovement(prev => ({ ...prev, transactionType, direction: ['supplier_payment', 'expense'].includes(transactionType) ? 'out' : 'in', payer: '', counterAccountId: '' }));
  const printMovements = () => printSection('صورت گردش بانک و صندوق', `<p>بازه گزارش: ${filters.fromDate ? toJalali(filters.fromDate) : 'ابتدای دوره'} تا ${filters.toDate ? toJalali(filters.toDate) : 'انتهای دوره'}</p><table><thead><tr><th>تاریخ</th><th>حساب</th><th>ماهیت</th><th>طرف حساب</th><th>مبلغ</th><th>رهگیری</th><th>شناسه سند هزینه</th><th>تطبیق</th></tr></thead><tbody>${rows.map(row => `<tr><td>${toJalali(row.date)}</td><td>${row.accountName}</td><td>${typedTransactionLabels[row.typedType] || typeLabels[row.transactionType] || row.direction}</td><td>${counterpartyText(row)}</td><td>${money(row.amount)}</td><td>${row.trackingNo || '-'}</td><td>${row.expenseTraceId || '-'}</td><td>${row.reconciled ? 'تطبیق شد' : 'باز'}</td></tr>`).join('')}</tbody></table>`);

  return <div className="space-y-5">
    <div className="grid grid-cols-4 gap-4">{finance.accounts.map(a => <Field key={a.id} label={`${a.type}: ${a.name}`} value={`${money(accountBalance(a, finance.movements))} تومان`} tone={accountBalance(a, finance.movements) >= 0 ? 'text-emerald-300' : 'text-red-300'} />)}</div>
    <Card><h3 className="mb-4 font-bold">تعریف بانک یا صندوق</h3><form className="grid grid-cols-4 gap-3" onSubmit={addAccount}><TextInput placeholder="نام حساب" value={account.name} onChange={e => setAccount({ ...account, name: e.target.value })} /><SelectInput value={account.type} onChange={e => setAccount({ ...account, type: e.target.value })}><option>بانک</option><option>صندوق</option></SelectInput><TextInput type="number" placeholder="موجودی اولیه" value={account.opening} onChange={e => setAccount({ ...account, opening: e.target.value })} /><PrimaryButton type="submit">ثبت حساب</PrimaryButton></form></Card>
    <Card><h3 className="mb-4 font-bold">ثبت دریافت، پرداخت یا انتقال</h3><form className="grid grid-cols-4 gap-3" onSubmit={addMovement}>
      <SelectInput value={movement.transactionType} onChange={e => changeType(e.target.value)}><option value="customer_receipt">دریافت از مشتری</option><option value="supplier_payment">پرداخت به فروشنده</option><option value="transfer">انتقال بین حساب‌ها</option><option value="expense">پرداخت مستقیم هزینه</option><option value="other_income">سایر درآمد</option><option value="capital">آورده/برداشت سرمایه</option></SelectInput>
      <SelectInput value={movement.accountId} onChange={e => setMovement({ ...movement, accountId: e.target.value })}><option value="">حساب مبدأ/مقصد</option>{finance.accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput>
      {movement.transactionType === 'transfer' ? <SelectInput value={movement.counterAccountId} onChange={e => setMovement({ ...movement, counterAccountId: e.target.value })}><option value="">حساب مقصد انتقال</option>{finance.accounts.filter(a => a.id !== movement.accountId).map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput> : movementNeedsCounterparty(movement) ? <SelectInput value={movement.payer} onChange={e => setMovement({ ...movement, payer: e.target.value })}><option value="">طرف حساب (الزامی)</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput> : <div className="flex items-end"><span className="text-xs text-slate-400">این ماهیت طرف حساب ندارد</span></div>}
      <DateInput value={movement.date} onChange={e => setMovement({ ...movement, date: e.target.value })} />
      <TextInput type="number" min="1" placeholder="مبلغ" value={movement.amount} onChange={e => setMovement({ ...movement, amount: e.target.value })} />
      <TextInput placeholder="شماره رهگیری" value={movement.trackingNo} onChange={e => setMovement({ ...movement, trackingNo: e.target.value })} />
      <TextInput placeholder="شرح" value={movement.description} onChange={e => setMovement({ ...movement, description: e.target.value })} />
      <PrimaryButton type="submit">ثبت گردش</PrimaryButton>
    </form><div className="mt-3 rounded-md border border-blue-800 bg-blue-950 p-3 text-xs text-blue-100">فیلد طرف حساب فقط برای ماهیت‌های «دریافت از مشتری» و «پرداخت به فروشنده» الزامی است؛ هزینه مستقیم و سایر درآمدها طرف حساب ندارند. نام‌های دریافتی از حسابیار تا زمان تأیید کاربر در گزارش و مانده اشخاص اعمال نمی‌شوند.</div></Card>
    <Card><div className="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">گردش و مغایرت‌گیری بانک و صندوق</h3><div className="flex flex-wrap items-end gap-2"><SelectInput value={filters.accountId} onChange={e => setFilters({ ...filters, accountId: e.target.value })}><option value="all">همه حساب‌ها</option>{finance.accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</SelectInput><SelectInput value={filters.reconciled} onChange={e => setFilters({ ...filters, reconciled: e.target.value })}><option value="all">همه وضعیت‌ها</option><option value="false">تطبیق‌نشده</option><option value="true">تطبیق‌شده</option></SelectInput><TextInput placeholder="شناسه سند هزینه" value={filters.traceId} onChange={e => setFilters({ ...filters, traceId: e.target.value })} /><label className="text-xs text-slate-300"><span className="mb-1 block">از تاریخ</span><DateInput value={filters.fromDate} onChange={e => setFilters({ ...filters, fromDate: e.target.value })} /></label><label className="text-xs text-slate-300"><span className="mb-1 block">تا تاریخ</span><DateInput value={filters.toDate} onChange={e => setFilters({ ...filters, toDate: e.target.value })} /></label><PrimaryButton onClick={printMovements}>چاپ صورت حساب</PrimaryButton><PrimaryButton onClick={() => exportExcel('گردش بانک و صندوق', rows.map(row => ({ ...row, counterparty_text: counterpartyText(row), transaction_text: typedTransactionLabels[row.typedType] || typeLabels[row.transactionType] || row.direction, expense_trace: row.expenseTraceId || '', reconciled_text: row.reconciled ? 'تطبیق شد' : 'باز' })), [['date','تاریخ'],['accountName','حساب'],['transaction_text','ماهیت'],['counterparty_text','طرف حساب'],['amount','مبلغ'],['trackingNo','رهگیری'],['expense_trace','شناسه سند هزینه'],['reconciled_text','تطبیق']])}>خروجی اکسل</PrimaryButton></div></div>
      <div className="overflow-auto"><table className="w-full text-right text-sm"><thead><tr className="border-b border-slate-700 text-slate-300"><th className="p-3">تاریخ</th><th>حساب</th><th>ماهیت</th><th>طرف حساب</th><th>مبلغ</th><th>رهگیری</th><th>سند هزینه</th><th>وضعیت تطبیق</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{toJalali(row.date)}</td><td>{row.accountName}{row.transactionType === 'transfer' ? ` ← ${row.counterAccountName}` : ''}</td><td>{movementTypeLabel(row)}</td><td className="min-w-[280px]">{movementNeedsCounterparty(row) ? (row.confirmedCounterparty ? <div className="flex items-center gap-2"><span>{row.confirmedCounterparty}</span><GhostButton onClick={() => reopenCounterparty(row.id)}>اصلاح</GhostButton></div> : <div className="flex flex-wrap items-center gap-2"><span className="text-amber-300">{counterpartyText(row)}</span><SelectInput value={pendingCounterpartyValue(row)} onChange={e => setPendingCounterparties(prev => ({ ...prev, [row.id]: e.target.value }))}><option value="">انتخاب طرف حساب</option>{row.counterpartyCandidate && !customers.includes(row.counterpartyCandidate) && <option value={row.counterpartyCandidate}>{row.counterpartyCandidate}</option>}{customers.map(name => <option key={name} value={name}>{name}</option>)}</SelectInput><PrimaryButton disabled={!pendingCounterpartyValue(row)} onClick={() => confirmCounterparty(row.id)}>تأیید</PrimaryButton></div>) : <span className="rounded-full border border-slate-700 bg-slate-950 px-3 py-1 text-xs text-slate-300">{counterpartyText(row)}</span>}</td><td>{money(row.amount)}</td><td>{row.trackingNo || '-'}</td><td>{row.expenseTraceId ? <div className="flex flex-wrap items-center gap-2"><span className="rounded-full border border-blue-800 bg-blue-950 px-3 py-1 text-xs text-blue-100">{row.expenseTraceId}</span><GhostButton onClick={() => goToExpense(row)}>مشاهده هزینه</GhostButton></div> : '-'}</td><td><GhostButton onClick={() => setReconciled(row.id)} disabled={movementNeedsCounterparty(row) && !row.confirmedCounterparty}>{row.reconciled ? 'تطبیق شده' : 'علامت تطبیق'}</GhostButton></td></tr>)}</tbody></table></div>
    </Card>
    <Card><div className="mb-4 flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">دفتر مرکزی تراکنش‌های بانکی (ماهیت‌محور)</h3><span className="text-xs text-slate-400">دریافت‌شده از حسابیار و ثبت‌های سیستمی، بر اساس ماهیت حسابداری</span></div>
      {typedLedgerError ? <div className="rounded-md border border-amber-700 bg-amber-950 p-3 text-xs text-amber-100">دفتر مرکزی در دسترس نیست: {typedLedgerError}</div> : <div className="overflow-auto"><table className="w-full text-right text-sm"><thead><tr className="border-b border-slate-700 text-slate-300"><th className="p-3">تاریخ</th><th>حساب</th><th>ورودی/خروجی</th><th>ماهیت</th><th>مبلغ</th><th>طرف حساب</th><th>منبع</th><th>وضعیت</th></tr></thead><tbody>{typedLedger.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{toJalali(row.transaction_date)}</td><td>{row.bank_account_name || '-'}</td><td>{row.direction === 'IN' ? <span className="text-emerald-300">ورودی</span> : <span className="text-rose-300">خروجی</span>}</td><td>{typedTransactionLabels[row.transaction_type] || row.transaction_type}</td><td>{money(row.amount)}</td><td>{row.party_name ? row.party_name : typedLedgerPartyRequired.has(row.transaction_type) ? <span className="text-amber-300">{typedLedgerCounterpartyLabel(row)}</span> : typedLedgerCounterpartyLabel(row)}</td><td>{typedSourceLabels[row.source] || row.source}</td><td>{row.posting_status === 'NEEDS_REVIEW' ? <span className="text-amber-300">نیازمند بررسی</span> : <span className="text-slate-300">{row.status === 'VOIDED' ? 'ابطال‌شده' : 'ثبت‌شده'}</span>}</td></tr>)}</tbody></table></div>}
    </Card>
  </div>;
}

function AccountingPage({ finance, setFinance, revision }) {
  const [report, setReport] = useState({ trialBalance: [], partyBalances: [], vouchers: [], periods: [], summary: {} });
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [form, setForm] = useState({ date: today(), description: '', debitCode: '1110', creditCode: '3100', amount: '', party: '' });
  const [periodForm, setPeriodForm] = useState({ title: '', startDate: '', endDate: '' });
  const chart = [
    ['1110', 'صندوق', 'Asset'], ['1120', 'بانک', 'Asset'], ['1200', 'حساب‌های دریافتنی', 'Asset'], ['1210', 'اسناد دریافتنی', 'Asset'], ['1300', 'موجودی مواد و کالا', 'Asset'], ['1310', 'موجودی نخ', 'Asset'], ['1320', 'موجودی پارچه', 'Asset'], ['1330', 'موجودی قطعات', 'Asset'], ['1410', 'مالیات ارزش افزوده خرید', 'Asset'], ['2100', 'حساب‌های پرداختنی', 'Liability'], ['2110', 'اسناد پرداختنی', 'Liability'], ['2310', 'مالیات ارزش افزوده فروش', 'Liability'], ['3100', 'سرمایه و مانده افتتاحیه', 'Equity'], ['4200', 'درآمد فروش', 'Income'], ['4300', 'درآمد فروش نخ', 'Income'], ['4900', 'سایر درآمدها', 'Income'], ['5300', 'بهای تمام‌شده کالای فروش‌رفته', 'Expense'], ['5900', 'هزینه‌های عملیاتی', 'Expense'],
  ];
  const load = () => { setLoading(true); setError(''); apiJSON('/accounting/reports').then(setReport).catch(err => setError(err.message || 'دریافت دفاتر انجام نشد')).finally(() => setLoading(false)); };
  useEffect(load, [revision]);
  const account = code => chart.find(row => row[0] === code) || [code, code, 'Asset'];
  const addManual = e => {
    e.preventDefault();
    const amount = Number(form.amount || 0);
    if (!amount || form.debitCode === form.creditCode || !form.description.trim()) { window.alert('شرح، مبلغ و دو حساب متفاوت الزامی است.'); return; }
    const debit = account(form.debitCode), credit = account(form.creditCode);
    const entry = { id: uid('jv'), number: shortId('JV'), date: form.date, description: form.description, lines: [
      { accountCode: debit[0], accountName: debit[1], accountType: debit[2], party: form.party, debit: amount, credit: 0, description: form.description },
      { accountCode: credit[0], accountName: credit[1], accountType: credit[2], party: form.party, debit: 0, credit: amount, description: form.description },
    ] };
    setFinance(prev => ({ ...prev, journalEntries: [entry, ...(prev.journalEntries || [])] }));
    setForm(prev => ({ ...prev, description: '', amount: '', party: '' }));
  };
  const addPeriod = async e => {
    e.preventDefault();
    if (!periodForm.title.trim() || !periodForm.startDate || !periodForm.endDate) return;
    try { await apiJSON('/accounting/periods', { method: 'POST', body: periodForm }); setPeriodForm({ title: '', startDate: '', endDate: '' }); load(); } catch (err) { window.alert(err.message || 'ثبت دوره مالی انجام نشد'); }
  };
  const togglePeriod = async row => {
    const next = row.status === 'Closed' ? 'Open' : 'Closed';
    if (next === 'Closed' && !window.confirm('با بستن دوره، ثبت یا ویرایش سند در این بازه متوقف می‌شود. ادامه می‌دهید؟')) return;
    try { await apiJSON('/accounting/periods', { method: 'PUT', body: { id: Number(row.id), status: next } }); load(); } catch (err) { window.alert(err.message || 'تغییر وضعیت دوره انجام نشد'); }
  };
  const s = report.summary || {};
  const profit = Number(s.income || 0) - Number(s.expense || 0);
  const balanced = Math.abs(Number(s.total_debit || 0) - Number(s.total_credit || 0)) <= 1;
  const printTrial = () => printSection('تراز آزمایشی', `<table><thead><tr><th>کد</th><th>حساب</th><th>بدهکار</th><th>بستانکار</th><th>مانده</th></tr></thead><tbody>${(report.trialBalance || []).map(row => `<tr><td>${toPersianDigits(row.code)}</td><td>${row.name}</td><td>${money(row.debit)}</td><td>${money(row.credit)}</td><td>${money(row.balance)}</td></tr>`).join('')}</tbody></table>`);
  return <div className="space-y-5">
    {error && <ErrorBox message={error} />}
    <div className="grid grid-cols-5 gap-4"><Field label="جمع بدهکار" value={money(s.total_debit) + ' تومان'} /><Field label="جمع بستانکار" value={money(s.total_credit) + ' تومان'} /><Field label="سود/زیان دوره" value={money(profit) + ' تومان'} tone={profit >= 0 ? 'text-emerald-300' : 'text-red-300'} /><Field label="دارایی خالص" value={money(s.assets) + ' تومان'} /><Field label="کنترل تراز" value={balanced ? 'تراز است' : 'عدم تراز'} tone={balanced ? 'text-emerald-300' : 'text-red-300'} /></div>
    <Card><h3 className="mb-4 font-bold">ثبت سند دستی دوبل</h3><form className="grid grid-cols-6 gap-3" onSubmit={addManual}><DateInput value={form.date} onChange={e => setForm({ ...form, date: e.target.value })} /><SelectInput value={form.debitCode} onChange={e => setForm({ ...form, debitCode: e.target.value })}>{chart.map(row => <option key={row[0]} value={row[0]}>بدهکار: {toPersianDigits(row[0])} - {row[1]}</option>)}</SelectInput><SelectInput value={form.creditCode} onChange={e => setForm({ ...form, creditCode: e.target.value })}>{chart.map(row => <option key={row[0]} value={row[0]}>بستانکار: {toPersianDigits(row[0])} - {row[1]}</option>)}</SelectInput><TextInput type="number" min="1" placeholder="مبلغ" value={form.amount} onChange={e => setForm({ ...form, amount: e.target.value })} /><TextInput placeholder="طرف حساب (اختیاری)" value={form.party} onChange={e => setForm({ ...form, party: e.target.value })} /><PrimaryButton type="submit">ثبت سند</PrimaryButton><TextInput className="col-span-6" placeholder="شرح سند" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></form></Card>
    <Card><h3 className="mb-4 font-bold">دوره مالی و قفل ثبت</h3><form className="grid grid-cols-4 gap-3" onSubmit={addPeriod}><TextInput placeholder="عنوان دوره" value={periodForm.title} onChange={e => setPeriodForm({ ...periodForm, title: e.target.value })} /><DateInput value={periodForm.startDate} onChange={e => setPeriodForm({ ...periodForm, startDate: e.target.value })} /><DateInput value={periodForm.endDate} onChange={e => setPeriodForm({ ...periodForm, endDate: e.target.value })} /><PrimaryButton type="submit">ایجاد دوره</PrimaryButton></form><div className="mt-4 space-y-2">{(report.periods || []).map(row => <div key={row.id} className="flex items-center justify-between rounded-md border border-slate-700 bg-slate-950 p-3 text-sm"><span>{row.title} | {toJalali(row.start_date)} تا {toJalali(row.end_date)}</span><GhostButton onClick={() => togglePeriod(row)}>{row.status === 'Closed' ? 'بازکردن دوره' : 'بستن دوره'}</GhostButton></div>)}</div></Card>
    <Card><div className="mb-4 flex items-center justify-between"><h3 className="font-bold">تراز آزمایشی دفتر کل</h3><div className="flex gap-2"><PrimaryButton onClick={load}>{loading ? 'در حال دریافت...' : 'بازخوانی'}</PrimaryButton><PrimaryButton onClick={printTrial}>چاپ تراز</PrimaryButton><PrimaryButton onClick={() => exportExcel('تراز آزمایشی دفتر کل', (report.trialBalance || []).map(row => ({ code: row.code, account: row.name, type: row.type, debit: row.debit, credit: row.credit, balance: row.balance })))}>خروجی اکسل</PrimaryButton></div></div><GenericTable rows={(report.trialBalance || []).map(row => ({ code: row.code, account: row.name, type: row.type, debit: row.debit, credit: row.credit, balance: row.balance }))} /></Card>
    <Card><h3 className="mb-4 font-bold">مانده تفصیلی اشخاص</h3><GenericTable rows={(report.partyBalances || []).map(row => ({ party: row.party, debit: row.debit, credit: row.credit, balance: row.balance, nature: Number(row.balance) >= 0 ? 'بدهکار' : 'بستانکار' }))} /></Card>
    <Card><h3 className="mb-4 font-bold">دفتر روزنامه (حداکثر ۱۰۰۰ آرتیکل اخیر)</h3><GenericTable rows={(report.vouchers || []).map(row => ({ voucher: row.voucher_no, date: row.voucher_date, source: row.source_doc_type, description: row.description, account: `${row.account_code} - ${row.account_name}`, party: row.party || '-', debit: row.debit, credit: row.credit }))} /></Card>
    <Card><h3 className="mb-3 font-bold">تنظیم مالیات</h3><div className="flex items-center gap-3"><label className="text-sm text-slate-300">نرخ پیش‌فرض پیشنهادی (درصد)</label><TextInput type="number" min="0" max="100" value={finance.accountingSettings?.defaultVatRate ?? 0} onChange={e => setFinance(prev => ({ ...prev, accountingSettings: { ...(prev.accountingSettings || {}), defaultVatRate: Number(e.target.value || 0) } }))} /><span className="text-xs text-amber-200">مالیات فقط برای اسنادی محاسبه می‌شود که هنگام ثبت، مشمول علامت خورده باشند؛ نرخ قانونی را حسابدار دوره کنترل کند.</span></div></Card>
  </div>;
}

function ReportsPage({ finance, setFinance }) {

  const customers = [...new Set([...finance.invoices.map(x => x.customer), ...finance.incomingInvoices.map(x => x.customer), ...(finance.yarnOutInvoices || []).map(x => x.customer), ...finance.receivableDocs.map(x => x.customer), ...finance.receivableDocs.map(x => x.assignedTo), ...finance.payableDocs.map(x => x.customer), ...(finance.openingBalances || []).map(x => x.customer)].filter(Boolean))];

  const [customer, setCustomer] = useState('all');

  const [fromDate, setFromDate] = useState('');

  const [toDate, setToDate] = useState('');

  const [typeFilter, setTypeFilter] = useState('all');

  const legacyPaymentPlans = (() => { try { return JSON.parse(localStorage.getItem('textile-payment-plans') || '{}'); } catch { return {}; } })();

  const paymentPlans = { ...legacyPaymentPlans, ...(finance.paymentPlans || {}) };

  const activePlan = customer === 'all' ? { cashPercent: 30, checkPercent: 70, checkMonths: 3 } : (paymentPlans[customer] || { cashPercent: 30, checkPercent: 70, checkMonths: 3 });

  const savePlan = patch => {

    if (customer === 'all') return;

    const nextPlan = { ...activePlan, ...patch };

    setFinance(prev => ({ ...prev, paymentPlans: { ...(prev.paymentPlans || {}), [customer]: nextPlan } }));

    localStorage.setItem('textile-payment-plans', JSON.stringify({ ...paymentPlans, [customer]: nextPlan }));

  };

  const allReportRows = [

    ...(finance.openingBalances || []).map(row => ({ type: 'مانده افتتاحيه', invoice_no: row.id, date: row.date, customer: row.customer, item: row.description || '-', pricing_basis: row.type === 'receivable' ? 'طلب از مشتري' : 'بدهي به شخص', quantity: '', unit_price: '', total: row.type === 'receivable' ? Number(row.amount || 0) : -Number(row.amount || 0), paid: 0, debt: row.type === 'receivable' ? Number(row.amount || 0) : -Number(row.amount || 0) })),

    ...finance.invoices.map(row => { const plan = paymentPlans[row.customer] || row.paymentTerms || paymentPlanFor(row.customer); const debt = invoiceDebt(row); return { type: 'فاکتور خروج مالي', invoice_no: row.number, date: row.date, customer: row.customer, item: row.item, pricing_basis: row.basis === 'weight' ? 'وزني' : 'متري', quantity: row.quantity, unit_price: row.unitPrice, total: row.total, paid: paidAmount(row), debt, expected_cash: Math.round(debt * Number(plan.cashPercent || 0) / 100), expected_check: Math.round(debt * Number(plan.checkPercent || 0) / 100), expected_check_date: addMonths(row.date, plan.checkMonths) }; }),

    ...finance.incomingInvoices.map(row => ({ type: row.nonFinancial ? 'فاکتور ورود اماني' : 'فاکتور ورود', invoice_no: row.id, date: row.date, customer: row.customer, item: row.itemName, pricing_basis: row.inventoryType === 'yarn' ? 'نخ' : row.inventoryType === 'fabric' ? 'پارچه' : 'ساير', quantity: row.quantity, unit_price: row.unitPrice, total: row.nonFinancial ? 0 : row.amount, paid: row.nonFinancial ? 0 : (row.payments || []).filter(p => p.type !== 'credit').reduce((s, p) => s + Number(p.amount || 0), 0), debt: row.nonFinancial ? 0 : -(row.payments || []).filter(p => p.type === 'credit').reduce((s, p) => s + Number(p.amount || 0), 0), inventory_value: row.nonFinancial ? row.amount : '' })),

    ...finance.movements.map(row => ({ type: 'گردش بانک/صندوق', invoice_no: row.sourceIncomingInvoice || row.sourceInvoice || row.id, date: row.date, customer: confirmedMovementCounterparty(row) || (row.transactionType === 'transfer' ? '-' : 'تأیید نشده'), item: row.description || '-', pricing_basis: row.direction === 'in' ? 'واريز' : 'برداشت', quantity: '', unit_price: '', total: row.direction === 'in' ? Number(row.amount || 0) : -Number(row.amount || 0), paid: Number(row.amount || 0), debt: 0 })),

    ...finance.receivableDocs.map(row => ({ type: row.assignedTo ? 'واگذاري چک دريافتي' : 'چک دريافتي', invoice_no: row.checkNo || row.id, date: row.assignedAt || row.dueDate || '', customer: row.assignedTo || row.customer || '-', item: row.customer ? 'از ' + row.customer : '-', pricing_basis: statusLabel(row.status), quantity: '', unit_price: '', total: Number(row.amount || 0), paid: row.status === 'cleared' ? Number(row.amount || 0) : 0, debt: row.assignedTo ? Number(row.amount || 0) : 0 })),

    ...finance.payableDocs.map(row => ({ type: 'چک پرداختي', invoice_no: row.checkNo || row.id, date: row.dueDate || '', customer: row.customer || '-', item: row.bank || '-', pricing_basis: statusLabel(row.status), quantity: '', unit_price: '', total: -Number(row.amount || 0), paid: row.status === 'paid' ? Number(row.amount || 0) : 0, debt: -Number(row.amount || 0) })),

  ];

  const reportRows = allReportRows.filter(row => {

    const byCustomer = customer === 'all' || row.customer === customer;

    const byType = typeFilter === 'all' || row.type === typeFilter;

    const byFrom = !fromDate || !row.date || row.date >= fromDate;

    const byTo = !toDate || !row.date || row.date <= toDate;

    return byCustomer && byType && byFrom && byTo;

  });

  const summary = customer === 'all' ? null : customerFinance(finance, customer);

  const printReport = () => {

    const html = `<p>از تاريخ: ${toJalali(fromDate) || '-'} | تا تاريخ: ${toJalali(toDate) || '-'} | مشتري: ${customer === 'all' ? 'همه' : customer} | نوع: ${typeFilter === 'all' ? 'همه' : typeFilter}</p><table><thead><tr><th>نوع</th><th>شماره</th><th>تاريخ</th><th>شخص</th><th>شرح</th><th>مبنا/وضعيت</th><th>مقدار</th><th>نرخ واحد</th><th>مبلغ</th><th>پرداخت</th><th>مانده</th><th>نقد طبق قرار</th><th>چک طبق قرار</th><th>تاريخ چک</th></tr></thead><tbody>${reportRows.map(x => `<tr><td>${x.type}</td><td>${x.invoice_no}</td><td>${toJalali(x.date)}</td><td>${x.customer}</td><td>${x.item}</td><td>${x.pricing_basis}</td><td>${num(x.quantity)}</td><td>${money(x.unit_price)}</td><td>${money(x.total)}</td><td>${money(x.paid)}</td><td>${money(x.debt)}</td><td>${money(x.expected_cash)}</td><td>${money(x.expected_check)}</td><td>${toJalali(x.expected_check_date)}</td></tr>`).join('')}</tbody></table>`;

    printSection('ريز گزارش مالي فاکتورها', html);

  };

  return (

    <div className="space-y-5">

      <Card><div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">ريز گزارش مالي فاکتورها</h3><div className="flex flex-wrap gap-2"><SelectInput value={customer} onChange={e => setCustomer(e.target.value)}><option value="all">همه مشتريان</option>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><SelectInput value={typeFilter} onChange={e => setTypeFilter(e.target.value)}><option value="all">همه نوع ها</option>{[...new Set(allReportRows.map(x => x.type))].map(t => <option key={t} value={t}>{t}</option>)}</SelectInput><DateInput value={fromDate} onChange={e => setFromDate(e.target.value)} /><DateInput value={toDate} onChange={e => setToDate(e.target.value)} /><PrimaryButton onClick={printReport}>چاپ ريز گزارش</PrimaryButton><PrimaryButton onClick={() => exportExcel('ریز گزارش مالی', reportRows, [['type','نوع'],['invoice_no','شماره'],['date','تاریخ'],['customer','شخص'],['item','شرح'],['pricing_basis','مبنا یا وضعیت'],['quantity','مقدار'],['unit_price','نرخ واحد'],['total','مبلغ'],['paid','پرداخت'],['debt','مانده'],['expected_cash','نقد طبق قرار'],['expected_check','چک طبق قرار'],['expected_check_date','تاریخ چک طبق قرار']])}>خروجی اکسل</PrimaryButton></div></div></Card>

      {customer !== 'all' && <Card><div className="mb-4 flex items-center justify-between"><h3 className="font-bold">قرار پرداخت شخص</h3><span className="text-sm text-blue-200">{customer}</span></div><div className="grid grid-cols-3 gap-4"><label className="text-sm text-slate-300"><span className="mb-2 block">درصد پرداخت نقدي</span><TextInput className="w-full" type="number" min="0" max="100" value={activePlan.cashPercent} onChange={e => { const value = Math.min(100, Math.max(0, Number(e.target.value || 0))); savePlan({ cashPercent: value, checkPercent: 100 - value }); }} /></label><label className="text-sm text-slate-300"><span className="mb-2 block">درصد پرداخت چکي</span><TextInput className="w-full" type="number" min="0" max="100" value={activePlan.checkPercent} onChange={e => { const value = Math.min(100, Math.max(0, Number(e.target.value || 0))); savePlan({ checkPercent: value, cashPercent: 100 - value }); }} /></label><label className="text-sm text-slate-300"><span className="mb-2 block">سررسيد چک چند ماهه</span><TextInput className="w-full" type="number" min="0" value={activePlan.checkMonths} onChange={e => savePlan({ checkMonths: Math.max(0, Number(e.target.value || 0)) })} /></label></div><div className="mt-3 rounded-md border border-slate-700 bg-slate-900 p-3 text-xs text-slate-300">تنظیمات در فضای شرکت ذخیره می‌شود و مبلغ نقد، مبلغ چک و سررسید تمام فاکتورهای این شخص بلافاصله در جدول، چاپ و خروجی اکسل محاسبه می‌شود.</div></Card>}

      {summary && <div className="grid grid-cols-6 gap-4"><Field label="مانده خالص شخص" value={money(summary.netBalance) + ' تومان'} tone={summary.netBalance >= 0 ? 'text-amber-300' : 'text-emerald-300'} /><Field label="بدهی فاکتور خروج" value={money(summary.debt) + ' تومان'} tone="text-amber-300" /><Field label="بستانکاری فاکتور ورود" value={money(summary.payableToCustomer) + ' تومان'} tone="text-emerald-300" /><Field label="پرداختي خروجي" value={money(summary.paidOut) + ' تومان'} tone="text-blue-300" /><Field label="چک واگذار شده" value={money(summary.assignedChecks) + ' تومان'} tone="text-violet-300" /><Field label="جمع فاکتور ورود" value={money(summary.incomingTotal) + ' تومان'} /></div>}

      <Card><h3 className="mb-4 font-bold">جدول گزارش مالي</h3><GenericTable rows={reportRows} /></Card>

    </div>

  );

}



function TaxReportPage({ finance }) {

  const [fromDate, setFromDate] = useState('');

  const [toDate, setToDate] = useState('');

  const inRange = date => { const key = jalaliSortKey(date); const from = jalaliSortKey(fromDate); const to = jalaliSortKey(toDate); return (!from || !key || key >= from) && (!to || !key || key <= to); };

  const sales = finance.invoices.filter(x => inRange(x.date));

  const purchases = finance.incomingInvoices.filter(x => inRange(x.date));

  const expenses = finance.expenses.filter(x => inRange(x.date));

  const vatRate = Number(finance.accountingSettings?.defaultVatRate || 0) / 100;

  const salesTotal = sales.reduce((s, x) => s + Number(x.total || 0), 0);

  const purchasesTotal = purchases.reduce((s, x) => s + Number(x.amount || 0), 0);

  const expensesTotal = expenses.reduce((s, x) => s + Number(x.amount || 0), 0);

  const outputVat = sales.reduce((sum, row) => sum + Number(row.taxAmount || (row.taxable ? Math.round(Number(row.subtotal ?? row.total ?? 0) * vatRate) : 0)), 0);

  const inputVat = [...purchases, ...expenses].reduce((sum, row) => sum + Number(row.taxAmount || (row.taxable ? Math.round(Number(row.subtotal ?? row.amount ?? 0) * vatRate) : 0)), 0);

  const payableVat = outputVat - inputVat;

  const rows = [

    ...sales.map(x => ({ type: 'فروش/درآمد', invoice_no: x.number, date: x.date, party: x.customer, description: x.item, taxable_amount: x.taxable ? Number(x.subtotal ?? x.total ?? 0) : 0, vat: Number(x.taxAmount || 0), total: Number(x.total || 0) })),

    ...purchases.map(x => ({ type: 'خريد/ورود', invoice_no: x.id, date: x.date, party: x.customer, description: x.itemName, taxable_amount: x.taxable ? Number(x.subtotal ?? x.amount ?? 0) : 0, vat: Number(x.taxAmount || 0), total: Number(x.amount || 0) })),

    ...expenses.map(x => ({ type: 'هزينه', invoice_no: x.id, date: x.date, party: '-', description: `${expenseGroup(x)} / ${expenseSubgroup(x)}`, taxable_amount: x.taxable ? Number(x.subtotal ?? x.amount ?? 0) : 0, vat: Number(x.taxAmount || 0), total: Number(x.amount || 0) })),

  ].sort((a, b) => String(a.date).localeCompare(String(b.date)));

  const printTax = () => {

    const body = rows.map(x => '<tr><td>' + x.type + '</td><td>' + (x.invoice_no || '') + '</td><td>' + toJalali(x.date) + '</td><td>' + (x.party || '') + '</td><td>' + (x.description || '') + '</td><td>' + money(x.taxable_amount) + '</td><td>' + money(x.vat) + '</td><td>' + money(x.total) + '</td></tr>').join('');

    const html = '<p>دوره: ' + toJalali(fromDate) + ' تا ' + toJalali(toDate) + '</p><p>جمع فروش: ' + money(salesTotal) + ' | جمع خريد/ورود: ' + money(purchasesTotal) + ' | جمع هزينه: ' + money(expensesTotal) + ' | ماليات ارزش افزوده قابل پرداخت: ' + money(payableVat) + '</p><table><thead><tr><th>نوع</th><th>شماره</th><th>تاريخ</th><th>طرف حساب</th><th>شرح</th><th>مبلغ مشمول</th><th>ماليات/عوارض</th><th>جمع</th></tr></thead><tbody>' + body + '</tbody></table>';

    printSection('گزارش مالياتي قابل ارائه', html);

  };

  return <div className="space-y-5"><Card><div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-bold">گزارش مالیاتی ایران</h3><div className="flex flex-wrap gap-2"><DateInput value={fromDate} onChange={e => setFromDate(e.target.value)} /><DateInput value={toDate} onChange={e => setToDate(e.target.value)} /><PrimaryButton onClick={printTax}>چاپ گزارش مالیاتی</PrimaryButton><PrimaryButton onClick={() => exportExcel('گزارش مالیاتی', rows, [['type','نوع'],['invoice_no','شماره'],['date','تاریخ'],['party','طرف حساب'],['description','شرح'],['taxable_amount','مبلغ مشمول'],['vat','مالیات و عوارض'],['total','جمع']], { label: 'جمع', taxable_amount: salesTotal + purchasesTotal + expensesTotal, vat: payableVat })}>خروجی اکسل</PrimaryButton></div></div></Card><div className="grid grid-cols-4 gap-4"><Field label="جمع فروش/درآمد" value={money(salesTotal) + ' تومان'} tone="text-emerald-300" /><Field label="جمع خرید/ورود" value={money(purchasesTotal) + ' تومان'} tone="text-blue-300" /><Field label="جمع هزینه" value={money(expensesTotal) + ' تومان'} tone="text-red-300" /><Field label="مالیات ارزش افزوده قابل پرداخت" value={money(payableVat) + ' تومان'} tone={payableVat >= 0 ? 'text-amber-300' : 'text-emerald-300'} /></div><Card><h3 className="mb-4 font-bold">ریز اقلام مالیاتی</h3><GenericTable rows={rows} /></Card><Card><h3 className="mb-4 font-bold">کنترل حسابدار</h3><div className="text-sm leading-7 text-slate-300">نرخ پیشنهادی فعلی {num(vatRate * 100)}٪ است. فقط اسنادی که هنگام ثبت «مشمول مالیات» شده‌اند در اعتبار و بدهی مالیاتی محاسبه می‌شوند. این خروجی گزارش کنترلی است؛ ارسال رسمی سامانه مؤدیان نیازمند شناسه کالا/خدمت، شماره اقتصادی، الگوی صورتحساب و امضای الکترونیکی معتبر است.</div></Card></div>;

}



function CreditPage({ finance }) {

  const { data, loading, error } = useOperationalData();

  const customers = [...new Set([...data.invoices.map(x => x.mosh_f_khor), ...finance.invoices.map(x => x.customer), ...finance.incomingInvoices.map(x => x.customer), ...(finance.yarnOutInvoices || []).map(x => x.customer), ...finance.receivableDocs.map(x => x.customer), ...finance.receivableDocs.map(x => x.assignedTo), ...finance.payableDocs.map(x => x.customer), ...finance.movements.map(confirmedMovementCounterparty), ...(finance.openingBalances || []).map(x => x.customer)].filter(Boolean))];

  const [selected, setSelected] = useState('');

  const customer = selected || customers[0] || '';

  const invoices = data.invoices.filter(x => x.mosh_f_khor === customer);

  const financialInvoices = finance.invoices.filter(x => x.customer === customer);

  const yarnIn = data.yarnIn.filter(x => x.customer_name === customer).reduce((s, x) => s + Math.abs(Number(x.weight || 0)), 0);

  const yarnOut = data.yarnOut.filter(x => x.customer_name === customer).reduce((s, x) => s + Math.abs(Number(x.weight || 0)), 0);

  const financial = customerFinance(finance, customer);

  const yarnBalance = yarnIn - yarnOut;

  const futureChecks = finance.receivableDocs.filter(x => x.customer === customer && x.status !== 'cleared' && x.status !== 'assigned').reduce((s, x) => s + Number(x.amount || 0), 0);

  const todayDate = new Date(today());

  const lateChecks = finance.receivableDocs.filter(x => x.customer === customer && x.status !== 'cleared' && x.dueDate && new Date(x.dueDate) < todayDate);

  const maxDelay = lateChecks.reduce((max, x) => Math.max(max, Math.ceil((todayDate - new Date(x.dueDate)) / 86400000)), 0);

  const ownedYarn = finance.ownedInventory.filter(x => x.customer === customer && x.kindCode === 'yarn').reduce((s, x) => s + Number(x.amount || 0), 0);

  const ownedFabric = finance.ownedInventory.filter(x => x.customer === customer && x.kindCode === 'fabric').reduce((s, x) => s + Number(x.amount || 0), 0);

  const netExposure = financial.debt - futureChecks - ownedYarn - ownedFabric;

  const score = Math.max(0, Math.min(100, 60 + Math.min(financialInvoices.length * 5, 15) + (netExposure <= 0 ? 20 : netExposure < 500000000 ? 5 : -25) + (yarnBalance >= 0 ? 5 : -10) + (futureChecks > 0 ? 5 : 0) - Math.min(maxDelay, 30)));

  const level = score >= 75 ? 'اعتبار مناسب' : score >= 50 ? 'نيازمند کنترل' : 'پرريسک';

  const tone = score >= 75 ? 'text-emerald-300' : score >= 50 ? 'text-amber-300' : 'text-red-300';

  const reasons = [

    invoices.length > 0 ? `داراي ${num(invoices.length)} فاکتور خروج ثبت شده در عملياتي است.` : 'براي اين شخص فاکتور خروج ثبت نشده است.',

    yarnBalance >= 0 ? `مانده نخ محاسباتي مثبت يا صفر است: ${num(yarnBalance)} کيلو.` : `مانده نخ محاسباتي منفي است: ${num(yarnBalance)} کيلو.`,

    `مانده مالي مشتري ${money(financial.debt)} تومان است.`,

    `چک هاي دريافتني سررسيد نشده/باز مشتري ${money(futureChecks)} تومان است و در کاهش ريسک لحاظ شد.`,

    `ارزش نخ و پارچه مالکي تهاتري مشتري ${money(ownedYarn + ownedFabric)} تومان است.`,

    `مانده ريسک خالص پس از کسر چک و تهاتر ${money(netExposure)} تومان است.`,

    maxDelay > 0 ? `اين مشتري در پرداخت اسناد ${num(maxDelay)} روز تاخير دارد و از امتياز اعتبار او کسر شد.` : 'تاخير سررسيد شده ثبت نشده است.',

  ];

  const printCredit = () => printSection('گزارش اعتبارسنجي مشتري', `<p>مشتري: ${customer}</p><p>امتياز: ${num(score)} - ${level}</p><ul>${reasons.map(r => `<li>${r}</li>`).join('')}</ul>`);

  return (

    <div className="space-y-5">

      <Card><div className="flex items-center justify-between gap-3"><h3 className="font-bold">اعتبارسنجي مشتري بر اساس داده عملياتي و مالي</h3><div className="flex gap-2"><SelectInput value={customer} onChange={e => setSelected(e.target.value)}>{customers.map(c => <option key={c} value={c}>{c}</option>)}</SelectInput><PrimaryButton onClick={printCredit}>چاپ دلايل اعتبار</PrimaryButton><PrimaryButton onClick={() => exportExcel(`اعتبارسنجی ${customer}`, reasons.map((reason, index) => ({ row: index + 1, customer, score, level, reason })), [['row','ردیف'],['customer','مشتری'],['score','امتیاز'],['level','وضعیت'],['reason','دلیل']])}>خروجی اکسل</PrimaryButton></div></div></Card>

      {loading && <p className="text-sm text-slate-400">در حال دريافت...</p>}

      {error ? <ErrorBox message={error} /> : <><div className="grid grid-cols-4 gap-4"><Field label="امتياز اعتبار" value={num(score)} tone={tone} /><Field label="وضعيت" value={level} tone={tone} /><Field label="مانده ریسک خالص" value={`${money(netExposure)} تومان`} tone={netExposure > 0 ? 'text-amber-300' : 'text-emerald-300'} /><Field label="مانده نخ" value={`${num(yarnBalance)} کيلو`} tone={yarnBalance >= 0 ? 'text-emerald-300' : 'text-red-300'} /></div><Card><h3 className="mb-4 font-bold">دلايل اعتبار يا عدم اعتبار</h3><div className="space-y-2">{reasons.map((reason, index) => <div key={index} className="rounded-md border border-slate-700 bg-slate-900 p-3 text-sm text-slate-200">{reason}</div>)}</div></Card><Card><h3 className="mb-4 font-bold">فاکتورهاي موثر در اعتبار</h3><GenericTable rows={financialInvoices.map(x => ({ invoice_no: x.number, date: x.date, item: x.item, pricing_basis: x.basis === 'weight' ? 'وزني' : 'متري', quantity: x.quantity, unit_price: x.unitPrice, total: x.total, paid: paidAmount(x), debt: invoiceDebt(x) }))} /></Card></>}

    </div>

  );

}



function AdvisorPage({ finance }) {

  const { data } = useOperationalData();

  const [months, setMonths] = useState(3);

  const [aiBusy, setAiBusy] = useState(false);

  const [aiResult, setAiResult] = useState(null);

  const [aiError, setAiError] = useState('');

  const horizonMonths = Number(months || 1);

  const assignedDebt = finance.receivableDocs.filter(x => x.assignedTo).reduce((s, x) => s + Number(x.amount || 0), 0);

  const totalDebt = finance.invoices.reduce((s, x) => s + invoiceDebt(x), 0) + assignedDebt;

  const cashBalance = finance.accounts.reduce((s, a) => s + accountBalance(a, finance.movements), 0);

  const dueChecksRows = finance.receivableDocs.filter(x => x.status !== 'cleared' && x.status !== 'assigned' && sameMonth(x.dueDate, 0));

  const dueChecks = dueChecksRows.reduce((s, x) => s + Number(x.amount || 0), 0);

  const payablesRows = finance.payableDocs.filter(x => x.status !== 'paid');

  const payables = payablesRows.reduce((s, x) => s + Number(x.amount || 0), 0);

  const advisorGroups = [...new Set(finance.expenses.map(expenseGroup))];
  const expensesByTitle = advisorGroups.map(title => ({ title, amount: finance.expenses.filter(x => expenseGroup(x) === title).reduce((s, x) => s + Number(x.amount || 0), 0) })).filter(x => x.amount > 0).sort((a, b) => b.amount - a.amount);

  const forecastRows = advisorGroups.map(title => {

    const related = finance.expenses.filter(x => expenseGroup(x) === title && x.date);

    const byMonth = related.reduce((acc, row) => { const key = String(row.date).slice(0, 7); acc[key] = (acc[key] || 0) + Number(row.amount || 0); return acc; }, {});

    const values = Object.values(byMonth);

    const monthly = values.length ? values.reduce((sum, v) => sum + v, 0) / values.length : 0;

    return { title, monthly_average: Math.round(monthly), forecast_months: horizonMonths, forecast_amount: Math.round(monthly * horizonMonths) };

  }).filter(x => x.forecast_amount > 0).sort((a, b) => b.forecast_amount - a.forecast_amount);

  const forecastTotal = forecastRows.reduce((s, x) => s + x.forecast_amount, 0);

  const topExpense = expensesByTitle[0];

  const topForecast = forecastRows[0];

  const unassignedChecks = finance.receivableDocs.filter(x => x.status === 'open' && !x.assignedTo).reduce((s, x) => s + Number(x.amount || 0), 0);

  const horizonDate = new Date();

  horizonDate.setMonth(horizonDate.getMonth() + horizonMonths);

  const horizonPayables = payablesRows.filter(x => x.dueDate && new Date(x.dueDate) <= horizonDate).reduce((s, x) => s + Number(x.amount || 0), 0);

  const horizonReceivables = finance.receivableDocs.filter(x => x.status !== 'cleared' && x.status !== 'assigned' && x.dueDate && new Date(x.dueDate) <= horizonDate).reduce((s, x) => s + Number(x.amount || 0), 0);

  const liquidityGap = cashBalance + horizonReceivables - horizonPayables - forecastTotal;

  const totalRevenue = finance.invoices.reduce((sum, row) => sum + Number(row.total || 0), 0);

  const totalExpenses = finance.expenses.reduce((sum, row) => sum + Number(row.amount || 0), 0);

  const unpostedOperationalInvoices = Math.max(0, data.invoices.length - finance.invoices.length);

  const healthPenalties = [
    liquidityGap < 0 ? 30 : 0,
    totalRevenue > 0 && totalDebt > totalRevenue * 0.5 ? 20 : 0,
    cashBalance < payables ? 15 : 0,
    unpostedOperationalInvoices > 0 ? Math.min(15, unpostedOperationalInvoices * 3) : 0,
    finance.receivableDocs.length && dueChecks === 0 ? 0 : dueChecks > cashBalance ? 10 : 0,
  ];

  const healthScore = Math.max(0, Math.min(100, Math.round(100 - healthPenalties.reduce((sum, value) => sum + value, 0))));

  const healthLabel = healthScore >= 80 ? 'پایدار' : healthScore >= 60 ? 'نیازمند توجه' : 'نیازمند اقدام فوری';

  const healthTone = healthScore >= 80 ? 'text-emerald-300' : healthScore >= 60 ? 'text-amber-300' : 'text-red-300';

  const dataSignals = [
    finance.invoices.length > 0,
    finance.expenses.length > 0,
    finance.accounts.length > 0,
    finance.receivableDocs.length > 0,
    finance.payableDocs.length > 0,
    data.invoices.length > 0,
  ];

  const dataCompleteness = Math.round(dataSignals.filter(Boolean).length / dataSignals.length * 100);

  const priorities = [
    liquidityGap < 0 && {
      level: 'فوری',
      title: 'پوشش کسری نقدینگی',
      detail: 'شکاف پیش‌بینی‌شده ' + money(Math.abs(liquidityGap)) + ' تومان است.',
      action: 'وصول چک‌های نزدیک، تعویق پرداخت کم‌اهمیت و کاهش هزینه قابل‌کنترل را امروز بررسی کنید.',
    },
    unpostedOperationalInvoices > 0 && {
      level: 'مهم',
      title: 'تکمیل ثبت مالی تولید و فروش',
      detail: num(unpostedOperationalInvoices) + ' فاکتور عملیاتی هنوز اثر مالی کامل ندارد.',
      action: 'فاکتورها را با حسابداری تطبیق دهید تا سود و اعتبار مشتریان واقعی محاسبه شود.',
    },
    totalDebt > 0 && {
      level: totalRevenue > 0 && totalDebt > totalRevenue * 0.5 ? 'فوری' : 'مهم',
      title: 'کاهش مطالبات و ریسک مشتری',
      detail: 'مانده مشتریان و واگذاری‌ها ' + money(totalDebt) + ' تومان است.',
      action: 'سه مشتری با مانده بالاتر را برای تماس و برنامه وصول در اولویت قرار دهید.',
    },
    topForecast && {
      level: 'پیشنهادی',
      title: 'کنترل بودجه هزینه',
      detail: 'بیشترین هزینه پیش‌بینی‌شده مربوط به «' + topForecast.title + '» است.',
      action: 'برای این سرفصل سقف ماهانه و مسئول تأیید تعیین کنید.',
    },
  ].filter(Boolean).slice(0, 4);

  const dataGaps = [
    !finance.invoices.length && 'برای تحلیل فروش و سود، فاکتورهای مالی ثبت شوند.',
    !finance.expenses.length && 'برای پیش‌بینی نقدینگی، هزینه‌ها با تاریخ و سرفصل ثبت شوند.',
    !finance.accounts.length && 'برای سنجش نقدینگی، مانده بانک و صندوق تکمیل شود.',
    !finance.receivableDocs.length && 'برای برنامه وصول، اسناد دریافتنی و سررسید آن‌ها ثبت شود.',
    !data.invoices.length && 'برای تحلیل تولید، ارتباط داده‌های عملیاتی فعال و به‌روز شود.',
  ].filter(Boolean);

  const advices = [

    liquidityGap < 0 && 'با احتساب هزينه هاي پيش بيني شده، کسري نقدينگي احتمالي در افق ' + num(months) + ' ماهه ' + money(Math.abs(liquidityGap)) + ' تومان است. بايد از چک هاي وصولي نزديک، کاهش هزينه هاي قابل کنترل و مذاکره براي سررسيد اسناد پرداختي استفاده شود.',

    liquidityGap >= 0 && 'پس از کسر هزينه هاي پيش بيني شده، پوشش نقدينگي افق ' + num(months) + ' ماهه مثبت است و حدود ' + money(liquidityGap) + ' تومان حاشيه داريد.',

    forecastTotal > 0 && 'بر اساس سابقه هزينه هاي ثبت شده، حدود ' + money(forecastTotal) + ' تومان هزينه براي ' + num(months) + ' ماه آينده پيش بيني مي شود. اين مبلغ بايد قبل از تصميم گيري براي پرداخت هاي جديد کنار گذاشته شود.',

    topForecast && 'بزرگ ترين هزينه پيش بيني شده «' + topForecast.title + '» با ميانگين ماهانه ' + money(topForecast.monthly_average) + ' تومان است. براي اين سرفصل سقف بودجه و تاريخ تامين وجه تعريف کنيد.',

    dueChecks > 0 && 'در ماه جاري ' + money(dueChecks) + ' تومان چک دريافتي سررسيد مي شود؛ براي هر چک پيگيري وصول و وضعيت بانک ثبت شود.',

    payables > 0 && 'اسناد پرداختي باز ' + money(payables) + ' تومان است. پرداخت ها را بر اساس سررسيد، ريسک جريمه و اهميت تامين کننده اولويت بندي کنيد.',

    topExpense && 'بزرگ ترين سرفصل هزينه ثبت شده «' + topExpense.title + '» با مبلغ ' + money(topExpense.amount) + ' تومان است. سهم آن را نسبت به فروش و فاکتورهاي مالي بررسي کنيد.',

    unassignedChecks > 0 && money(unassignedChecks) + ' تومان چک دريافتي هنوز واگذار يا وصول نشده است. تعيين تکليف اين اسناد مي تواند فشار نقدينگي را کم کند.',

    assignedDebt > 0 && money(assignedDebt) + ' تومان چک دريافتني به اشخاص ثالث واگذار شده و در مانده بدهي آن اشخاص لحاظ شده است. گزارش اشخاص واگذارگيرنده را کنترل کنيد.',

    totalDebt > 0 && 'مانده بدهي مشتريان و واگذاري ها ' + money(totalDebt) + ' تومان است. مشتريان با مانده بالا و بدون چک معتبر بايد در وضعيت کنترل اعتبار قرار بگيرند.',

    data.invoices.length > finance.invoices.length && num(data.invoices.length - finance.invoices.length) + ' فاکتور عملياتي هنوز ثبت مالي نشده اند؛ تا ثبت نشوند سود، مانده و اعتبارسنجي کامل واقعي نيست.',

  ].filter(Boolean);

  const runAIAnalysis = async () => {

    setAiBusy(true);
    setAiError('');

    try {
      const response = await fetch(`${API_BASE}/intelligence/ai-advisor`, {
        method: 'POST',
        headers: { ...authHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({
          period_months: horizonMonths,
          health_score: healthScore,
          data_completeness: dataCompleteness,
          revenue: totalRevenue,
          expenses: totalExpenses,
          cash_balance: cashBalance,
          customer_debt: totalDebt,
          forecast_expenses: forecastTotal,
          forecast_liquidity_gap: liquidityGap,
          receivables_in_horizon: horizonReceivables,
          payables_in_horizon: horizonPayables,
          unposted_operational_invoices: unpostedOperationalInvoices,
          top_expenses: expensesByTitle.slice(0, 5).map(row => ({ name: row.title, value: row.amount })),
          priorities: priorities.slice(0, 5).map(row => ({ level: row.level, title: row.title, detail: row.detail })),
          data_gaps: dataGaps,
        }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.error || 'تحلیل AI در دسترس نیست.');
      setAiResult(payload);
    } catch (error) {
      setAiError(error.message || 'تحلیل AI در دسترس نیست.');
    } finally {
      setAiBusy(false);
    }

  };

  const printAdvisor = () => {

    const forecastBody = forecastRows.map(x => '<tr><td>' + x.title + '</td><td>' + money(x.monthly_average) + '</td><td>' + num(x.forecast_months) + '</td><td>' + money(x.forecast_amount) + '</td></tr>').join('');

    const forecastHtml = '<h2>پيش بيني هزينه ها</h2><table><thead><tr><th>عنوان</th><th>ميانگين ماهانه</th><th>افق ماه</th><th>مبلغ پيش بيني</th></tr></thead><tbody>' + forecastBody + '</tbody></table>';

    const html = '<p>امتیاز سلامت کسب‌وکار: ' + num(healthScore) + ' از ۱۰۰ (' + healthLabel + ')</p><p>کامل بودن داده‌ها: ' + num(dataCompleteness) + '٪</p><p>افق بررسي: ' + num(months) + ' ماه</p><p>شکاف نقدينگي پس از پيش بيني هزينه: ' + money(liquidityGap) + ' تومان</p><ul>' + advices.map(a => '<li>' + a + '</li>').join('') + '</ul>' + forecastHtml;

    printSection('گزارش تحلیل و مشاور هوشمند نساجی', html);

  };

  return (

    <div className="space-y-5">

      <Card><div className="flex flex-wrap items-start justify-between gap-4"><div><h2 className="text-xl font-extrabold text-white">تحلیل و مشاور هوشمند Textile ERP</h2><p className="mt-2 max-w-3xl text-sm leading-7 text-slate-300">فروش، مطالبات، نقدینگی، هزینه‌ها و داده‌های عملیاتی را کنار هم می‌گذارد و مهم‌ترین تصمیم‌های مدیریتی را به ترتیب اولویت نشان می‌دهد.</p><div className="mt-3 flex flex-wrap gap-2 text-xs"><span className="rounded-full border border-emerald-800 bg-emerald-950 px-3 py-1 text-emerald-200">محاسبه امن داخل سامانه</span><span className="rounded-full border border-blue-800 bg-blue-950 px-3 py-1 text-blue-200">بدون نمایش کلید یا اطلاعات فنی</span></div></div><div className="flex gap-2"><SelectInput value={months} onChange={e => setMonths(e.target.value)}><option value="1">۱ ماه</option><option value="2">۲ ماه</option><option value="3">۳ ماه</option><option value="6">۶ ماه</option></SelectInput><PrimaryButton onClick={printAdvisor}>چاپ گزارش مدیریتی</PrimaryButton><PrimaryButton onClick={() => exportExcel('گزارش مشاور هوشمند', [...priorities.map(item => ({ type: 'اقدام اولویت‌دار', title: item.title, level: item.level, detail: item.detail, action: item.action })), ...advices.map((advice, index) => ({ type: 'توصیه', title: `توصیه ${index + 1}`, level: '', detail: advice, action: '' }))])}>خروجی اکسل</PrimaryButton></div></div></Card>

      <Card>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h3 className="font-bold text-white">جمع‌بندی هوشمند مالی</h3>
            <p className="mt-2 max-w-3xl text-sm leading-7 text-slate-300">موتور داخلی Viora همیشه در دسترس است. اگر سرویس AI بیرونی فعال باشد فقط شاخص‌های تجمیعی برای تفسیر تکمیلی ارسال می‌شوند؛ کلید، رمز و سند خام از مرورگر خارج نمی‌شود.</p>
          </div>
          <PrimaryButton onClick={runAIAnalysis} disabled={aiBusy}>{aiBusy ? 'در حال تحلیل…' : 'اجرای تحلیل هوشمند'}</PrimaryButton>
        </div>
        {aiError && <div className="mt-4 rounded-lg border border-amber-800 bg-amber-950/50 p-4 text-sm leading-7 text-amber-100">{aiError}</div>}
        {aiResult?.narrative && <div className="mt-4 grid gap-4 xl:grid-cols-2">
          <div className="rounded-lg border border-blue-800 bg-blue-950/40 p-4"><h4 className="font-bold text-blue-100">خلاصه مدیر</h4><p className="mt-2 text-sm leading-7 text-slate-200">{aiResult.narrative.executive_summary}</p><p className="mt-3 text-sm font-bold leading-7 text-emerald-200">تمرکز پیشنهادی: {aiResult.narrative.recommended_focus}</p></div>
          <div className="grid gap-3"><div className="rounded-lg border border-emerald-800 bg-emerald-950/40 p-4"><h4 className="font-bold text-emerald-100">نقاط مهم</h4><ul className="mt-2 list-disc space-y-1 pr-5 text-sm leading-7 text-slate-200">{aiResult.narrative.highlights.map((item, index) => <li key={index}>{item}</li>)}</ul></div><div className="rounded-lg border border-red-800 bg-red-950/40 p-4"><h4 className="font-bold text-red-100">ریسک‌ها</h4><ul className="mt-2 list-disc space-y-1 pr-5 text-sm leading-7 text-slate-200">{aiResult.narrative.risks.map((item, index) => <li key={index}>{item}</li>)}</ul></div></div>
          <p className="text-xs text-slate-400 xl:col-span-2">حالت: {aiResult.mode === 'provider' ? 'AI بیرونی امن' : aiResult.mode === 'local-fallback' ? 'موتور داخلی جایگزین' : 'موتور داخلی Viora'} · مدل: {aiResult.model}{Number(aiResult.total_tokens || 0) > 0 ? ` · مصرف: ${num(aiResult.total_tokens)} توکن` : ' · بدون مصرف توکن بیرونی'} · شناسه: {aiResult.run_id}</p>
        </div>}
      </Card>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4"><Field label="امتیاز سلامت کسب‌وکار" value={num(healthScore) + ' از ۱۰۰ — ' + healthLabel} tone={healthTone} /><Field label="کامل بودن داده‌ها" value={num(dataCompleteness) + '٪'} tone={dataCompleteness >= 80 ? 'text-emerald-300' : 'text-amber-300'} /><Field label="شکاف نقدینگی با پیش‌بینی هزینه" value={money(liquidityGap) + ' تومان'} tone={liquidityGap >= 0 ? 'text-emerald-300' : 'text-red-300'} /><Field label="مانده مشتریان و واگذاری‌ها" value={money(totalDebt) + ' تومان'} tone="text-amber-300" /></div>

      <Card><div className="mb-4 flex flex-wrap items-center justify-between gap-2"><h3 className="font-bold">اقدام‌های اولویت‌دار مدیر</h3><span className="text-xs text-slate-400">از مهم‌ترین مورد به کم‌ریسک‌ترین مورد</span></div><div className="grid gap-3 md:grid-cols-2">{priorities.length ? priorities.map((item, index) => <div key={`${item.title}-${index}`} className={`rounded-lg border p-4 ${item.level === 'فوری' ? 'border-red-800 bg-red-950/60' : item.level === 'مهم' ? 'border-amber-800 bg-amber-950/50' : 'border-slate-700 bg-slate-900'}`}><div className="flex items-center justify-between gap-2"><strong className="text-slate-100">{item.title}</strong><span className="rounded-full border border-current px-2 py-1 text-xs text-slate-300">{item.level}</span></div><p className="mt-2 text-sm leading-7 text-slate-300">{item.detail}</p><p className="mt-2 text-sm font-bold leading-7 text-blue-200">{item.action}</p></div>) : <div className="rounded-lg border border-emerald-800 bg-emerald-950/50 p-4 text-sm text-emerald-100">مورد فوری پیدا نشد؛ ثبت منظم داده‌ها و مرور هفتگی گزارش ادامه پیدا کند.</div>}</div></Card>

      <Card><h3 className="mb-4 font-bold">تحلیل تفصیلی و پیشنهادهای مدیریتی</h3><div className="space-y-3">{advices.length ? advices.map((a, i) => <div key={i} className="rounded-md border border-slate-700 bg-slate-900 p-4 text-sm leading-7 text-slate-100">{a}</div>) : <div className="rounded-md border border-emerald-800 bg-emerald-950 p-4 text-sm text-emerald-100">وضعيت فعلي نگران کننده نيست. ثبت مالي منظم را ادامه بدهيد.</div>}</div></Card>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-2"><Card><h3 className="mb-4 font-bold">پیش‌بینی هزینه ماه‌های آینده</h3><GenericTable rows={forecastRows} /></Card><Card><h3 className="mb-4 font-bold">چک‌های قابل وصول این ماه</h3><DocsMiniTable rows={dueChecksRows} /></Card></div>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-2"><Card><h3 className="mb-4 font-bold">تحلیل هزینه‌های ثبت‌شده</h3><GenericTable rows={expensesByTitle.map(x => ({ title: x.title, amount: x.amount }))} /></Card><Card><h3 className="mb-3 font-bold">کیفیت داده و محدودیت تحلیل</h3>{dataGaps.length ? <ul className="space-y-2 text-sm leading-7 text-amber-100">{dataGaps.map((item, index) => <li key={index} className="rounded-md border border-amber-900 bg-amber-950/40 p-3">{item}</li>)}</ul> : <p className="rounded-md border border-emerald-800 bg-emerald-950/50 p-4 text-sm leading-7 text-emerald-100">داده‌های پایه موردنیاز این گزارش ثبت شده‌اند.</p>}<p className="mt-4 text-xs leading-6 text-slate-400">این امتیازها ابزار تصمیم‌یار هستند و جای تأیید مدیر یا حسابدار را نمی‌گیرند. موتور داخلی بدون وابستگی بیرونی کار می‌کند و سرویس مولد فقط در صورت تنظیم امن کلید سرور استفاده می‌شود.</p></Card></div>

    </div>

  );

}



function ExpensesTable({ rows, onEdit, onDelete, onImport, highlightTrace = '' }) {

  if (!rows.length) return <EmptyState />;

  return (

    <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">منبع</th><th className="p-3 text-right">تاریخ</th><th className="p-3 text-right">شناسه سند</th><th className="p-3 text-right">گروه</th><th className="p-3 text-right">زیرگروه</th><th className="p-3 text-right">مبلغ</th><th className="p-3 text-right">توضیحات</th><th className="p-3 text-right">عملیات</th></tr></thead><tbody>{rows.map(row => { const traceId = expenseTraceId(row); const isHighlighted = highlightTrace && matchesExpenseTrace(row, highlightTrace); return <tr key={`${row.source}-${row.id}`} className={`border-b border-slate-800 ${isHighlighted ? 'bg-blue-950/50' : ''}`}><td className="p-3">{row.source}</td><td className="p-3 whitespace-nowrap">{toJalali(row.date)}</td><td className="p-3"><span className="rounded-full border border-slate-700 bg-slate-950 px-3 py-1 text-xs text-blue-100">{traceId || '-'}</span></td><td className="p-3 font-bold text-blue-200">{expenseGroup(row)}</td><td className="p-3">{expenseSubgroup(row)}</td><td className="p-3 font-bold text-red-200">{money(row.amount)}</td><td className="p-3 text-slate-400">{row.description || '-'}</td><td className="p-3">{row.financialRecord ? <div className="flex gap-2"><GhostButton onClick={() => onEdit(row)}>ویرایش</GhostButton><DangerButton onClick={() => onDelete(row.id)}>حذف</DangerButton></div> : <PrimaryButton onClick={() => onImport(row)}>ثبت در مالی</PrimaryButton>}</td></tr>; })}</tbody></table></div>

  );

}



function IncomingInvoiceTable({ rows, onEdit, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">تاريخ</th><th className="p-3 text-right">شخص</th><th className="p-3 text-right">کالا/قطعه</th><th className="p-3 text-right">مقدار</th><th className="p-3 text-right">نرخ</th><th className="p-3 text-right">مبلغ</th><th className="p-3 text-right">منبع</th><th className="p-3 text-right">عمليات</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{toJalali(row.date)}</td><td className="p-3">{row.customer}</td><td className="p-3">{row.itemName}</td><td className="p-3">{num(row.quantity)}</td><td className="p-3">{money(row.unitPrice)}</td><td className="p-3 font-bold text-emerald-200">{money(row.amount)}</td><td className="p-3">{row.source_type === 'operational_misc' ? 'ورودي عملياتي' : 'ثبت مالي'}</td><td className="p-3"><div className="flex gap-2"><GhostButton onClick={() => onEdit(row)}>ويرايش</GhostButton><DangerButton onClick={() => onDelete(row.id)}>حذف</DangerButton></div></td></tr>)}</tbody></table></div>;

}



function DocsTable({ rows, isReceivable, onStatus, onEdit, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">شخص</th>{isReceivable ? <th className="p-3 text-right">واگذار شده به</th> : <th className="p-3 text-right">شناسه صیادی</th>}<th className="p-3 text-right">مبلغ</th><th className="p-3 text-right">شماره</th><th className="p-3 text-right">سررسيد شمسي</th><th className="p-3 text-right">بانک</th><th className="p-3 text-right">وضعيت</th><th className="p-3 text-right">عمليات</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{row.customer}</td><td className="p-3">{isReceivable ? (row.assignedTo || row.previousAssignedTo || '-') : (row.sayadId || 'ثبت نشده')}</td><td className="p-3">{money(row.amount)}</td><td className="p-3">{row.checkNo}</td><td className="p-3">{row.dueJalali || toJalali(row.dueDate)}</td><td className="p-3">{row.bank}</td><td className="p-3"><div>{statusLabel(row.status)}</div>{row.operationReason && <small className="text-slate-400">{row.operationReason}</small>}</td><td className="p-3"><div className="flex flex-wrap gap-2"><GhostButton onClick={() => onEdit(row)}>ویرایش</GhostButton>{row.status === 'assigned' ? <span className="rounded-md border border-violet-700 px-3 py-2 text-xs text-violet-200">واگذار شد</span> : !['cleared', 'paid'].includes(row.status) && <GhostButton onClick={() => onStatus(row.id, isReceivable ? 'cleared' : 'paid')}>{isReceivable ? 'وصول شد' : 'پرداخت شد'}</GhostButton>}{row.status !== 'returned' && <GhostButton onClick={() => onStatus(row.id, 'returned')}>مرجوع</GhostButton>}{row.status !== 'bounced' && <DangerButton onClick={() => onStatus(row.id, 'bounced')}>برگشت</DangerButton>}<DangerButton onClick={() => onDelete(row.id)}>حذف</DangerButton></div></td></tr>)}</tbody></table></div>;

}



function DocsMiniTable({ rows }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-2 text-right">شخص</th><th className="p-2 text-right">مبلغ</th><th className="p-2 text-right">شماره</th><th className="p-2 text-right">سررسيد</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-2">{row.customer}</td><td className="p-2">{money(row.amount)}</td><td className="p-2">{row.checkNo}</td><td className="p-2">{row.dueJalali || toJalali(row.dueDate)}</td></tr>)}</tbody></table></div>;

}



function OpeningBalanceTable({ rows, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">تاريخ</th><th className="p-3 text-right">شخص</th><th className="p-3 text-right">نوع</th><th className="p-3 text-right">مبلغ</th><th className="p-3 text-right">شرح</th><th className="p-3 text-right">عمليات</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{toJalali(row.date)}</td><td className="p-3">{row.customer}</td><td className="p-3">{row.type === 'receivable' ? 'طلب از مشتري' : 'بدهي به شخص'}</td><td className={`p-3 font-bold ${row.type === 'receivable' ? 'text-emerald-200' : 'text-red-200'}`}>{money(row.amount)}</td><td className="p-3 text-slate-400">{row.description || '-'}</td><td className="p-3"><DangerButton onClick={() => onDelete(row.id)}>حذف</DangerButton></td></tr>)}</tbody></table></div>;

}



function OpeningDocsTable({ rows, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">نوع</th><th className="p-3 text-right">شخص</th><th className="p-3 text-right">شماره</th><th className="p-3 text-right">بانک</th><th className="p-3 text-right">سررسيد</th><th className="p-3 text-right">مبلغ</th><th className="p-3 text-right">وضعيت</th><th className="p-3 text-right">عمليات</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{String(row.id).startsWith('rch') ? 'دريافتي' : 'پرداختي'}</td><td className="p-3">{row.customer}</td><td className="p-3">{row.checkNo}</td><td className="p-3">{row.bank || '-'}</td><td className="p-3">{row.dueJalali || toJalali(row.dueDate)}</td><td className="p-3 font-bold text-blue-200">{money(row.amount)}</td><td className="p-3">{statusLabel(row.status)}</td><td className="p-3"><DangerButton onClick={() => onDelete(row)}>حذف</DangerButton></td></tr>)}</tbody></table></div>;

}



function OpeningInventoryTable({ rows, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">تاريخ</th><th className="p-3 text-right">نوع</th><th className="p-3 text-right">نام</th><th className="p-3 text-right">مقدار</th><th className="p-3 text-right">نرخ</th><th className="p-3 text-right">مبلغ</th><th className="p-3 text-right">مالک/طرف حساب</th><th className="p-3 text-right">عمليات</th></tr></thead><tbody>{rows.map(row => <tr key={row.id} className="border-b border-slate-800"><td className="p-3">{toJalali(row.date)}</td><td className="p-3">{row.kind}</td><td className="p-3">{row.itemName}</td><td className="p-3">{num(row.quantity)}</td><td className="p-3">{money(row.unitPrice)}</td><td className="p-3 font-bold text-amber-200">{money(row.amount)}</td><td className="p-3">{row.customer || '-'}</td><td className="p-3"><DangerButton onClick={() => onDelete(row.id)}>حذف</DangerButton></td></tr>)}</tbody></table></div>;

}



function FinancialInvoiceTable({ rows, onEdit, onPrint, onDelete }) {

  if (!rows.length) return <EmptyState />;

  return (

    <div className="overflow-auto">

      <table className="w-full border-collapse text-sm">

        <thead>

          <tr className="border-b border-slate-700 text-slate-400">

            <th className="p-3 text-right">شماره</th>

            <th className="p-3 text-right">مشتري</th>

            <th className="p-3 text-right">کالا</th>

            <th className="p-3 text-right">مبلغ</th>

            <th className="p-3 text-right">تسويه شده</th>

            <th className="p-3 text-right">نسيه/مانده</th>

            <th className="p-3 text-right">روش‌ها</th>

            <th className="p-3 text-right">عمليات</th>

          </tr>

        </thead>

        <tbody>

          {rows.map(row => (

            <tr key={row.id} className="border-b border-slate-800">

              <td className="p-3 font-bold text-blue-200">{row.number}</td>

              <td className="p-3">{row.customer}</td>

              <td className="p-3">{row.item}</td>

              <td className="p-3">{money(row.total)}</td>

              <td className="p-3 text-emerald-200">{money(paidAmount(row))}</td>

              <td className="p-3 text-amber-200">{money(invoiceDebt(row))}</td>

              <td className="p-3 text-slate-300">{(row.payments || []).map(p => paymentTypes.find(t => t.id === p.type)?.label || p.type).join('، ')}</td>

              <td className="p-3"><div className="flex flex-wrap gap-2"><GhostButton onClick={() => onEdit(row)}>ويرايش</GhostButton><GhostButton onClick={() => onPrint(row)}>چاپ</GhostButton><DangerButton onClick={() => onDelete(row)}>حذف</DangerButton></div></td>

            </tr>

          ))}

        </tbody>

      </table>

    </div>

  );

}



function InvoiceTable({ rows }) {

  if (!rows.length) return <EmptyState />;

  return <div className="overflow-auto"><table className="w-full border-collapse text-sm"><thead><tr className="border-b border-slate-700 text-slate-400"><th className="p-3 text-right">شماره</th><th className="p-3 text-right">تاريخ</th><th className="p-3 text-right">مشتري</th><th className="p-3 text-right">کالا</th><th className="p-3 text-right">طاقه</th><th className="p-3 text-right">متر</th><th className="p-3 text-right">وزن</th></tr></thead><tbody>{rows.map(row => <tr key={`${row.id_f_khor}-${row.shom_f_khor}`} className="border-b border-slate-800"><td className="p-3 font-bold text-blue-200">{row.shom_f_khor}</td><td className="p-3">{row.tarikh_f_khor}</td><td className="p-3">{row.mosh_f_khor}</td><td className="p-3">{row.kala_name}</td><td className="p-3">{num(row.piece_count)}</td><td className="p-3">{num(row.metr_salon)}</td><td className="p-3">{num(row.w_salon)}</td></tr>)}</tbody></table></div>;

}



function GenericTable({ rows }) {

  if (!rows.length) return <EmptyState />;

  const cols = Object.keys(rows[0]);

  return (

    <div className="overflow-auto">

      <table className="w-full border-collapse text-sm">

        <thead><tr className="border-b border-slate-700 text-slate-400">{cols.map(col => <th key={col} className="p-3 text-right">{columnLabels[col] || col}</th>)}</tr></thead>

        <tbody>

          {rows.map((row, index) => (

            <tr key={index} className="border-b border-slate-800">

              {cols.map(col => <td key={col} className="p-3">{col === 'status' ? statusLabel(row[col]) : col.toLowerCase().includes('date') || col === 'تاريخ' ? toJalali(row[col]) : formatTableValue(col, row[col])}</td>)}

            </tr>

          ))}

        </tbody>

      </table>

    </div>

  );

}



function EmptyState() {

  return <p className="rounded-md bg-slate-900 p-4 text-center text-sm text-slate-400">داده اي براي نمايش وجود ندارد.</p>;

}



function ErrorBox({ message }) {

  return <div className="rounded-md border border-red-800 bg-red-950 p-3 text-sm text-red-200">{message}</div>;

}














