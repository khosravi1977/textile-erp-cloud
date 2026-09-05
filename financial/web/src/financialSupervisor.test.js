import test from 'node:test';
import assert from 'node:assert/strict';
import { auditFinancialState, incomingInvoiceImpact, suggestIncomingPrice } from './financialSupervisor.js';

const account = { id: 'bank', name: 'بانک', opening: 1000 };

test('supervisor accepts a fully linked expense and calculates its bank effect', () => {
  const finance = {
    accounts: [account],
    expenses: [{ id: 'e1', date: '2026-09-01', group: 'هزینه', subgroup: 'حمل', amount: 100, accountId: 'bank' }],
    movements: [{ id: 'm1', date: '2026-09-01', direction: 'out', transactionType: 'expense', amount: 100, accountId: 'bank', sourceExpense: 'e1' }],
  };
  const audit = auditFinancialState(finance);
  assert.equal(audit.critical, 0);
  assert.equal(audit.balances[0].balance, 900);
});

test('supervisor detects missing and mismatched expense effects', () => {
  const audit = auditFinancialState({
    accounts: [account],
    expenses: [{ id: 'e1', date: '2026-09-01', group: 'هزینه', subgroup: 'حمل', amount: 100, accountId: 'bank' }],
    movements: [],
  });
  assert.equal(audit.critical, 1);
  assert.match(audit.findings[0].title, /اعمال نشده/);
});

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

test('incoming impact explains inventory, payable, bank and tax changes', () => {
  const impact = incomingInvoiceImpact({ quantity: 10, subtotal: 1000, taxAmount: 100, payments: [
    { type: 'cash', amount: 400, accountId: 'bank' },
    { type: 'credit', amount: 700 },
  ] }, [account]);
  assert.equal(impact.inventoryQuantity, 10);
  assert.equal(impact.payable, 700);
  assert.equal(impact.accountEffects[0].amount, -400);
  assert.equal(impact.taxCredit, 100);
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
