import test from 'node:test';
import assert from 'node:assert/strict';

import { isMonetaryColumn, monetaryColumnTotals, parseLocalizedNumber } from './reportTotals.js';

test('parses Persian and Arabic monetary digits', () => {
  assert.equal(parseLocalizedNumber('۲۳٬۳۵۹٬۳۴۱٬۹۰۰ تومان'), 23359341900);
  assert.equal(parseLocalizedNumber('١,٥٨١,٠٠٠'), 1581000);
  assert.equal(parseLocalizedNumber('-'), null);
});

test('recognizes monetary columns but not unit rates', () => {
  assert.equal(isMonetaryColumn('مبلغ'), true);
  assert.equal(isMonetaryColumn('پرداخت'), true);
  assert.equal(isMonetaryColumn('نرخ واحد'), false);
  assert.equal(isMonetaryColumn('مقدار'), false);
});

test('places calculated totals at their original column indexes', () => {
  const totals = monetaryColumnTotals([
    { type: 'الف', amount: 1200, paid: '۲۰۰' },
    { type: 'ب', amount: 800, paid: '۳۰۰' },
  ], [['type', 'نوع'], ['amount', 'مبلغ'], ['paid', 'پرداخت']]);

  assert.deepEqual(totals, [
    { index: 1, key: 'amount', total: 2000 },
    { index: 2, key: 'paid', total: 500 },
  ]);
});
