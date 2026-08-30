function cleanText(value) {
  return String(value ?? '').trim();
}

function meaningful(value) {
  const text = cleanText(value);
  return text && text !== '-' ? text : '';
}

export function expenseTraceId(row = {}) {
  const explicit = meaningful(row.expenseTraceId)
    || meaningful(row.traceId)
    || meaningful(row.documentNo)
    || meaningful(row.shomare_sanad)
    || meaningful(row.doc_no);
  if (explicit) return explicit;

  const sourceType = cleanText(row.source_type || row.sourceType);
  const sourceId = meaningful(row.sourceId || row.source_id);
  if (sourceType === 'operational_expense' && sourceId) return `OP-EXP-${sourceId}`;

  const id = meaningful(row.id);
  if (sourceType === 'operational_expense' && id) return `OP-EXP-${id}`;
  if (id) return `EXP-${id}`;
  return '';
}

export function linkedExpenseTraceId(movement = {}) {
  return meaningful(movement.sourceExpenseTraceId)
    || meaningful(movement.expenseTraceId)
    || meaningful(movement.sourceExpense)
    || '';
}

export function matchesExpenseTrace(row = {}, traceId = '') {
  const trace = cleanText(traceId);
  if (!trace) return false;
  const candidates = [
    expenseTraceId(row),
    row.id,
    row.sourceId,
    row.sourceExpense,
    row.sourceExpenseTraceId,
    row.documentNo,
    row.shomare_sanad,
    row.doc_no,
  ].map(cleanText).filter(Boolean);
  return candidates.includes(trace);
}

export function mapOperationalExpense(row) {
  const documentNo = row.documentNo || row.shomare_sanad || row.doc_no || '';
  const group = row.group || row.title || row.onvan_hazine || 'سایر';
  const subgroup = row.subgroup || row.weaver_name || 'سایر';
  const amount = Number(row.amount ?? row.mablagh ?? 0);
  const description = row.description ?? row.tozih ?? '';
  const date = row.date || row.tarikh || '';
  return {
    ...row,
    date,
    group,
    subgroup,
    title: group,
    amount,
    description,
    documentNo,
    expenseTraceId: expenseTraceId({ ...row, source_type: 'operational_expense', documentNo }),
    source: 'عملیاتی',
    financialRecord: false,
  };
}

export function matchesExpenseFilters(row, filters) {
  const term = String(filters.term || '');
  const searchable = `${row.id || ''} ${row.sourceId || ''} ${row.group || ''} ${row.subgroup || ''} ${row.source || ''} ${row.description || ''} ${row.doc_no || ''} ${row.documentNo || ''} ${row.expenseTraceId || ''}`;

  return searchable.includes(term)
    && (filters.group === 'all' || row.group === filters.group)
    && (filters.subgroup === 'all' || row.subgroup === filters.subgroup)
    && (filters.source === 'all' || row.source === filters.source)
    && (filters.accountId === 'all' || row.accountId === filters.accountId);
}
