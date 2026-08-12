function normalizeDigits(value) {
  return String(value ?? '')
    .replace(/[۰-۹]/g, digit => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(digit)))
    .replace(/[٠-٩]/g, digit => String('٠١٢٣٤٥٦٧٨٩'.indexOf(digit)));
}

export function normalizeSayadId(value) {
  return normalizeDigits(value).replace(/[^0-9]/g, '');
}

export function isValidSayadId(value) {
  return /^\d{16}$/.test(normalizeSayadId(value));
}

function checkNumber(value) {
  const normalized = normalizeDigits(value).replace(/[,٬\s]/g, '');
  if (!/^\d+$/.test(normalized)) return null;
  const parsed = Number(normalized);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

function bankName(value) {
  return String(value ?? '').trim();
}

export function issuedChecksForCheckbook(checkbook, payableDocs = []) {
  if (!checkbook) return [];
  const from = checkNumber(checkbook.fromNo);
  const to = checkNumber(checkbook.toNo);
  const bank = bankName(checkbook.bank);

  return payableDocs.filter(document => {
    if (document.checkbookId) return document.checkbookId === checkbook.id;
    const number = checkNumber(document.checkNo);
    return number !== null
      && from !== null
      && to !== null
      && bankName(document.bank) === bank
      && number >= Math.min(from, to)
      && number <= Math.max(from, to);
  });
}

export function validateCheckbookUpdate(current, next, payableDocs = []) {
  const from = checkNumber(next?.fromNo);
  const to = checkNumber(next?.toNo);
  if (!bankName(next?.bank) || from === null || to === null) {
    return { valid: false, message: 'بانک و بازه شماره‌های دسته‌چک باید کامل و عددی باشد.' };
  }
  if (from > to) {
    return { valid: false, message: 'شماره شروع دسته‌چک نمی‌تواند از شماره پایان بزرگ‌تر باشد.' };
  }
  if (!current) return { valid: true, message: '' };

  const rangeChanged = checkNumber(current.fromNo) !== from || checkNumber(current.toNo) !== to;
  const bankChanged = bankName(current.bank) !== bankName(next.bank);
  if (!rangeChanged && !bankChanged) return { valid: true, message: '' };

  const incompatible = issuedChecksForCheckbook(current, payableDocs).filter(document => {
    const number = checkNumber(document.checkNo);
    return number === null
      || bankName(document.bank) !== bankName(next.bank)
      || number < from
      || number > to;
  });
  if (incompatible.length) {
    return {
      valid: false,
      message: `بانک یا بازه جدید با ${incompatible.length} چک صادرشده سازگار نیست. عنوان دسته‌چک را می‌توانید بدون تغییر بازه ویرایش کنید.`,
    };
  }
  return { valid: true, message: '' };
}
