// A party statement is derived from source records; it never changes the ledger.
const rows = (value) => Array.isArray(value) ? value : [];
const amount = (value) => Number(value || 0);
const text = (value) => String(value ?? '').trim();

export function buildCustomerStatement(finance, customer, { dateKey = value => text(value), counterparty = row => row.payer || row.customer || '' } = {}) {
  const result = [];
  const add = (date, description, debit, credit, kind = '') => {
    debit = amount(debit); credit = amount(credit);
    if (debit || credit) result.push({ date: text(date), description, debit, credit, kind });
  };
  const sales = rows(finance.invoices).filter(row => row.customer === customer);
  const received = rows(finance.receivableDocs).filter(row => row.customer === customer);
  const invoiceByNumber = new Map(sales.map(row => [text(row.number), row]));
  for (const row of rows(finance.openingBalances).filter(row => row.customer === customer)) {
    add(row.date, row.description || 'مانده افتتاحیه', row.type === 'receivable' ? row.amount : 0, row.type === 'payable' ? row.amount : 0, 'opening');
  }
  for (const invoice of sales) {
    add(invoice.date, `فاکتور فروش ${text(invoice.number)}`, invoice.total, 0, 'sale');
    for (const payment of rows(invoice.payments)) {
      if (payment.type === 'credit') continue;
      const hasCheck = payment.type === 'check' && text(payment.checkNo) && received.some(doc => text(doc.sourceInvoice) === text(invoice.number) && text(doc.checkNo) === text(payment.checkNo));
      if (!hasCheck) add(payment.date || invoice.date, `تسویه فاکتور ${text(invoice.number)}${payment.checkNo ? ` - چک ${payment.checkNo}` : ''}`, 0, payment.amount, 'sale-payment');
    }
  }
  for (const doc of received) {
    const invoice = invoiceByNumber.get(text(doc.sourceInvoice));
    const date = doc.receivedAt || invoice?.date || '';
    add(date, `چک دریافتی ${text(doc.checkNo)}${doc.sourceInvoice ? ` - فاکتور ${doc.sourceInvoice}` : ''}`, 0, doc.amount, 'check');
    if (doc.status === 'bounced' || doc.status === 'returned') add(doc.bouncedAt || doc.returnedAt || '', `برگشت چک ${text(doc.checkNo)}`, doc.amount, 0, 'check-return');
  }
  for (const invoice of rows(finance.incomingInvoices).filter(row => row.customer === customer && !row.nonFinancial)) {
    add(invoice.date, `فاکتور خرید ${text(invoice.id)}`, 0, invoice.amount, 'purchase');
    for (const payment of rows(invoice.payments)) {
      if (payment.type === 'credit') continue;
      add(payment.date || invoice.date, `پرداخت فاکتور خرید ${text(invoice.id)}`, payment.amount, 0, 'purchase-payment');
      const doc = rows(finance.payableDocs).find(doc => text(doc.sourceIncomingInvoice) === text(invoice.id) && text(payment.checkNo) && text(doc.checkNo) === text(payment.checkNo));
      if (doc && ['bounced', 'returned'].includes(doc.status)) add(doc.bouncedAt || doc.returnedAt || '', `برگشت چک پرداختی ${text(doc.checkNo)}`, 0, doc.amount, 'payable-return');
    }
  }
  for (const invoice of rows(finance.yarnOutInvoices).filter(row => row.customer === customer && (row.outMode === 'sale' || row.outMode === 'barter'))) add(invoice.date, `فاکتور خروج نخ ${text(invoice.number || invoice.id)}`, invoice.amount, 0, 'yarn-sale');
  for (const movement of rows(finance.movements).filter(row => !row.sourceInvoice && !row.sourceIncomingInvoice && counterparty(row) === customer)) {
    if (movement.transactionType === 'customer_receipt' && movement.direction === 'in') add(movement.date, movement.description || 'دریافت از مشتری', 0, movement.amount, 'receipt');
    if (movement.transactionType === 'supplier_payment' && movement.direction === 'out') add(movement.date, movement.description || 'پرداخت به شخص', movement.amount, 0, 'payment');
  }
  for (const doc of rows(finance.payableDocs).filter(row => row.customer === customer && !row.sourceIncomingInvoice)) {
    add(doc.issuedAt || '', `چک پرداختی ${text(doc.checkNo)}`, doc.amount, 0, 'payable-check');
    if (['bounced', 'returned'].includes(doc.status)) add(doc.bouncedAt || doc.returnedAt || '', `برگشت چک پرداختی ${text(doc.checkNo)}`, 0, doc.amount, 'payable-return');
  }
  for (const doc of rows(finance.receivableDocs).filter(row => row.assignedTo === customer && row.status === 'assigned' && !row.assignedIncomingInvoice)) add(doc.assignedAt || '', `واگذاری چک ${text(doc.checkNo)}`, doc.amount, 0, 'assigned-check');
  for (const journal of rows(finance.journalEntries)) {
    for (const line of rows(journal.lines).filter(line => line.party === customer && ['1200', '2100'].includes(text(line.accountCode)))) add(journal.date, `سند دستی ${text(journal.number || journal.id)} - ${text(line.description || journal.description)}`, line.debit, line.credit, 'journal');
  }
  result.sort((a, b) => text(dateKey(a.date)).localeCompare(text(dateKey(b.date))));
  let balance = 0;
  let totalDebit = 0, totalCredit = 0;
  const entries = result.map(row => { totalDebit += row.debit; totalCredit += row.credit; balance += row.debit - row.credit; return { ...row, balance }; });
  return { entries, totalDebit, totalCredit, balance, hasUndated: entries.some(row => !row.date) };
}
