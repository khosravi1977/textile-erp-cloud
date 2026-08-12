import test from 'node:test';
import assert from 'node:assert/strict';

import { isValidSayadId, issuedChecksForCheckbook, normalizeSayadId, validateCheckbookUpdate } from './checkbook.js';

const book = { id: 'book-1', title: 'جاری صادرات', bank: 'صادرات', fromNo: '1', toNo: '99' };

test('linked issued cheque prevents treating a checkbook as unused', () => {
  const documents = [{ id: 'p1', checkbookId: 'book-1', checkNo: '7', bank: 'صادرات' }];
  assert.equal(issuedChecksForCheckbook(book, documents).length, 1);
});

test('legacy payable cheque is matched by bank and number range', () => {
  const documents = [{ id: 'p1', checkNo: '۷', bank: 'صادرات' }];
  assert.equal(issuedChecksForCheckbook(book, documents).length, 1);
});

test('cheque linked to another checkbook is not counted by overlapping range', () => {
  const documents = [{ id: 'p1', checkbookId: 'book-2', checkNo: '7', bank: 'صادرات' }];
  assert.equal(issuedChecksForCheckbook(book, documents).length, 0);
});

test('title-only edit remains valid after cheque issuance', () => {
  const documents = [{ id: 'p1', checkbookId: 'book-1', checkNo: '7', bank: 'صادرات' }];
  const result = validateCheckbookUpdate(book, { ...book, title: 'دسته اصلی' }, documents);
  assert.equal(result.valid, true);
});

test('edit cannot orphan an issued cheque by changing bank or range', () => {
  const documents = [{ id: 'p1', checkbookId: 'book-1', checkNo: '7', bank: 'صادرات' }];
  assert.equal(validateCheckbookUpdate(book, { ...book, fromNo: '10' }, documents).valid, false);
  assert.equal(validateCheckbookUpdate(book, { ...book, bank: 'ملت' }, documents).valid, false);
});

test('unused checkbook accepts bank and range edits', () => {
  const result = validateCheckbookUpdate(book, { ...book, bank: 'ملت', fromNo: '100', toNo: '150' }, []);
  assert.equal(result.valid, true);
});

test('Sayad identifier accepts Persian digits and stores sixteen ASCII digits', () => {
  assert.equal(normalizeSayadId('۱۲۳۴ ۵۶۷۸-۹۰۱۲ ۳۴۵۶'), '1234567890123456');
  assert.equal(isValidSayadId('۱۲۳۴ ۵۶۷۸-۹۰۱۲ ۳۴۵۶'), true);
  assert.equal(isValidSayadId('1234567890'), false);
});
