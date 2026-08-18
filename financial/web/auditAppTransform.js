function replaceExact(code, from, to, label, expected = 1) {
  const pieces = code.split(from);
  const count = pieces.length - 1;
  if (count !== expected) {
    throw new Error(`[accounting-audit] ${label}: expected ${expected} match(es), found ${count}`);
  }
  return pieces.join(to);
}

export function transformFinancialApp(source) {
  let code = source;

  code = replaceExact(
    code,
    "{ period: 'سررسید گذشته ۶۱ تا ۹۰ روز', min: 31, max: 60, amount: 0, customers: new Set() },",
    "{ period: 'سررسید گذشته ۳۱ تا ۶۰ روز', min: 31, max: 60, amount: 0, customers: new Set() },",
    'aging 31-60 label',
  );

  code = replaceExact(
    code,
    "x.status !== 'cleared' && x.status !== 'assigned'",
    "x.status === 'open'",
    'collectible receivable check policy',
    4,
  );

  code = replaceExact(
    code,
    "<label className=\"text-sm text-slate-300\">درصد چک<TextInput className=\"mt-2 w-full\" type=\"number\" min=\"0\" max=\"100\" value={termCheckPercent} onChange={e => setTermCheckPercent(Number(e.target.value || 0))} /></label>",
    "<label className=\"text-sm text-slate-300\">درصد چک<TextInput className=\"mt-2 w-full\" type=\"number\" min=\"0\" max=\"100\" value={termCheckPercent} onChange={e => { const v = Math.min(100, Math.max(0, Number(e.target.value || 0))); setTermCheckPercent(v); setTermCashPercent(100 - v); }} /></label>",
    'invoice payment term percentages',
  );

  code = replaceExact(
    code,
    "const purchases = finance.incomingInvoices.filter(x => inRange(x.date));",
    "const purchases = finance.incomingInvoices.filter(x => !x.nonFinancial && inRange(x.date));",
    'exclude nonfinancial incoming rows from tax purchases',
  );

  code = replaceExact(
    code,
    "{ label: 'جمع', taxable_amount: salesTotal + purchasesTotal + expensesTotal, vat: payableVat }",
    "{ label: 'جمع ردیف‌ها', taxable_amount: rows.reduce((sum, row) => sum + Number(row.taxable_amount || 0), 0), vat: rows.reduce((sum, row) => sum + Number(row.vat || 0), 0), total: rows.reduce((sum, row) => sum + Number(row.total || 0), 0) }",
    'tax Excel row reconciliation',
  );

  code = replaceExact(
    code,
    "<DangerButton onClick={resetAllFinance}>پاک کردن کل اطلاعات مالي</DangerButton>",
    "<span className=\"rounded-md border border-slate-700 px-3 py-2 text-xs text-slate-400\">پاک کردن کل اطلاعات مالی در محیط عملیاتی غیرفعال است</span>",
    'disable full financial reset control',
  );

  return code;
}

export function accountingAuditAppTransformPlugin() {
  return {
    name: 'viora-accounting-audit-app-transform',
    enforce: 'pre',
    transform(code, id) {
      if (!id.replaceAll('\\\\', '/').endsWith('/financial/web/src/App.jsx')) return null;
      return { code: transformFinancialApp(code), map: null };
    },
  };
}
