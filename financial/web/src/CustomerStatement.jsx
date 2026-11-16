import React from 'react';

const columns = [['date','تاریخ'],['description','شرح'],['debit','بدهکار'],['credit','بستانکار'],['balance','مانده'],['balance_kind','وضعیت مانده']];
const escape = value => String(value ?? '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
const label = balance => balance > 0 ? 'بدهکار' : balance < 0 ? 'بستانکار' : 'تسویه';

export default function CustomerStatement({ customer, statement, money, toJalali, onPrint, onExcel }) {
  const records = statement.entries.map(row => ({ date: row.date ? toJalali(row.date) : 'ثبت نشده', description: row.description, debit: row.debit, credit: row.credit, balance: Math.abs(row.balance), balance_kind: label(row.balance) }));
  const totalRow = { date: 'جمع و مانده نهایی', description: '', debit: statement.totalDebit, credit: statement.totalCredit, balance: Math.abs(statement.balance), balance_kind: label(statement.balance) };
  const print = () => {
    const headers = columns.map(([, title]) => `<th>${title}</th>`).join('');
    const rowHTML = row => `<tr>${columns.map(([key]) => `<td>${escape(['debit','credit','balance'].includes(key) ? money(row[key]) : row[key])}</td>`).join('')}</tr>`;
    onPrint(`صورت حساب ${customer}`, `<p>طرف حساب: ${escape(customer)} | تمام سوابق ثبت شده | مبالغ به تومان</p><table><thead><tr>${headers}</tr></thead><tbody>${records.map(rowHTML).join('')}</tbody><tfoot>${rowHTML(totalRow)}</tfoot></table>`);
  };
  return <section className="rounded-xl border border-slate-700 bg-slate-900 p-4">
    <div className="mb-4 flex flex-wrap items-center justify-between gap-3"><div><h3 className="font-bold">صورت حساب کامل {customer}</h3><p className="mt-1 text-xs text-slate-400">تمام سوابق شخص؛ مستقل از فیلتر نوع و تاریخ گزارش بالا. مبالغ به تومان.</p></div><div className="flex gap-2"><button className="rounded-md bg-blue-600 px-4 py-2" onClick={print}>چاپ صورت حساب</button><button className="rounded-md bg-blue-600 px-4 py-2" onClick={() => onExcel(`صورت حساب ${customer}`, records, columns, { label:'جمع و مانده نهایی', debit:statement.totalDebit, credit:statement.totalCredit, balance:Math.abs(statement.balance), balance_kind:label(statement.balance) })}>اکسل صورت حساب</button></div></div>
    {statement.hasUndated && <p className="mb-3 text-amber-300">تاریخ ثبت بعضی رویدادها موجود نیست؛ ترتیب تاریخی آنها نیاز به بررسی دارد.</p>}
    <p className="mb-3 font-bold">مانده نهایی: {money(Math.abs(statement.balance))} تومان {label(statement.balance)}</p>
    <div className="overflow-x-auto"><table className="w-full text-right text-sm"><thead><tr>{columns.map(([, title]) => <th className="p-2" key={title}>{title}</th>)}</tr></thead><tbody>{records.map((row, i) => <tr className="border-t border-slate-700" key={i}>{columns.map(([key]) => <td className="p-2" key={key}>{['debit','credit','balance'].includes(key) ? money(row[key]) : row[key]}</td>)}</tr>)}</tbody><tfoot><tr className="border-t-2 border-emerald-700 font-bold">{columns.map(([key]) => <td className="p-2" key={key}>{['debit','credit','balance'].includes(key) ? money(totalRow[key]) : totalRow[key]}</td>)}</tr></tfoot></table></div>
  </section>;
}
