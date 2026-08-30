import test from 'node:test';
import assert from 'node:assert/strict';

import { expenseTraceId, linkedExpenseTraceId, mapOperationalExpense, matchesExpenseFilters, matchesExpenseTrace } from './expenseMapping.js';

test('operational expense maps title to group and weaver to subgroup', () => {
  const mapped = mapOperationalExpense({
    id: 7,
    title: 'تعمیرات',
    weaver_name: 'بافنده شیفت شب',
    amount: 1200,
    description: 'تعویض قطعه',
  });

  assert.equal(mapped.group, 'تعمیرات');
  assert.equal(mapped.subgroup, 'بافنده شیفت شب');
  assert.equal(mapped.source, 'عملیاتی');
  assert.equal(mapped.description, 'تعویض قطعه');
  assert.equal(mapped.expenseTraceId, 'OP-EXP-7');
});

test('expense filters support subgroup and source together', () => {
  const row = { group: 'تعمیرات', subgroup: 'شیفت شب', source: 'عملیاتی', accountId: '' };

  assert.equal(matchesExpenseFilters(row, { term: '', group: 'all', subgroup: 'شیفت شب', source: 'عملیاتی', accountId: 'all' }), true);
  assert.equal(matchesExpenseFilters(row, { term: '', group: 'all', subgroup: 'شیفت روز', source: 'عملیاتی', accountId: 'all' }), false);
  assert.equal(matchesExpenseFilters(row, { term: '', group: 'all', subgroup: 'all', source: 'ثبت وب‌اپ', accountId: 'all' }), false);
});

test('operational expense keeps document number as trace id', () => {
  const mapped = mapOperationalExpense({ id: 9, onvan_hazine: 'هزینه', mablagh: '1200000', tarikh: '1405/06/07', shomare_sanad: 'SANAD-44' });

  assert.equal(mapped.documentNo, 'SANAD-44');
  assert.equal(mapped.expenseTraceId, 'SANAD-44');
  assert.equal(mapped.group, 'هزینه');
  assert.equal(mapped.amount, 1200000);
  assert.equal(mapped.date, '1405/06/07');
  assert.equal(matchesExpenseTrace(mapped, 'SANAD-44'), true);
});

test('manual expense gets a stable display trace from its id', () => {
  assert.equal(expenseTraceId({ id: 'exp-123' }), 'EXP-exp-123');
  assert.equal(linkedExpenseTraceId({ sourceExpenseTraceId: 'EXP-55', sourceExpense: 'exp-1' }), 'EXP-55');
});
