import test from 'node:test';
import assert from 'node:assert/strict';

import { mapOperationalExpense, matchesExpenseFilters } from './expenseMapping.js';

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
});

test('expense filters support subgroup and source together', () => {
  const row = { group: 'تعمیرات', subgroup: 'شیفت شب', source: 'عملیاتی', accountId: '' };

  assert.equal(matchesExpenseFilters(row, { term: '', group: 'all', subgroup: 'شیفت شب', source: 'عملیاتی', accountId: 'all' }), true);
  assert.equal(matchesExpenseFilters(row, { term: '', group: 'all', subgroup: 'شیفت روز', source: 'عملیاتی', accountId: 'all' }), false);
  assert.equal(matchesExpenseFilters(row, { term: '', group: 'all', subgroup: 'all', source: 'ثبت وب‌اپ', accountId: 'all' }), false);
});
