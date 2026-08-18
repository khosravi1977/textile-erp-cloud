import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

import { transformFinancialAppSource } from '../vite-financial-integrity-v2.js';

test('financial integrity transform hardens the real App source', () => {
  const source = fs.readFileSync(new URL('./App.jsx', import.meta.url), 'utf8');
  const transformed = transformFinancialAppSource(source);

  assert.match(transformed, /buildFinancialHealthAccurate/);
  assert.match(transformed, /normalizeAccessListStrict/);
  assert.match(transformed, /const buildFinancialHealth = buildFinancialHealthAccurate;/);
  assert.match(transformed, /const normalizeAccessList = normalizeAccessListStrict;/);
  assert.match(transformed, /!x\.nonFinancial && inRange\(x\.date\)/);
  assert.match(transformed, /financial\.debt \+ futureChecks - ownedYarn - ownedFabric/);
  assert.match(transformed, /سود عملیاتی ثبت‌شده/);
  assert.match(transformed, /انحراف بودجه \(نیازمند بودجه مصوب\)/);
  assert.doesNotMatch(transformed, /function buildFinancialHealth\(finance\)/);
  assert.doesNotMatch(transformed, /claims\.role \|\| 'owner'/);
  assert.doesNotMatch(transformed, /: fullPageAccess;/);
  assert.doesNotMatch(transformed, /financial\.debt - futureChecks - ownedYarn - ownedFabric/);
  assert.doesNotMatch(transformed, /label="EBITDA"/);
});

test('financial integrity transform is idempotent', () => {
  const source = fs.readFileSync(new URL('./App.jsx', import.meta.url), 'utf8');
  const once = transformFinancialAppSource(source);
  const twice = transformFinancialAppSource(once);
  assert.equal(twice, once);
});
