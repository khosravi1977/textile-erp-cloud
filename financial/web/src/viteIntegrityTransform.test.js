import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

import { transformFinancialAppSource } from '../vite-financial-integrity.js';

test('financial integrity transform hardens the real App source', () => {
  const source = fs.readFileSync(new URL('./App.jsx', import.meta.url), 'utf8');
  const transformed = transformFinancialAppSource(source);

  assert.match(transformed, /buildFinancialHealthAccurate/);
  assert.match(transformed, /normalizeAccessListStrict/);
  assert.match(transformed, /const buildFinancialHealth = buildFinancialHealthAccurate;/);
  assert.match(transformed, /const normalizeAccessList = normalizeAccessListStrict;/);
  assert.doesNotMatch(transformed, /function buildFinancialHealth\(finance\)/);
  assert.doesNotMatch(transformed, /claims\.role \|\| 'owner'/);
  assert.doesNotMatch(transformed, /: fullPageAccess;/);
});

test('financial integrity transform is idempotent', () => {
  const source = fs.readFileSync(new URL('./App.jsx', import.meta.url), 'utf8');
  const once = transformFinancialAppSource(source);
  const twice = transformFinancialAppSource(once);
  assert.equal(twice, once);
});
