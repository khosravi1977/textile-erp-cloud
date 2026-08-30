import test from 'node:test';
import assert from 'node:assert/strict';

import { normalizeEditableJalaliDate, toLatinDigits } from './persianDateInput.js';

test('persian and arabic digits are normalized to latin digits', () => {
  assert.equal(toLatinDigits('۱۴۰۵/۰۶/۰۸'), '1405/06/08');
  assert.equal(toLatinDigits('۱۴٠۵/٠۶/۰۸'), '1405/06/08');
});

test('jalali date typing stays in editable jalali format', () => {
  assert.equal(normalizeEditableJalaliDate('۱۴۰۵-۶-۸'), '1405/06/08');
  assert.equal(normalizeEditableJalaliDate('14050608'), '1405/06/08');
  assert.equal(normalizeEditableJalaliDate('1405/06/08'), '1405/06/08');
});
