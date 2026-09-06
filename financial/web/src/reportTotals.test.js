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

test('payment-plan cash and cheque totals stay under their own columns, not cheque dates', () => {
  const rows = [
    { expected_cash: '۲۰۰', expected_check: 800, expected_check_date: '1405/09/24' },
    { expected_cash: 400, expected_check: '١٬٦٠٠', expected_check_date: '1405/10/24' },
    { expected_cash: 0, expected_check: 0, expected_check_date: '-' },
  ];
  const columns = [['expected_cash', 'نقد طبق قرار'], ['expected_check', 'چک طبق قرار'], ['expected_check_date', 'تاریخ چک طبق قرار']];
  assert.deepEqual(monetaryColumnTotals(rows, columns), [
    { index: 0, key: 'expected_cash', total: 600 },
    { index: 1, key: 'expected_check', total: 2400 },
  ]);
  assert.equal(monetaryColumnTotals(rows.slice(0, 1), columns)[0].total, 200);
  for (const label of ['تاریخ چک', 'تاريخ چک', 'شماره چک', 'درصد نقد', 'درصد چک', 'نرخ واحد']) assert.equal(isMonetaryColumn(label), false);
  assert.deepEqual(monetaryColumnTotals([], columns), []);
});
