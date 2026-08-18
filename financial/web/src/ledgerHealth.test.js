import test from 'node:test';
import assert from 'node:assert/strict';

import { buildLedgerHealth } from './ledgerHealth.js';

test('derives profit, current ratio and balanced status from posted ledger report', () => {
  const health = buildLedgerHealth({
    summary: {
      total_debit: 250000,
      total_credit: 250000,
      income: 150000,
      expense: 90000,
      assets: 180000,
      liabilities: 70000,
      equity: 50000,
    },
    trialBalance: [
      { code: '1110A', balance: 20000 },
      { code: '1120A', balance: 30000 },
      { code: '1200', balance: 40000 },
      { code: '1210', balance: 10000 },
      { code: '1300', balance: 50000 },
      { code: '1410', balance: 5000 },
      { code: '2100', balance: -35000 },
      { code: '2110', balance: -10000 },
      { code: '2310', balance: -5000 },
    ],
  });

  assert.equal(health.netProfit, 60000);
  assert.equal(health.cash, 50000);
  assert.equal(health.receivables, 50000);
  assert.equal(health.inventory, 50000);
  assert.equal(health.currentAssets, 155000);
  assert.equal(health.currentLiabilities, 50000);
  assert.equal(health.currentRatio, 3.1);
  assert.equal(health.adjustedEquity, 110000);
  assert.equal(health.balanced, true);
  assert.equal(health.status, 'healthy');
});

test('raises critical status for unbalanced ledger, loss and negative adjusted equity', () => {
  const health = buildLedgerHealth({
    summary: {
      total_debit: 1000,
      total_credit: 900,
      income: 100,
      expense: 500,
      assets: 100,
      liabilities: 500,
      equity: 100,
    },
    trialBalance: [
      { code: '1110A', balance: 100 },
      { code: '2100', balance: -500 },
    ],
  });
  assert.equal(health.balanced, false);
  assert.equal(health.netProfit, -400);
  assert.equal(health.adjustedEquity, -300);
  assert.equal(health.status, 'critical');
  assert.ok(health.alerts.length >= 3);
});
