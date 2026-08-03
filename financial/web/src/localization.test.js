import test from 'node:test';
import assert from 'node:assert/strict';

import { formatTableValue, localizeAccountType, toPersianDigits } from './localization.js';

test('converts identifiers and account numbers to Persian digits', () => {
  assert.equal(toPersianDigits('11274B375'), '۱۱۲۷۴B۳۷۵');
  assert.equal(formatTableValue('code', '1120'), '۱۱۲۰');
});

test('formats numeric strings with Persian digits and thousands separators', () => {
  assert.equal(formatTableValue('debit', '3865304960.00'), '۳٬۸۶۵٬۳۰۴٬۹۶۰');
  assert.equal(formatTableValue('balance', 500369471), '۵۰۰٬۳۶۹٬۴۷۱');
});

test('localizes accounting types', () => {
  assert.equal(localizeAccountType('Asset'), 'دارایی');
  assert.equal(localizeAccountType('Liability'), 'بدهی');
  assert.equal(localizeAccountType('Equity'), 'حقوق مالکانه');
  assert.equal(localizeAccountType('Income'), 'درآمد');
  assert.equal(localizeAccountType('Expense'), 'هزینه');
});
