import { toLatinDigits } from './persianDateInput.js';
const number = value => Number.isFinite(Number(value)) ? Number(value) : 0;
const normalizedName = value => toLatinDigits(value || '').normalize('NFKC').replace(/ي/g, 'ی').replace(/ك/g, 'ک').replace(/[\u200c\s]+/g, ' ').trim().toLocaleLowerCase('fa');
function priceDate(value) {
  const raw = toLatinDigits(value || '').trim();
  const jalali = raw.match(/^(1[34]\d{2})[/-](\d{1,2})[/-](\d{1,2})$/);
  if (jalali) return Number(jalali[2]) >= 1 && Number(jalali[2]) <= 12 && Number(jalali[3]) >= 1 && Number(jalali[3]) <= 31 ? `${jalali[1]}/${jalali[2].padStart(2, '0')}/${jalali[3].padStart(2, '0')}` : '';
  if (!/^20\d{2}-\d{2}-\d{2}$/.test(raw)) return '';
  const date = new Date(raw + 'T12:00:00Z');
  if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== raw) return '';
  const parts = new Intl.DateTimeFormat('en-US-u-ca-persian', { year: 'numeric', month: '2-digit', day: '2-digit', timeZone: 'UTC' }).formatToParts(date);
  const part = key => parts.find(x => x.type === key)?.value;
  return `${part('year')}/${part('month')}/${part('day')}`;
}
const text = value => String(value ?? '').trim();
const near = (left, right) => Math.abs(number(left) - number(right)) <= Math.max(1, Math.max(Math.abs(number(left)), Math.abs(number(right))) * 0.000001);

function rows(value) {
  return Array.isArray(value) ? value : [];
}

function sourceKey(row) {
  const type = text(row?.source_type);
  const id = text(row?.sourceId);
  return type && id ? `${type}:${id}` : '';
}

function cashPayments(invoice) {
  return rows(invoice?.payments).filter(payment => text(payment.type) === 'cash' && number(payment.amount) > 0);
}

function addFinding(findings, severity, category, title, detail, page, reference = '') {
  findings.push({
    id: `${category}:${reference || title}:${findings.length}`,
    severity,
    category,
    title,
    detail,
    page,
    reference,
  });
}

function compareCashLinks(findings, invoice, movements, sourceField, label, page) {
  const invoiceRef = text(invoice.number || invoice.id);
  const expected = cashPayments(invoice);
  const linked = movements.filter(row => text(row[sourceField]) === invoiceRef);
  const unmatched = [...linked];
  for (const payment of expected) {
    const index = unmatched.findIndex(row => near(row.amount, payment.amount) && text(row.accountId) === text(payment.accountId));
    if (index >= 0) unmatched.splice(index, 1);
    else addFinding(findings, 'critical', 'اثر بانک و صندوق', `${label} در بانک/صندوق اعمال نشده است`, `پرداخت نقدی ${number(payment.amount).toLocaleString('fa-IR')} تومان از حساب انتخاب‌شده، گردش متناظر ندارد.`, page, invoiceRef);
  }
  if (unmatched.length) {
    addFinding(findings, 'critical', 'ثبت تکراری', `${label} گردش نقدی اضافه دارد`, `${unmatched.length.toLocaleString('fa-IR')} گردش بانکی بیش از ردیف‌های تسویه به این سند متصل است.`, page, invoiceRef);
  }
}

export function suggestIncomingPrice(finance, draft, limit = 5) {
  const currentId = text(draft?.id);
  const item = normalizedName(draft?.itemName);
  const customer = normalizedName(draft?.customer);
  const asOf = priceDate(draft?.date || new Date().toISOString().slice(0, 10));
  if (!item) return { price: 0, samples: [], confidence: 'none', basis: 'سابقه‌ای پیدا نشد' };
  const samples = rows(finance?.incomingInvoices)
    .filter(row => text(row.id) !== currentId && !row.nonFinancial && normalizedName(row.itemName) === item
      && (row.inventoryType || 'yarn') === (draft?.inventoryType || 'yarn')
      && (row.unit || '') === (draft?.unit || '')
      && (row.currency || 'IRT') === (draft?.currency || 'IRT')
      && (row.source_type === 'operational_chelle_in') === (draft?.source_type === 'operational_chelle_in')
      && asOf && priceDate(row.date) && priceDate(row.date) <= asOf)
    .map(row => ({
      id: text(row.id),
      date: text(row.date),
      dateKey: priceDate(row.date),
      customer: text(row.customer),
      price: number(row.unitPrice) || (number(row.quantity) > 0 ? number(row.subtotal || (number(row.amount) - number(row.taxAmount))) / number(row.quantity) : 0),
    }))
    .filter(row => row.price > 0)
    .sort((left, right) => right.dateKey.localeCompare(left.dateKey) || left.id.localeCompare(right.id));
  const exact = samples.filter(row => normalizedName(row.customer) === customer);
  const selected = (exact.length ? exact : samples).slice(0, limit);
  if (!selected.length) return { price: 0, samples: [], confidence: 'none', basis: 'سابقه‌ای برای این کالا پیدا نشد' };
  const ordered = selected.map(row => row.price).sort((a, b) => a - b);
  const middle = Math.floor(ordered.length / 2);
  const median = ordered.length % 2 ? ordered[middle] : (ordered[middle - 1] + ordered[middle]) / 2;
  return {
    price: Math.round(median),
    samples: selected,
    confidence: exact.length >= 2 ? 'high' : exact.length === 1 ? 'medium' : 'low',
    basis: exact.length ? `${selected.length.toLocaleString('fa-IR')} فاکتور قبلی همین فروشنده و کالا` : `${selected.length.toLocaleString('fa-IR')} فاکتور قبلی همین کالا از فروشندگان دیگر`,
    min: Math.min(...ordered),
    max: Math.max(...ordered),
  };
}

export function incomingInvoiceImpact(invoice, accounts = []) {
  const accountNames = new Map(rows(accounts).map(account => [text(account.id), text(account.name || account.id)]));
  const payments = rows(invoice?.payments);
  const cash = payments.filter(row => text(row.type) === 'cash').reduce((sum, row) => sum + number(row.amount), 0);
  const checks = payments.filter(row => text(row.type) === 'check').reduce((sum, row) => sum + number(row.amount), 0);
  const assigned = payments.filter(row => text(row.type) === 'assign_receivable').reduce((sum, row) => sum + number(row.amount), 0);
  const barter = payments.filter(row => ['barter_yarn', 'barter_fabric'].includes(text(row.type))).reduce((sum, row) => sum + number(row.amount), 0);
  const credit = payments.filter(row => text(row.type) === 'credit').reduce((sum, row) => sum + number(row.amount), 0);
  const accountEffects = payments
    .filter(row => text(row.type) === 'cash' && number(row.amount) > 0)
    .map(row => ({ account: accountNames.get(text(row.accountId)) || 'حساب انتخاب‌نشده', amount: -number(row.amount) }));
  return {
    inventoryQuantity: number(invoice?.quantity),
    inventoryValue: number(invoice?.subtotal || invoice?.amount),
    cash,
    checks,
    assigned,
    barter,
    payable: credit,
    taxCredit: number(invoice?.taxAmount),
    accountEffects,
    nonFinancial: Boolean(invoice?.nonFinancial),
  };
}

export function auditFinancialState(finance, options = {}) {
  const findings = [];
  const accounts = rows(finance?.accounts);
  const movements = rows(finance?.movements);
  const expenses = rows(finance?.expenses);
  const incoming = rows(finance?.incomingInvoices);
  const invoices = rows(finance?.invoices);
  const mobile = rows(finance?.mobileTransactions);
  const accountIds = new Set(accounts.map(row => text(row.id)).filter(Boolean));
  const expenseIds = new Set(expenses.map(row => text(row.id)).filter(Boolean));
  const incomingIds = new Set(incoming.map(row => text(row.id)).filter(Boolean));
  const invoiceNumbers = new Set(invoices.map(row => text(row.number || row.id)).filter(Boolean));

  const sourceSeen = new Map();
  for (const [collection, collectionRows] of [['فاکتور ورود', incoming], ['هزینه', expenses], ['خروج نخ', rows(finance?.yarnOutInvoices)]]) {
    for (const row of collectionRows) {
      const key = sourceKey(row);
      if (!key) continue;
      if (sourceSeen.has(key)) addFinding(findings, 'critical', 'ثبت تکراری', 'منبع دوبار در مالی ثبت شده است', `${collection} و ${sourceSeen.get(key)} هر دو به ${key} متصل‌اند.`, 'financialSupervisor', key);
      else sourceSeen.set(key, collection);
    }
  }

  for (const expense of expenses) {
    const id = text(expense.id || expense.sourceId);
    if (!(number(expense.amount) > 0) || !text(expense.date) || !text(expense.group || expense.title) || !text(expense.subgroup)) {
      addFinding(findings, 'critical', 'اطلاعات هزینه', 'هزینه ناقص است', 'تاریخ، گروه، زیرگروه و مبلغ مثبت باید کامل باشند.', 'costs', id);
    }
    if (!accountIds.has(text(expense.accountId))) addFinding(findings, 'critical', 'اثر بانک و صندوق', 'حساب پرداخت هزینه معتبر نیست', 'هزینه به بانک یا صندوق موجود متصل نشده است.', 'costs', id);
    const linked = movements.filter(row => text(row.sourceExpense) === id);
    if (linked.length !== 1) addFinding(findings, 'critical', 'ارتباط هزینه', linked.length ? 'هزینه چند گردش بانکی دارد' : 'هزینه در بانک/صندوق اعمال نشده است', `برای این هزینه باید دقیقاً یک گردش خروجی وجود داشته باشد؛ تعداد فعلی ${linked.length.toLocaleString('fa-IR')} است.`, 'costs', id);
    else {
      const movement = linked[0];
      if (text(movement.direction) !== 'out' || !near(movement.amount, expense.amount) || text(movement.accountId) !== text(expense.accountId) || text(movement.date) !== text(expense.date)) {
        addFinding(findings, 'critical', 'ارتباط هزینه', 'اثر هزینه با سند اصلی یکسان نیست', 'مبلغ، تاریخ، جهت یا حساب گردش بانک/صندوق با هزینه تفاوت دارد.', 'costs', id);
      }
    }
  }

  for (const invoice of incoming) {
    const id = text(invoice.id || invoice.sourceId);
    if (!invoice.nonFinancial) {
      const paid = rows(invoice.payments).reduce((sum, row) => sum + Math.max(0, number(row.amount)), 0);
      if (!near(paid, invoice.amount)) addFinding(findings, 'critical', 'تسویه فاکتور', 'جمع تسویه فاکتور ورود برابر مبلغ آن نیست', `مبلغ فاکتور ${number(invoice.amount).toLocaleString('fa-IR')} و جمع تسویه ${paid.toLocaleString('fa-IR')} تومان است.`, 'incomingInvoices', id);
      compareCashLinks(findings, invoice, movements, 'sourceIncomingInvoice', 'فاکتور ورود', 'incomingInvoices');
    }
    if (['yarn', 'fabric', 'spare_part'].includes(text(invoice.inventoryType))) {
      const stockLinks = rows(finance?.ownedInventory).filter(row => text(row.sourceIncomingInvoice) === id);
      if (stockLinks.length !== 1) addFinding(findings, 'critical', 'اثر موجودی', stockLinks.length ? 'فاکتور ورود چند اثر موجودی دارد' : 'اثر موجودی فاکتور ورود ثبت نشده است', `برای این فاکتور باید یک ردیف موجودی مرتبط وجود داشته باشد؛ تعداد فعلی ${stockLinks.length.toLocaleString('fa-IR')} است.`, 'inventory', id);
    }
  }

  for (const invoice of invoices) compareCashLinks(findings, invoice, movements, 'sourceInvoice', 'فاکتور فروش', 'invoices');

  for (const movement of movements) {
    const id = text(movement.id || movement.trackingNo);
    if (!accountIds.has(text(movement.accountId))) addFinding(findings, 'critical', 'اثر بانک و صندوق', 'گردش به حساب نامعتبر متصل است', 'بانک یا صندوق این گردش در فهرست حساب‌ها وجود ندارد.', 'bankCash', id);
    if (text(movement.sourceExpense) && !expenseIds.has(text(movement.sourceExpense))) addFinding(findings, 'critical', 'سند یتیم', 'گردش بانکی هزینه بدون سند مبدأ است', 'هزینه مرتبط با این گردش پیدا نشد.', 'bankCash', id);
    if (text(movement.sourceIncomingInvoice) && !incomingIds.has(text(movement.sourceIncomingInvoice))) addFinding(findings, 'critical', 'سند یتیم', 'پرداخت فاکتور ورود بدون سند مبدأ است', 'فاکتور ورود مرتبط با این گردش پیدا نشد.', 'bankCash', id);
    if (text(movement.sourceInvoice) && !invoiceNumbers.has(text(movement.sourceInvoice))) addFinding(findings, 'critical', 'سند یتیم', 'دریافت فاکتور فروش بدون سند مبدأ است', 'فاکتور فروش مرتبط با این گردش پیدا نشد.', 'bankCash', id);
  }

  const mobileKeys = new Set(mobile.map(row => text(row.externalId || row.sourceId || row.id).replace(/^sms-/, '')).filter(Boolean));
  for (const row of mobile) {
    const id = text(row.externalId || row.sourceId || row.id).replace(/^sms-/, '');
    const linked = movements.filter(movement => text(movement.sourceMobileTransaction) === id || sourceKey(movement) === `mobile_sms:${id}`);
    if (linked.length !== 1) addFinding(findings, 'critical', 'ارتباط حسابیار', linked.length ? 'تراکنش حسابیار چندبار در بانک/صندوق ثبت شده است' : 'تراکنش حسابیار به بانک/صندوق نرسیده است', `برای تراکنش ${id || '-'} باید دقیقاً یک گردش مالی وجود داشته باشد.`, 'mobileApp', id);
  }
  for (const movement of movements.filter(row => text(row.sourceMobileTransaction))) {
    const id = text(movement.sourceMobileTransaction);
    if (!mobileKeys.has(id)) addFinding(findings, 'warning', 'ارتباط حسابیار', 'گردش حسابیار ردیف نمایشی مبدأ ندارد', 'گردش در بانک/صندوق وجود دارد اما ردیف متناظر حسابیار پیدا نشد.', 'mobileApp', id);
  }

  const typed = rows(options.typedTransactions);
  const workspaceTypedKeys = new Set(movements.map(row => text(row.sourceId || row.sourceMobileTransaction)).filter(Boolean));
  for (const row of typed.filter(row => text(row.source).toUpperCase() === 'HESABYAR' && text(row.status).toLowerCase() !== 'voided')) {
    const raw = text(row.external_transaction_id).replace(/^HY-/, '');
    if (raw && !workspaceTypedKeys.has(raw) && !workspaceTypedKeys.has(`HY-${raw}`)) addFinding(findings, 'critical', 'ارتباط حسابیار', 'تراکنش ثبت‌شده حسابیار در محیط مالی نمایش داده نشده است', `تراکنش ${raw} در هسته مالی وجود دارد اما اثر آن در بانک/صندوق پیدا نشد.`, 'mobileApp', raw);
  }

  const operationalExpenses = rows(options.operationalExpenses);
  const importedOperational = new Set(expenses.filter(row => text(row.source_type) === 'operational_expense').map(row => text(row.sourceId)));
  for (const row of operationalExpenses) {
    const id = text(row.id);
    if (id && !importedOperational.has(id)) addFinding(findings, 'critical', 'ارتباط عملیاتی', 'هزینه عملیاتی به مالی نرسیده است', `${text(row.title || row.onvan_hazine) || 'هزینه'} با شناسه ${id} هنوز در هزینه‌های مالی ذخیره نشده است.`, 'costs', id);
  }

  const balances = accounts.map(account => {
    let balance = number(account.opening);
    for (const movement of movements) {
      const amount = number(movement.amount);
      if (text(movement.accountId) === text(account.id)) balance += text(movement.direction) === 'in' ? amount : -amount;
      if (text(movement.transactionType) === 'transfer' && text(movement.counterAccountId) === text(account.id)) balance += text(movement.direction) === 'in' ? -amount : amount;
    }
    return { id: text(account.id), name: text(account.name || account.id), balance };
  });
  findings.sort((left, right) => ({ critical: 0, warning: 1, info: 2 }[left.severity] - { critical: 0, warning: 1, info: 2 }[right.severity]));
  return {
    findings,
    critical: findings.filter(row => row.severity === 'critical').length,
    warnings: findings.filter(row => row.severity === 'warning').length,
    checked: expenses.length + incoming.length + invoices.length + movements.length + mobile.length,
    balances,
    sources: {
      operational: expenses.filter(row => text(row.source_type).startsWith('operational_')).length,
      hesabYar: movements.filter(row => ['mobile_sms', 'hesabyar_mobile'].includes(text(row.source_type))).length,
      manual: movements.filter(row => !text(row.source_type) || text(row.source_type) === 'manual').length,
    },
  };
}
