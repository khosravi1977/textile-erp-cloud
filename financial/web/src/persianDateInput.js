const persianDigits = '۰۱۲۳۴۵۶۷۸۹';
const arabicDigits = '٠١٢٣٤٥٦٧٨٩';

export function toLatinDigits(value = '') {
  return String(value)
    .replace(/[۰-۹]/g, digit => String(persianDigits.indexOf(digit)))
    .replace(/[٠-٩]/g, digit => String(arabicDigits.indexOf(digit)));
}

export function normalizeEditableJalaliDate(value = '') {
  const cleaned = toLatinDigits(value)
    .trim()
    .replace(/[.\-\s]+/g, '/')
    .replace(/\/+/g, '/');

  if (/^1[34]\d{6}$/.test(cleaned)) {
    return `${cleaned.slice(0, 4)}/${cleaned.slice(4, 6)}/${cleaned.slice(6, 8)}`;
  }

  const parts = cleaned.split('/');
  if (parts.length >= 3 && /^1[34]\d{2}$/.test(parts[0])) {
    const month = parts[1].padStart(2, '0').slice(0, 2);
    const day = parts[2].padStart(2, '0').slice(0, 2);
    return `${parts[0]}/${month}/${day}`;
  }

  return cleaned;
}
