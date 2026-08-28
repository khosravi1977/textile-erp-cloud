const COUNTERPARTY_REQUIRED_TYPES = new Set([
  'customer_receipt',
  'supplier_payment',
]);

export function movementNeedsCounterparty(movement = {}) {
  return COUNTERPARTY_REQUIRED_TYPES.has(String(movement.transactionType || ''));
}

export function confirmedMovementCounterparty(movement = {}) {
  if (!movementNeedsCounterparty(movement) || movement.counterpartyConfirmed !== true) return '';
  return String(movement.payer || movement.customer || '').trim();
}

export function confirmMovementCounterparty(movement = {}, counterparty, source = 'manual_confirmation') {
  const name = String(counterparty || '').trim();
  if (!name) throw new Error('counterparty is required');
  return {
    ...movement,
    payer: name,
    customer: name,
    counterpartyConfirmed: true,
    counterpartySource: source,
  };
}

export function unconfirmedMovementCounterparty(movement = {}, candidate) {
  const name = String(candidate || movement.counterpartyCandidate || movement.payer || movement.customer || '').trim();
  const next = {
    ...movement,
    payer: '',
    customer: '',
    counterpartyConfirmed: false,
  };
  if (name) next.counterpartyCandidate = name;
  return next;
}
