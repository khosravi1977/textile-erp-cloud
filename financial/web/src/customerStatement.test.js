import test from 'node:test';
import assert from 'node:assert/strict';
import { buildCustomerStatement } from './customerStatement.js';
const build = data => buildCustomerStatement(data, 'A');

test('received checks reduce debt and excess remains a creditor balance', () => {
 const finance = { invoices: [{customer:'A', number:'1', date:'2026-01-01', total:20177064900, payments:[{type:'credit',amount:20177064900}]}], receivableDocs:[{customer:'A',checkNo:'1',amount:30620000000,status:'open',receivedAt:'2026-02-01'}] };
 const result = build(finance);
 assert.equal(result.totalDebit,20177064900); assert.equal(result.totalCredit,30620000000); assert.equal(result.balance,-10442935100);
 assert.equal(result.entries[1].balance,-10442935100);
});
test('linked check and linked bank movements count just once', () => {
 const data={invoices:[{customer:'A',number:'1',total:1000,payments:[{type:'check',checkNo:'42',amount:300},{type:'cash',amount:100}]}],receivableDocs:[{customer:'A',sourceInvoice:'1',checkNo:'42',amount:300,status:'open'}],movements:[{payer:'A',direction:'in',transactionType:'customer_receipt',sourceInvoice:'1',amount:100}]};
 const result=build(data); assert.equal(result.totalCredit,400); assert.equal(result.balance,600);
});
test('bounced check reverses credit; assigned check remains credit to payer', () => {
 const data={receivableDocs:[{customer:'A',checkNo:'1',amount:200,status:'bounced',receivedAt:'2026-01-01',bouncedAt:'2026-01-03'},{customer:'A',checkNo:'2',amount:120,status:'assigned',assignedTo:'B',receivedAt:'2026-01-02'}]};
 assert.equal(build(data).balance,-120); assert.equal(buildCustomerStatement(data,'B').balance,120);
});
test('purchases, payments, opening and unrelated people keep proper sides', () => {
 const data={openingBalances:[{customer:'A',type:'receivable',amount:1000},{customer:'B',type:'receivable',amount:9999}],incomingInvoices:[{customer:'A',id:'i',amount:400,payments:[{type:'cash',amount:150}]},{customer:'A',nonFinancial:true,amount:900}],movements:[{payer:'A',direction:'in',transactionType:'customer_receipt',amount:100},{payer:'A',direction:'out',transactionType:'supplier_payment',amount:50}]};
 const result=build(data); assert.equal(result.totalDebit,1200); assert.equal(result.totalCredit,500); assert.equal(result.balance,700);
});
test('an undated document is flagged and not treated as its due date', () => {
 const result=build({receivableDocs:[{customer:'A',checkNo:'1',amount:10,dueDate:'2026-12-01',status:'open'}]}); assert.equal(result.hasUndated,true); assert.equal(result.entries[0].date,'');
});

test('manual journal party accounts and returned supplier checks are reflected', () => {
 const data={ journalEntries:[{number:'J1',date:'2026-01-01',lines:[{party:'A',accountCode:'1200',debit:70,credit:0},{party:'A',accountCode:'1120',debit:0,credit:70}]}], incomingInvoices:[{customer:'A',id:'I1',amount:500,payments:[{type:'check',checkNo:'p1',amount:200}]}], payableDocs:[{customer:'A',sourceIncomingInvoice:'I1',checkNo:'p1',amount:200,status:'bounced',bouncedAt:'2026-02-01'}]};
 const statement=build(data); assert.equal(statement.totalDebit,270); assert.equal(statement.totalCredit,700); assert.equal(statement.balance,-430);
});
