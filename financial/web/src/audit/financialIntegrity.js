const PAGE_IDS = [
  'dashboard', 'financialHealth', 'initialData', 'operational', 'incomingInvoices',
  'chelleIncomingInvoices', 'yarnOutInvoices', 'invoices', 'inventory', 'costs',
  'receivableDocs', 'payableDocs', 'bankCash', 'accounting', 'reports', 'taxReports',
  'credit', 'advisor', 'telegramReports', 'mobileApp',
];

const number = value => Number.isFinite(Number(value)) ? Number(value) : 0;
const lower = value => String(value || '').trim().toLowerCase();

export function normalizeAccessListStrict(list, role = 'viewer') {
  const normalizedRole = lower(role);
  if (!Array.isArray(list) || list.length === 0) {
    return normalizedRole === 'owner' || normalizedRole === 'admin' ? [...PAGE_IDS] : [];
  }
  const allowed = list.filter(item => PAGE_IDS.includes(item));
  if (allowed.includes('incomingInvoices') && !allowed.includes('chelleIncomingInvoices')) allowed.push('chelleIncomingInvoices');
  if (allowed.includes('reports') && !allowed.includes('telegramReports')) allowed.push('telegramReports');
  return [...new Set(allowed)];
}

function paymentCreditAmount(row) {
  const payments = Array.isArray(row?.payments) ? row.payments : [];
  if (!payments.length) return Math.max(0, number(row?.total ?? row?.amount));
  return payments.filter(payment => lower(payment?.type) === 'credit').reduce((sum, payment) => sum + Math.max(0, number(payment?.amount)), 0);
}

function invoiceRevenue(row) {
  const gross = Math.max(0, number(row?.total));
  const tax = Math.min(gross, Math.max(0, number(row?.taxAmount)));
  return gross - tax;
}

function expenseNetAmount(row) {
  const gross = Math.max(0, number(row?.amount));
  const tax = Math.min(gross, Math.max(0, number(row?.taxAmount)));
  return gross - tax;
}

export function accountBalanceAccurate(account, movements = []) {
  const id = String(account?.id || '').trim();
  let balance = number(account?.opening);
  for (const movement of movements) {
    const amount = Math.max(0, number(movement?.amount));
    const direction = lower(movement?.direction);
    if (String(movement?.accountId || '').trim() === id) {
      balance += direction === 'in' ? amount : -amount;
    }
    if (lower(movement?.transactionType) === 'transfer' && String(movement?.counterAccountId || '').trim() === id) {
      balance += direction === 'in' ? -amount : amount;
    }
  }
  return balance;
}

function dateAgeDays(value, now = new Date()) {
  const date = new Date(value || '');
  if (Number.isNaN(date.getTime())) return 0;
  return Math.max(0, Math.floor((now.getTime() - date.getTime()) / 86400000));
}

function analysisPeriodDays(invoices) {
  const times = invoices.map(row => new Date(row?.date || '').getTime()).filter(Number.isFinite).sort((a, b) => a - b);
  if (times.length < 2) return 30;
  return Math.max(30, Math.ceil((times[times.length - 1] - times[0]) / 86400000) + 1);
}

export function buildFinancialHealthAccurate(finance, now = new Date()) {
  const invoices = Array.isArray(finance?.invoices) ? finance.invoices : [];
  const incomingInvoices = Array.isArray(finance?.incomingInvoices) ? finance.incomingInvoices : [];
  const yarnOutInvoices = Array.isArray(finance?.yarnOutInvoices) ? finance.yarnOutInvoices : [];
  const expenses = Array.isArray(finance?.expenses) ? finance.expenses : [];
  const receivableDocs = Array.isArray(finance?.receivableDocs) ? finance.receivableDocs : [];
  const payableDocs = Array.isArray(finance?.payableDocs) ? finance.payableDocs : [];
  const openingBalances = Array.isArray(finance?.openingBalances) ? finance.openingBalances : [];
  const ownedInventory = Array.isArray(finance?.ownedInventory) ? finance.ownedInventory : [];
  const accounts = Array.isArray(finance?.accounts) ? finance.accounts : [];
  const movements = Array.isArray(finance?.movements) ? finance.movements : [];

  const salesTotal = invoices.reduce((sum, row) => sum + Math.max(0, number(row?.total)), 0);
  const salesRevenue = invoices.reduce((sum, row) => sum + invoiceRevenue(row), 0);
  const yarnRevenue = yarnOutInvoices
    .filter(row => ['sale', 'barter'].includes(lower(row?.outMode)))
    .reduce((sum, row) => sum + Math.max(0, number(row?.amount)), 0);
  const revenue = salesRevenue + yarnRevenue;
  const cogs = invoices.reduce((sum, row) => sum + Math.max(0, number(row?.costAmount)), 0) + yarnOutInvoices
    .filter(row => ['sale', 'barter'].includes(lower(row?.outMode)) && lower(row?.stockType) !== 'amanat')
    .reduce((sum, row) => sum + Math.max(0, number(row?.costAmount)), 0);
  const expensesTotal = expenses.reduce((sum, row) => sum + expenseNetAmount(row), 0);
  const grossProfit = revenue - cogs;
  const netProfit = grossProfit - expensesTotal;

  const invoiceReceivables = invoices.reduce((sum, row) => sum + paymentCreditAmount(row), 0);
  const checkReceivables = receivableDocs
    .filter(row => !['cleared', 'assigned'].includes(lower(row?.status)))
    .reduce((sum, row) => sum + Math.max(0, number(row?.amount)), 0);
  const openingReceivables = openingBalances
    .filter(row => lower(row?.type) === 'receivable')
    .reduce((sum, row) => sum + Math.max(0, number(row?.amount)), 0);
  const receivables = invoiceReceivables + checkReceivables + openingReceivables;

  const purchaseCredits = incomingInvoices
    .filter(row => !row?.nonFinancial)
    .reduce((sum, row) => sum + paymentCreditAmount(row), 0);
  const checkPayables = payableDocs
    .filter(row => lower(row?.status) !== 'paid')
    .reduce((sum, row) => sum + Math.max(0, number(row?.amount)), 0);
  const openingPayables = openingBalances
    .filter(row => lower(row?.type) === 'payable')
    .reduce((sum, row) => sum + Math.max(0, number(row?.amount)), 0);
  const payables = purchaseCredits + checkPayables + openingPayables;

  const inventoryValue = ownedInventory.reduce((sum, row) => sum + number(row?.amount), 0);
  const cashBalance = accounts.reduce((sum, account) => sum + accountBalanceAccurate(account, movements), 0);
  const currentAssets = cashBalance + receivables + inventoryValue;
  const equity = currentAssets - payables;
  const currentRatio = payables > 0 ? currentAssets / payables : currentAssets > 0 ? 9.99 : 0;
  const debtToEquity = Math.abs(equity) > 0.000001 ? payables / Math.abs(equity) : payables > 0 ? 9.99 : 0;
  const netProfitMargin = revenue > 0 ? (netProfit / revenue) * 100 : 0;

  const operatingCashFlow = movements.reduce((sum, movement) => {
    const type = lower(movement?.transactionType);
    if (type === 'transfer' || type === 'capital') return sum;
    const amount = Math.max(0, number(movement?.amount));
    return sum + (lower(movement?.direction) === 'in' ? amount : -amount);
  }, 0);

  const periodDays = analysisPeriodDays(invoices);
  const purchasesNet = incomingInvoices.filter(row => !row?.nonFinancial).reduce((sum, row) => {
    const gross = Math.max(0, number(row?.amount));
    const tax = Math.min(gross, Math.max(0, number(row?.taxAmount)));
    return sum + gross - tax;
  }, 0);
  const dso = revenue > 0 ? Math.round(receivables / Math.max(revenue / periodDays, 1)) : 0;
  const dio = cogs > 0 ? Math.round(Math.max(0, inventoryValue) / Math.max(cogs / periodDays, 1)) : 0;
  const dpo = purchasesNet > 0 ? Math.round(payables / Math.max(purchasesNet / periodDays, 1)) : 0;
  const ccc = dso + dio - dpo;

  const revenueMap = new Map();
  for (const row of invoices) {
    const item = row?.item || 'نامشخص';
    const current = revenueMap.get(item) || { item, total: 0, cost: 0 };
    current.total += invoiceRevenue(row);
    current.cost += Math.max(0, number(row?.costAmount));
    revenueMap.set(item, current);
  }
  const revenueRows = [...revenueMap.values()].map(row => {
    const gross = row.total - row.cost;
    return {
      item: row.item,
      total: Math.round(row.total),
      cost: Math.round(row.cost),
      gross_profit: Math.round(gross),
      margin_percent: row.total > 0 ? Math.round((gross / row.total) * 1000) / 10 : 0,
    };
  }).sort((a, b) => b.total - a.total);

  const expenseMap = new Map();
  for (const row of expenses) {
    const title = row?.group || row?.title || 'سایر';
    expenseMap.set(title, (expenseMap.get(title) || 0) + expenseNetAmount(row));
  }
  const expenseRows = [...expenseMap.entries()].map(([title, amount]) => ({
    title,
    amount: Math.round(amount),
    percent: expensesTotal > 0 ? Math.round((amount / expensesTotal) * 1000) / 10 : 0,
  })).sort((a, b) => b.amount - a.amount).slice(0, 10);

  const agingBuckets = [
    { period: 'مطالبات جاری', min: 0, max: 0, amount: 0, customers: new Set() },
    { period: 'سررسید گذشته ۱ تا ۳۰ روز', min: 1, max: 30, amount: 0, customers: new Set() },
    { period: 'سررسید گذشته ۳۱ تا ۶۰ روز', min: 31, max: 60, amount: 0, customers: new Set() },
    { period: 'سررسید گذشته ۶۱ تا ۹۰ روز', min: 61, max: 90, amount: 0, customers: new Set() },
    { period: 'سررسید گذشته بیش از ۹۰ روز', min: 91, max: Number.POSITIVE_INFINITY, amount: 0, customers: new Set() },
  ];
  for (const row of invoices) {
    const debt = paymentCreditAmount(row);
    if (debt <= 0) continue;
    const dueValue = row?.dueDate || row?.paymentDueDate || row?.date;
    const age = dateAgeDays(dueValue, now);
    const bucket = agingBuckets.find(item => age >= item.min && age <= item.max) || agingBuckets[0];
    bucket.amount += debt;
    if (row?.customer) bucket.customers.add(row.customer);
  }
  const agingRows = agingBuckets.map(bucket => ({
    period: bucket.period,
    count: bucket.customers.size,
    amount: Math.round(bucket.amount),
    percent: receivables > 0 ? Math.round((bucket.amount / receivables) * 1000) / 10 : 0,
  }));

  const staleInventory = ownedInventory.filter(row => dateAgeDays(row?.date, now) > 365 && number(row?.amount) > 0);
  const staleValue = staleInventory.reduce((sum, row) => sum + number(row?.amount), 0);

  // No budget/master target source is currently persisted in this workspace.
  // Returning invented variances is worse than returning no variance rows.
  const varianceRows = [];
  const alerts = [
    currentRatio < 1 && { severity: 'بحرانی', message: 'ریسک نقدینگی بالا است؛ دارایی جاری از بدهی جاری کمتر است.' },
    netProfit < 0 && { severity: 'بحرانی', message: 'عملکرد ثبت‌شده در این دوره زیان عملیاتی نشان می‌دهد.' },
    dso > 60 && { severity: 'هشدار', message: 'دوره وصول مطالبات از ۶۰ روز عبور کرده است.' },
    inventoryValue > 0 && staleValue > inventoryValue * 0.1 && { severity: 'هشدار', message: 'موجودی راکد بیش از ۱۰٪ موجودی ثبت‌شده است.' },
  ].filter(Boolean);
  const status = alerts.some(item => item.severity === 'بحرانی') ? 'critical' : alerts.length ? 'warning' : 'healthy';
  const narrative = [
    revenue > 0 ? `درآمد خالص از مالیات ${Math.round(revenue).toLocaleString('fa-IR')} تومان و سود عملیاتی ${Math.round(netProfit).toLocaleString('fa-IR')} تومان است.` : 'برای محاسبه سودآوری هنوز درآمد قابل اتکایی ثبت نشده است.',
    `مطالبات جاری ${Math.round(receivables).toLocaleString('fa-IR')} تومان و بدهی جاری ${Math.round(payables).toLocaleString('fa-IR')} تومان است.`,
    varianceRows.length ? '' : 'برای تحلیل انحراف بودجه، ابتدا بودجه یا هدف مصوب باید در سامانه ثبت شود؛ عدد فرضی نمایش داده نمی‌شود.',
  ].filter(Boolean);
  const recommendations = [
    currentRatio < 1 ? 'وصول مطالبات و زمان‌بندی پرداخت‌های کوتاه‌مدت در اولویت قرار گیرد.' : 'برنامه وصول و پرداخت به‌صورت هفتگی کنترل شود.',
    dso > 60 ? 'برای مطالبات معوق، سقف اعتبار و فروش نسیه بازبینی شود.' : 'شرایط تسویه و تاریخ سررسید در فاکتورهای جدید کامل ثبت شود.',
    revenueRows.some(row => row.margin_percent < 0) ? 'قیمت یا بهای تمام‌شده اقلام با حاشیه منفی بررسی شود.' : 'تمرکز فروش روی اقلام با حاشیه سود ثبت‌شده بالاتر انجام شود.',
  ];

  return {
    salesTotal,
    salesRevenue,
    revenue,
    cogs,
    grossProfit,
    netProfit,
    expensesTotal,
    receivables,
    payables,
    inventoryValue,
    staleValue,
    currentAssets,
    status,
    alerts,
    kpis: {
      netProfitMargin: Math.round(netProfitMargin * 10) / 10,
      operatingCashFlow: Math.round(operatingCashFlow),
      currentRatio: Math.round(currentRatio * 100) / 100,
      ebitda: Math.round(netProfit),
      debtToEquity: Math.round(debtToEquity * 100) / 100,
    },
    revenueRows,
    expenseRows,
    cash: { dso, dio, dpo, ccc },
    agingRows,
    varianceRows,
    narrative,
    recommendations,
  };
}

export { PAGE_IDS as financialPageIds };
