const n = value => Number(value || 0);

const codeOf = row => String(row?.code || '').trim();
const balanceOf = row => n(row?.balance);

const sumBalances = (rows, predicate, liability = false) => rows
  .filter(predicate)
  .reduce((sum, row) => sum + (liability ? -balanceOf(row) : balanceOf(row)), 0);

export function buildLedgerHealth(report = {}) {
  const rows = Array.isArray(report.trialBalance) ? report.trialBalance : [];
  const summary = report.summary || {};

  const totalDebit = n(summary.total_debit);
  const totalCredit = n(summary.total_credit);
  const income = n(summary.income);
  const expense = n(summary.expense);
  const assets = n(summary.assets);
  const liabilities = n(summary.liabilities);
  const postedEquity = n(summary.equity);
  const netProfit = income - expense;
  const adjustedEquity = postedEquity + netProfit;

  const cash = sumBalances(rows, row => /^11/.test(codeOf(row)));
  const receivables = sumBalances(rows, row => /^(1200|1210)/.test(codeOf(row)));
  const inventory = sumBalances(rows, row => /^13/.test(codeOf(row)));
  const inputVat = sumBalances(rows, row => /^1410/.test(codeOf(row)));
  const currentAssets = cash + receivables + inventory + inputVat;

  const currentLiabilities = sumBalances(
    rows,
    row => /^(2100|2110|2190|2310)/.test(codeOf(row)),
    true,
  );

  const workingCapital = currentAssets - currentLiabilities;
  const currentRatio = currentLiabilities > 0 ? currentAssets / currentLiabilities : null;
  const profitMargin = income > 0 ? (netProfit / income) * 100 : null;
  const debtToEquity = adjustedEquity > 0 ? liabilities / adjustedEquity : null;
  const imbalance = Math.abs(totalDebit - totalCredit);
  const tolerance = Math.max(1, Math.max(Math.abs(totalDebit), Math.abs(totalCredit)) * 0.000001);
  const balanced = imbalance <= tolerance;

  const alerts = [];
  if (!balanced) alerts.push({ severity: 'critical', title: 'عدم توازن دفترکل', message: `اختلاف بدهکار و بستانکار ${Math.round(imbalance).toLocaleString('fa-IR')} تومان است.` });
  if (netProfit < 0) alerts.push({ severity: 'critical', title: 'زیان خالص', message: 'جمع هزینه‌های ثبت‌شده از درآمدهای ثبت‌شده بیشتر است.' });
  if (adjustedEquity <= 0) alerts.push({ severity: 'critical', title: 'حقوق مالکانه منفی یا صفر', message: 'نسبت بدهی به حقوق مالکانه در این وضعیت قابل اتکا نیست.' });
  if (currentRatio !== null && currentRatio < 1) alerts.push({ severity: 'critical', title: 'نسبت جاری کمتر از ۱', message: 'دارایی‌های جاری ثبت‌شده برای پوشش بدهی‌های جاری کافی نیست.' });
  else if (currentRatio !== null && currentRatio < 1.5) alerts.push({ severity: 'warning', title: 'حاشیه نقدینگی محدود', message: 'نسبت جاری بین ۱ و ۱.۵ است و باید روند وصول/پرداخت کنترل شود.' });
  if (profitMargin !== null && profitMargin < 5 && netProfit >= 0) alerts.push({ severity: 'warning', title: 'حاشیه سود پایین', message: 'حاشیه سود خالص کمتر از ۵٪ است.' });

  const status = alerts.some(item => item.severity === 'critical')
    ? 'critical'
    : alerts.some(item => item.severity === 'warning')
      ? 'warning'
      : 'healthy';

  return {
    totalDebit,
    totalCredit,
    balanced,
    imbalance,
    income,
    expense,
    netProfit,
    profitMargin,
    assets,
    liabilities,
    postedEquity,
    adjustedEquity,
    debtToEquity,
    cash,
    receivables,
    inventory,
    inputVat,
    currentAssets,
    currentLiabilities,
    workingCapital,
    currentRatio,
    alerts,
    status,
  };
}
