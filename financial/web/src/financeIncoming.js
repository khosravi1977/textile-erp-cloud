export function isOperationalInvoiceSource(rowOrSourceType) {
  const sourceType = typeof rowOrSourceType === 'string'
    ? rowOrSourceType
    : rowOrSourceType?.source_type;
  return String(sourceType || '').startsWith('operational_');
}

export function incomingInvoiceSourceLabel(sourceType) {
  switch (sourceType) {
    case 'operational_yarn_in':
      return 'ورود نخ عملیاتی';
    case 'operational_chelle_in':
      return 'ورود چله عملیاتی';
    case 'operational_spare_part':
      return 'قطعه یدکی عملیاتی';
    case 'operational_misc':
      return 'ورودی عملیاتی';
    case 'manual':
    case '':
    case undefined:
    case null:
      return 'ثبت مالی';
    default:
      return isOperationalInvoiceSource(sourceType) ? 'عملیاتی' : 'ثبت مالی';
  }
}

export function uniqueSortedNames(values) {
  return [...new Set((values || []).map(value => String(value || '').trim()).filter(Boolean))]
    .sort((left, right) => left.localeCompare(right, 'fa'));
}
