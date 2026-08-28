import test from 'node:test';
import assert from 'node:assert/strict';

import {
  accountBalanceAccurate,
  buildFinancialHealthAccurate,
  financialPageIds,
  normalizeAccessListStrict,
} from './audit/financialIntegrity.js';

test('non-owner permission fallback fails closed', () => {
  assert.deepEqual(normalizeAccessListStrict(undefined, 'viewer'), []);
  assert.deepEqual(normalizeAccessListStrict([], 'accountant'), []);
  assert.deepEqual(normalizeAccessListStrict(['unknown-page'], 'manager'), []);
});

test('owner/admin fallback remains fully accessible when claims omit permissions', () => {
  assert.deepEqual(normalizeAccessListStrict(undefined, 'owner'), financialPageIds);
  assert.deepEqual(normalizeAccessListStrict([], 'admin'), financialPageIds);
});

test('derived financial child pages follow their parent permission', () => {
  assert.deepEqual(normalizeAccessListStrict(['incomingInvoices'], 'accountant'), ['incomingInvoices', 'chelleIncomingInvoices']);
  assert.deepEqual(normalizeAccessListStrict(['reports'], 'viewer'), ['reports', 'telegramReports']);
});

test('internal transfer preserves total cash and moves balance between accounts', () => {
  const movements = [{ accountId: 'a', counterAccountId: 'b', transactionType: 'transfer', direction: 'out', amount: 300 }];
  assert.equal(accountBalanceAccurate({ id: 'a', opening: 1000 }, movements), 700);
  assert.equal(accountBalanceAccurate({ id: 'b', opening: 500 }, movements), 800);
  assert.equal(
    accountBalanceAccurate({ id: 'a', opening: 1000 }, movements) + accountBalanceAccurate({ id: 'b', opening: 500 }, movements),
    1500,
  );
});

test('financial health uses net revenue and COGS instead of purchases as expense', () => {
  const health = buildFinancialHealthAccurate({
    invoices: [{ date: '2026-08-01', total: 1100, taxAmount: 100, costAmount: 400, payments: [{ type: 'credit', amount: 1100 }], item: 'پارچه' }],
    incomingInvoices: [{ amount: 900, taxAmount: 90, nonFinancial: false, payments: [{ type: 'credit', amount: 900 }] }],
    yarnOutInvoices: [],
    expenses: [{ amount: 110, taxAmount: 10, group: 'اداری' }],
    receivableDocs: [],
    payableDocs: [],
    openingBalances: [],
    ownedInventory: [{ amount: 900, date: '2026-08-01' }],
    accounts: [],
    movements: [],
  }, new Date('2026-08-18T00:00:00Z'));

  assert.equal(health.salesTotal, 1100);
  assert.equal(health.salesRevenue, 1000);
  assert.equal(health.revenue, 1000);
  assert.equal(health.cogs, 400);
  assert.equal(health.grossProfit, 600);
  assert.equal(health.expensesTotal, 100);
  assert.equal(health.netProfit, 500);
  assert.equal(health.varianceRows.length, 0, 'budget variance must not be fabricated');
});

test('assigned and cleared checks are excluded from receivable assets', () => {
  const health = buildFinancialHealthAccurate({
    invoices: [], incomingInvoices: [], yarnOutInvoices: [], expenses: [], openingBalances: [], ownedInventory: [], accounts: [], movements: [], payableDocs: [],
    receivableDocs: [
      { status: 'open', amount: 100 },
      { status: 'bounced', amount: 200 },
      { status: 'assigned', amount: 300 },
      { status: 'cleared', amount: 400 },
    ],
  });
  assert.equal(health.receivables, 300);
});

test('aging buckets use distinct 31-60 and 61-90 labels', () => {
  const health = buildFinancialHealthAccurate({
    invoices: [{ date: '2026-06-20', total: 100, payments: [{ type: 'credit', amount: 100 }], customer: 'A' }],
    incomingInvoices: [], yarnOutInvoices: [], expenses: [], openingBalances: [], ownedInventory: [], accounts: [], movements: [], payableDocs: [], receivableDocs: [],
  }, new Date('2026-08-18T00:00:00Z'));
  const labels = health.agingRows.map(row => row.period);
  assert.ok(labels.includes('سررسید گذشته ۳۱ تا ۶۰ روز'));
  assert.ok(labels.includes('سررسید گذشته ۶۱ تا ۹۰ روز'));
  assert.equal(new Set(labels).size, labels.length);
});
