import test from 'node:test';
import assert from 'node:assert/strict';
import { suggestIncomingPrice } from './financialSupervisor.js';

test('price suggestion prefers same supplier and item history', () => {
  const suggestion = suggestIncomingPrice({ incomingInvoices: [
    { id: '1', customer: 'الف', itemName: 'نخ 20/1', unitPrice: 120, date: '2026-08-01' },
    { id: '2', customer: 'الف', itemName: 'نخ 20/1', unitPrice: 100, date: '2026-07-01' },
    { id: '3', customer: 'ب', itemName: 'نخ 20/1', unitPrice: 500, date: '2026-09-01' },
  ] }, { customer: 'الف', itemName: 'نخ 20/1' });
  assert.equal(suggestion.price, 110);
  assert.equal(suggestion.samples.length, 2);
  assert.equal(suggestion.confidence, 'high');
});

test('historical pricing excludes future, consignment, other currency/unit and chelle processing', () => {
  const base = { customer: 'علي كريمي', itemName: 'نخ ۲۰', date: '2026-08-01', unitPrice: 120 };
  const suggestion = suggestIncomingPrice({ incomingInvoices: [
    { ...base, id: 'valid' }, { ...base, id: 'future', date: '2027-01-01', unitPrice: 900 },
    { ...base, id: 'consigned', nonFinancial: true, unitPrice: 800 },
    { ...base, id: 'rial', currency: 'IRR', unitPrice: 1200 },
    { ...base, id: 'unit', unit: 'meter', unitPrice: 500 },
    { ...base, id: 'chelle', source_type: 'operational_chelle_in', unitPrice: 300 },
  ] }, { customer: 'علی کریمی', itemName: 'نخ 20', date: '1405/06/10' });
  assert.equal(suggestion.price, 120);
  assert.deepEqual(suggestion.samples.map(x => x.id), ['valid']);
});

test('a missing unit price uses net-of-tax value rather than gross', () => {
  const suggestion = suggestIncomingPrice({ incomingInvoices: [{ id: 'tax', customer: 'الف', itemName: 'نخ', date: '2026-08-01', quantity: 10, amount: 1100, taxAmount: 100 }] }, { customer: 'الف', itemName: 'نخ', date: '2026-09-01' });
  assert.equal(suggestion.price, 100);
});
