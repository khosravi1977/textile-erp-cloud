import test from 'node:test';
import assert from 'node:assert/strict';

import { isDateWithinInclusiveRange } from './dateRange.js';

test('date range includes both boundaries', () => {
  assert.equal(isDateWithinInclusiveRange('14050501', '14050501', '14050531'), true);
  assert.equal(isDateWithinInclusiveRange('14050531', '14050501', '14050531'), true);
});

test('date range excludes rows outside the selected range', () => {
  assert.equal(isDateWithinInclusiveRange('14050431', '14050501', '14050531'), false);
  assert.equal(isDateWithinInclusiveRange('14050601', '14050501', '14050531'), false);
});

test('empty boundaries remain open ended', () => {
  assert.equal(isDateWithinInclusiveRange('14050515', '', '14050531'), true);
  assert.equal(isDateWithinInclusiveRange('14050515', '14050501', ''), true);
  assert.equal(isDateWithinInclusiveRange('', '', ''), true);
  assert.equal(isDateWithinInclusiveRange('', '14050501', ''), false);
});
