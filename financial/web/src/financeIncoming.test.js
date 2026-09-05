import assert from 'node:assert/strict';
import { test } from 'node:test';
import { incomingInvoiceSourceLabel, isOperationalInvoiceSource, uniqueSortedNames } from './financeIncoming.js';

test('incoming invoice source labels distinguish operational sources', () => {
  assert.equal(incomingInvoiceSourceLabel('operational_yarn_in'), 'ورود نخ عملیاتی');
  assert.equal(incomingInvoiceSourceLabel('operational_chelle_in'), 'ورود چله عملیاتی');
  assert.equal(incomingInvoiceSourceLabel('operational_spare_part'), 'قطعه یدکی عملیاتی');
  assert.equal(incomingInvoiceSourceLabel('manual'), 'ثبت مالی');
});

test('operational invoice source detection is prefix based', () => {
  assert.equal(isOperationalInvoiceSource({ source_type: 'operational_yarn_in' }), true);
  assert.equal(isOperationalInvoiceSource('operational_unknown'), true);
  assert.equal(isOperationalInvoiceSource({ source_type: 'manual' }), false);
});

test('uniqueSortedNames trims duplicates and empty values', () => {
  assert.deepEqual(uniqueSortedNames([' ب ', '', 'الف', 'ب', null]), ['الف', 'ب']);
});
