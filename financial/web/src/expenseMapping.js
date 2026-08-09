export function mapOperationalExpense(row) {
  return {
    ...row,
    group: row.group || row.title || 'سایر',
    subgroup: row.subgroup || row.weaver_name || 'سایر',
    source: 'عملیاتی',
    financialRecord: false,
  };
}

export function matchesExpenseFilters(row, filters) {
  const term = String(filters.term || '');
  const searchable = `${row.group || ''} ${row.subgroup || ''} ${row.source || ''} ${row.description || ''} ${row.doc_no || ''}`;

  return searchable.includes(term)
    && (filters.group === 'all' || row.group === filters.group)
    && (filters.subgroup === 'all' || row.subgroup === filters.subgroup)
    && (filters.source === 'all' || row.source === filters.source)
    && (filters.accountId === 'all' || row.accountId === filters.accountId);
}
