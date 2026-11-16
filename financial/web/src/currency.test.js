import test from 'node:test';
import assert from 'node:assert/strict';
import { rialColumnLabel } from './currency.js';

test('Excel declares rials on money and unit price columns only', () => {
  for (const label of ['مبلغ', 'بدهکار', 'بستانکار', 'مانده', 'نرخ واحد', 'قیمت واحد']) assert.equal(rialColumnLabel(label), `${label} (ریال)`);
  for (const label of ['تاریخ پرداخت', 'شماره چک', 'درصد نقد', 'وضعیت مانده', 'جمع وزن', 'تعداد چک', 'مقدار', 'مبلغ (ریال)']) assert.equal(rialColumnLabel(label), label);
});
