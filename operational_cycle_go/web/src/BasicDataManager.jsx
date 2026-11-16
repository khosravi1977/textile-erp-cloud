import React, { useState } from 'react';

export default function BasicDataManager({ basicDefs, lookups, refresh, notify, request }) {
  const [values, setValues] = useState({});
  const [editValues, setEditValues] = useState({});

  const add = async (kind) => {
    try {
      await request(`/basic/${kind}`, { method: 'POST', body: { name: values[kind] || '' } });
      setValues(current => ({ ...current, [kind]: '' }));
      await refresh();
      notify('اطلاعات اولیه ثبت شد');
    } catch (err) {
      notify(err.message || 'ثبت اطلاعات اولیه انجام نشد');
    }
  };

  const saveEdit = async (kind, item) => {
    const key = `${kind}:${item.id}`;
    const name = String(editValues[key] ?? item.name).trim();
    if (!name) {
      notify('نام را وارد کنید');
      return;
    }
    try {
      await request(`/basic/${kind}`, { method: 'PUT', body: { id: Number(item.id), name } });
      setEditValues(current => {
        const next = { ...current };
        delete next[key];
        return next;
      });
      await refresh();
      notify('نام اطلاعات اولیه ویرایش شد');
    } catch (err) {
      notify(err.message || 'ویرایش اطلاعات اولیه انجام نشد');
    }
  };

  const remove = async (kind, item) => {
    if (!window.confirm(`«${item.name}» از فهرست حذف شود؟ سوابق قبلی تغییر نمی‌کنند و اگر رکوردی به این گزینه وابسته باشد، حذف انجام نمی‌شود.`)) return;
    try {
      await request(`/basic/${kind}`, { method: 'DELETE', body: { id: Number(item.id) } });
      await refresh();
      notify('گزینه از فهرست انتخاب بایگانی شد؛ سوابق محفوظ‌اند');
    } catch (err) {
      notify(err.message || 'حذف اطلاعات اولیه انجام نشد');
    }
  };

  return <>
    <p className="basic-info-help">با ویرایش نام، نام جدید در سوابق مرتبط هم اعمال می‌شود. حذف، گزینه را از فهرست انتخاب کنار می‌گذارد و اطلاعات تاریخی را حفظ می‌کند؛ افزودن دوبارهٔ همان نام، آن را بازمی‌گرداند.</p>
    <div className="grid two">{basicDefs.map(([kind, title]) => <section className="panel" key={kind}>
      <h2>{title}</h2>
      <div className="row"><input value={values[kind] || ''} onChange={e => setValues(current => ({ ...current, [kind]: e.target.value }))} placeholder={`ثبت ${title}`} /><button onClick={() => add(kind)}>ثبت</button></div>
      <div className="chips">{(lookups[kind] || []).map(item => {
        const key = `${kind}:${item.id}`;
        const isEditing = Object.prototype.hasOwnProperty.call(editValues, key);
        return <div className="basic-chip" key={item.id}>
          {isEditing
            ? <input aria-label={`نام جدید برای ${item.name}`} value={editValues[key]} onChange={e => setEditValues(current => ({ ...current, [key]: e.target.value }))} onKeyDown={e => { if (e.key === 'Enter') saveEdit(kind, item); if (e.key === 'Escape') setEditValues(current => { const next = { ...current }; delete next[key]; return next; }); }} />
            : <span>{item.name}</span>}
          <div className="basic-chip-actions">
            {isEditing
              ? <>
                <button type="button" onClick={() => saveEdit(kind, item)}>ذخیره</button>
                <button type="button" onClick={() => setEditValues(current => { const next = { ...current }; delete next[key]; return next; })}>لغو</button>
              </>
              : <button type="button" aria-label={`ویرایش ${item.name}`} title="ویرایش" onClick={() => setEditValues(current => ({ ...current, [key]: item.name }))}>ویرایش</button>}
            <button type="button" className="danger" aria-label={`حذف ${item.name}`} title="حذف" onClick={() => remove(kind, item)}>حذف</button>
          </div>
        </div>;
      })}</div>
    </section>)}</div>
  </>;
}
