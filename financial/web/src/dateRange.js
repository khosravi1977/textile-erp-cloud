export function isDateWithinInclusiveRange(dateKey, fromDateKey, toDateKey) {
  const date = String(dateKey || '').trim();
  const from = String(fromDateKey || '').trim();
  const to = String(toDateKey || '').trim();

  if (!from && !to) return true;
  if (!date) return false;

  return (!from || date >= from) && (!to || date <= to);
}
