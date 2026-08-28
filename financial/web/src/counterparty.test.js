import test from 'node:test';
import assert from 'node:assert/strict';

import {
  confirmMovementCounterparty,
  confirmedMovementCounterparty,
  movementNeedsCounterparty,
  unconfirmedMovementCounterparty,
} from './counterparty.js';

test('legacy or suggested names are hidden until explicitly confirmed', () => {
  assert.equal(confirmedMovementCounterparty({ transactionType: 'customer_receipt', payer: 'حاج حسن خسروی' }), '');
  assert.equal(confirmedMovementCounterparty({ transactionType: 'customer_receipt', payer: 'حاج حسن خسروی', counterpartyConfirmed: false }), '');
});

test('explicit confirmation stores and exposes the selected counterparty', () => {
  const movement = confirmMovementCounterparty({ id: 'mov-1', transactionType: 'customer_receipt' }, '  حاج حسن خسروی  ');
  assert.equal(movement.payer, 'حاج حسن خسروی');
  assert.equal(movement.customer, 'حاج حسن خسروی');
  assert.equal(movement.counterpartyConfirmed, true);
  assert.equal(confirmedMovementCounterparty(movement), 'حاج حسن خسروی');
});

test('an imported candidate is retained for review but is not assigned', () => {
  const movement = unconfirmedMovementCounterparty({ transactionType: 'supplier_payment', payer: 'نام قدیمی' });
  assert.equal(movement.counterpartyCandidate, 'نام قدیمی');
  assert.equal(movement.payer, '');
  assert.equal(movement.customer, '');
  assert.equal(confirmedMovementCounterparty(movement), '');
});

test('only customer and supplier flows require a counterparty', () => {
  assert.equal(movementNeedsCounterparty({ transactionType: 'transfer' }), false);
  assert.equal(movementNeedsCounterparty({ transactionType: 'expense' }), false, 'direct expense must not demand a party');
  assert.equal(movementNeedsCounterparty({ transactionType: 'other_income' }), false);
  assert.equal(movementNeedsCounterparty({ transactionType: 'customer_receipt' }), true);
  assert.equal(movementNeedsCounterparty({ transactionType: 'supplier_payment' }), true);
  assert.equal(movementNeedsCounterparty({}), false);
});
