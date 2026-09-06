import test from 'node:test';
import assert from 'node:assert/strict';

import { normalizeEditableJalaliDate, finalizeJalaliDate, lenientJalaliDate, toLatinDigits } from './persianDateInput.js';

test('persian and arabic digits are normalized to latin digits', () => {
  assert.equal(toLatinDigits('۱۴۰۵/۰۶/۰۸'), '1405/06/08');
  assert.equal(toLatinDigits('۱۴٠۵/٠۶/۰۸'), '1405/06/08');
});

test('compact 8-digit typing expands to jalali format', () => {
  assert.equal(normalizeEditableJalaliDate('14050608'), '1405/06/08');
  assert.equal(normalizeEditableJalaliDate('1405/06/08'), '1405/06/08');
});

test('mid-typing keeps partial day so multi-digit entry works', () => {
  assert.equal(normalizeEditableJalaliDate('1405/06/2'), '1405/06/2');
  assert.equal(normalizeEditableJalaliDate('1405/06/24'), '1405/06/24');
  assert.equal(normalizeEditableJalaliDate('1405/6/8'), '1405/6/8');
  assert.equal(normalizeEditableJalaliDate('1405/06/۰۲'), '1405/06/02');
});

test('blur finalize pads filled segments and never produces 00', () => {
  assert.equal(finalizeJalaliDate('1405/06/2'), '1405/06/02');
  assert.equal(finalizeJalaliDate('1405/6/8'), '1405/06/08');
  assert.equal(finalizeJalaliDate('۱۴۰۵-۶-۸'), '1405/06/08');
  assert.equal(finalizeJalaliDate('1405/06/24'), '1405/06/24');
  assert.equal(finalizeJalaliDate('1405/06/'), '1405/06');
  assert.equal(finalizeJalaliDate('1405/'), '1405');
  assert.equal(finalizeJalaliDate(''), '');
});

test('lenient cleanup only fixes separators and digits', () => {
  assert.equal(lenientJalaliDate(' ۱۴۰۵.۵.۱ '), '1405/5/1');
  assert.equal(lenientJalaliDate('1405--6///8'), '1405/6/8');
});
