const digitMap = new Map([
  ['۰', '0'], ['۱', '1'], ['۲', '2'], ['۳', '3'], ['۴', '4'],
  ['۵', '5'], ['۶', '6'], ['۷', '7'], ['۸', '8'], ['۹', '9'],
  ['٠', '0'], ['١', '1'], ['٢', '2'], ['٣', '3'], ['٤', '4'],
  ['٥', '5'], ['٦', '6'], ['٧', '7'], ['٨', '8'], ['٩', '9'],
]);

export function isMonetaryColumn(label) {
  const normalized = String(label || '').replace(/[\u200c\u200f\u202a-\u202e]/g, '').trim();
  if (!normalized || normalized.includes('نرخ')) return false;
  return /(مبلغ|جمع|پرداخت|دریافت|بدهکار|بستانکار|بدهی|طلب|هزینه|درآمد|مالیات|ارزش ریالی|مانده ریالی)/.test(normalized);
}

export function parseLocalizedNumber(value) {
  const localized = String(value ?? '').replace(/[۰-۹٠-٩]/g, digit => digitMap.get(digit) || digit);
  const negativeByParentheses = /^\s*\(.*\)\s*$/.test(localized);
  const cleaned = localized.replace(/[٬،,\s]/g, '').replace(/[^0-9.\-]/g, '');
  if (!cleaned || cleaned === '-' || cleaned === '.') return null;
  const number = Number(cleaned);
  if (!Number.isFinite(number)) return null;
  return negativeByParentheses ? -Math.abs(number) : number;
}

export function monetaryColumnTotals(rows, columns) {
  return columns.map((column, index) => {
    const [key, label] = Array.isArray(column) ? column : [column, column];
    if (!isMonetaryColumn(label)) return null;
    const values = rows.map(row => parseLocalizedNumber(row?.[key])).filter(value => value !== null);
    return values.length ? { index, key, total: values.reduce((sum, value) => sum + value, 0) } : null;
  }).filter(Boolean);
}
