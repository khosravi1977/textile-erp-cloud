import test from 'node:test';
import assert from 'node:assert/strict';

import { transformFinancialApp } from '../auditAppTransform.js';

const fixture = `
{ period: 'سررسید گذشته ۶۱ تا ۹۰ روز', min: 31, max: 60, amount: 0, customers: new Set() },
const a = x.status !== 'cleared' && x.status !== 'assigned';
const b = x.status !== 'cleared' && x.status !== 'assigned';
const c = x.status !== 'cleared' && x.status !== 'assigned';
const d = x.status !== 'cleared' && x.status !== 'assigned';
<label className="text-sm text-slate-300">درصد چک<TextInput className="mt-2 w-full" type="number" min="0" max="100" value={termCheckPercent} onChange={e => setTermCheckPercent(Number(e.target.value || 0))} /></label>
const purchases = finance.incomingInvoices.filter(x => inRange(x.date));
{ label: 'جمع', taxable_amount: salesTotal + purchasesTotal + expensesTotal, vat: payableVat }
<DangerButton onClick={resetAllFinance}>پاک کردن کل اطلاعات مالي</DangerButton>
`;

test('applies all accounting audit UI corrections', () => {
  const out = transformFinancialApp(fixture);
  assert.match(out, /۳۱ تا ۶۰/);
  assert.equal((out.match(/x\.status === 'open'/g) || []).length, 4);
  assert.match(out, /setTermCashPercent\(100 - v\)/);
  assert.match(out, /!x\.nonFinancial && inRange/);
  assert.match(out, /جمع ردیف‌ها/);
  assert.match(out, /غیرفعال است/);
});

test('fails closed when an expected source pattern is missing', () => {
  assert.throws(() => transformFinancialApp('unexpected source'), /expected .*match/);
});
