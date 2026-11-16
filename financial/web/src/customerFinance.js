import { buildCustomerStatement } from './customerStatement.js';

export function customerFinance(finance, customer, { paidAmount, invoiceDebt, counterparty, dateKey }) {

  const invoices = finance.invoices.filter(x => x.customer === customer);

  const openingRows = (finance.openingBalances || []).filter(x => x.customer === customer);

  const openingReceivable = openingRows.filter(x => x.type === 'receivable').reduce((s, x) => s + Number(x.amount || 0), 0);

  const openingPayable = openingRows.filter(x => x.type === 'payable').reduce((s, x) => s + Number(x.amount || 0), 0);

  const total = invoices.reduce((s, x) => s + Number(x.total || 0), 0);

  const paid = invoices.reduce((s, x) => s + paidAmount(x), 0);

  const cash = invoices.flatMap(x => x.payments || []).filter(p => p.type === 'cash').reduce((s, p) => s + Number(p.amount || 0), 0);

  const checks = invoices.flatMap(x => x.payments || []).filter(p => p.type === 'check').reduce((s, p) => s + Number(p.amount || 0), 0);

  const barter = invoices.flatMap(x => x.payments || []).filter(p => p.type === 'barter_yarn' || p.type === 'barter_fabric').reduce((s, p) => s + Number(p.amount || 0), 0);

  const assignedChecks = finance.receivableDocs.filter(x => x.assignedTo === customer).reduce((s, x) => s + Number(x.amount || 0), 0);

  const incomingInvoices = finance.incomingInvoices.filter(x => x.customer === customer);

  const yarnOutInvoices = (finance.yarnOutInvoices || []).filter(x => x.customer === customer);

  const financialIncomingInvoices = incomingInvoices.filter(x => !x.nonFinancial);

  const nonFinancialInventoryValue = incomingInvoices.filter(x => x.nonFinancial).reduce((s, x) => s + Number(x.amount || 0), 0);

  const incomingTotal = financialIncomingInvoices.reduce((s, x) => s + Number(x.amount || 0), 0);

  const yarnOutFinancialTotal = yarnOutInvoices.filter(x => x.outMode === 'sale' || x.outMode === 'barter').reduce((s, x) => s + Number(x.amount || 0), 0);

  const paidOut = financialIncomingInvoices.reduce((s, x) => s + (x.payments || []).filter(p => p.type !== 'credit').reduce((sum, p) => sum + Number(p.amount || 0), 0), 0);

  const payableToCustomer = financialIncomingInvoices.reduce((s, x) => s + Math.max(Number(x.amount || 0) - (x.payments || []).filter(p => p.type !== 'credit').reduce((sum, p) => sum + Number(p.amount || 0), 0), 0), 0);

  const manualCustomerReceipts = (finance.movements || []).filter(x => x.transactionType === 'customer_receipt' && !x.sourceInvoice && !x.sourceIncomingInvoice && counterparty(x) === customer).reduce((s, x) => s + Number(x.amount || 0), 0);

  const manualSupplierPayments = (finance.movements || []).filter(x => x.transactionType === 'supplier_payment' && !x.sourceInvoice && !x.sourceIncomingInvoice && counterparty(x) === customer).reduce((s, x) => s + Number(x.amount || 0), 0);

  const invoiceDebtTotal = invoices.reduce((s, x) => s + invoiceDebt(x), 0);

  const bouncedChecks = finance.receivableDocs
    .filter(x => x.customer === customer && x.sourceInvoice && (x.status === 'bounced' || x.status === 'returned'))
    .reduce((s, x) => s + Number(x.amount || 0), 0);

  const independentChecks = finance.receivableDocs.filter(x => x.customer === customer && !x.sourceInvoice && !['bounced', 'returned'].includes(x.status)).reduce((sum, x) => sum + Number(x.amount || 0), 0);
  const receivableBalance = Math.max(invoiceDebtTotal + yarnOutFinancialTotal + openingReceivable + bouncedChecks - manualCustomerReceipts - independentChecks, 0);

  const directAssignedChecks = finance.receivableDocs.filter(x => x.assignedTo === customer && !x.assignedIncomingInvoice).reduce((s, x) => s + Number(x.amount || 0), 0);

  const netBalance = buildCustomerStatement(finance, customer, { dateKey, counterparty }).balance;

  return { invoices, incomingInvoices, openingRows, openingReceivable, openingPayable, total, paid, cash, checks, barter, bouncedChecks, assignedChecks, assignedDebt: 0, incomingTotal, paidOut, payableToCustomer, nonFinancialInventoryValue, yarnOutFinancialTotal, invoiceDebt: invoiceDebtTotal, debt: receivableBalance, netBalance };

}
