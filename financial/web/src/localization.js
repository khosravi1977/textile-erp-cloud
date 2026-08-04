const PERSIAN_DIGITS = '۰۱۲۳۴۵۶۷۸۹';

const NUMBER_COLUMNS = new Set([
  'amount',
  'balance',
  'cost',
  'count',
  'credit',
  'debit',
  'debt',
  'paid',
  'percent',
  'quantity',
  'total',
  'unit_price',
]);

const ACCOUNT_TYPE_LABELS = {
  Asset: 'دارایی',
  Liability: 'بدهی',
  Equity: 'حقوق مالکانه',
  Income: 'درآمد',
  Revenue: 'درآمد',
  Expense: 'هزینه',
};

export function toPersianDigits(value) {
  return String(value ?? '').replace(/\d/g, digit => PERSIAN_DIGITS[Number(digit)]);
}

export function localizeAccountType(value) {
  const normalized = String(value ?? '').trim();
  return ACCOUNT_TYPE_LABELS[normalized] || toPersianDigits(normalized || '-');
}

export function formatTableValue(column, value) {
  if (value === null || value === undefined || value === '') return '-';
  if (column === 'type' || column === 'accountType') return localizeAccountType(value);

  const numericValue = typeof value === 'number'
    ? value
    : NUMBER_COLUMNS.has(column) && /^-?\d+(?:\.\d+)?$/.test(String(value).trim())
      ? Number(value)
      : null;

  if (numericValue !== null && Number.isFinite(numericValue)) {
    return numericValue.toLocaleString('fa-IR', { maximumFractionDigits: 2 });
  }

  return toPersianDigits(value);
}
