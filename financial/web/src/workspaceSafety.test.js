import test from 'node:test';
import assert from 'node:assert/strict';

import { normalizeChangedTransfers } from './workspaceSafety.js';

test('normalizes a new transfer to source-to-destination out direction', () => {
  const next = normalizeChangedTransfers({}, {
    movements: [{ id: 't1', transactionType: 'transfer', direction: 'in', accountId: 'A', counterAccountId: 'B', amount: 1000 }],
  });
  assert.equal(next.movements[0].direction, 'out');
});

test('normalizes an edited transfer but leaves unchanged historical transfer untouched', () => {
  const previous = {
    movements: [
      { id: 'old', transactionType: 'transfer', direction: 'in', accountId: 'A', counterAccountId: 'B', amount: 1000 },
      { id: 'edited', transactionType: 'transfer', direction: 'in', accountId: 'A', counterAccountId: 'B', amount: 1000 },
    ],
  };
  const proposed = {
    movements: [
      { id: 'old', transactionType: 'transfer', direction: 'in', accountId: 'A', counterAccountId: 'B', amount: 1000 },
      { id: 'edited', transactionType: 'transfer', direction: 'in', accountId: 'A', counterAccountId: 'B', amount: 2500 },
    ],
  };
  const next = normalizeChangedTransfers(previous, proposed);
  assert.equal(next.movements[0].direction, 'in');
  assert.equal(next.movements[1].direction, 'out');
});

test('does not change non-transfer movements or already-correct transfers', () => {
  const next = normalizeChangedTransfers({}, {
    movements: [
      { id: 'r1', transactionType: 'customer_receipt', direction: 'in', accountId: 'A', amount: 1000 },
      { id: 't2', transactionType: 'transfer', direction: 'out', accountId: 'A', counterAccountId: 'B', amount: 1000 },
    ],
  });
  assert.equal(next.movements[0].direction, 'in');
  assert.equal(next.movements[1].direction, 'out');
});
