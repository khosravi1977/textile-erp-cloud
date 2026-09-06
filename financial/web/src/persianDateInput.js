const persianDigits = '۰۱۲۳۴۵۶۷۸۹';
const arabicDigits = '٠١٢٣٤٥٦٧٨٩';

export function toLatinDigits(value = '') {
  return String(value)
    .replace(/[۰-۹]/g, digit => String(persianDigits.indexOf(digit)))
    .replace(/[٠-٩]/g, digit => String(arabicDigits.indexOf(digit)));
}

// typing-safe cleanup: never pads or truncates segments mid-edit
export function lenientJalaliDate(value = '') {
  return toLatinDigits(value)
    .trim()
    .replace(/[.\-\s]+/g, '/')
    .replace(/\/+/g, '/');
}

export function normalizeEditableJalaliDate(value = '') {
  const cleaned = lenientJalaliDate(value);

  if (/^1[34]\d{6}$/.test(cleaned)) {
    return `${cleaned.slice(0, 4)}/${cleaned.slice(4, 6)}/${cleaned.slice(6, 8)}`;
  }

  return cleaned;
}

// blur-time canonical form: pads only the segments actually filled, never invents 00
export function finalizeJalaliDate(value = '') {
  const cleaned = normalizeEditableJalaliDate(value).replace(/\/+$/, '');
  const parts = cleaned.split('/');
  if (parts.length === 3 && /^1[34]\d{2}$/.test(parts[0])) {
    const month = parts[1] ? parts[1].padStart(2, '0').slice(0, 2) : '';
    const day = parts[2] ? parts[2].padStart(2, '0').slice(0, 2) : '';
    const tail = [month, day].filter(Boolean).join('/');
    return tail ? `${parts[0]}/${tail}` : parts[0];
  }
  return cleaned;
}
