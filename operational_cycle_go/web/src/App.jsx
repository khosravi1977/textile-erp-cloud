import React, { useEffect, useMemo, useState } from 'react';

import { createRoot } from 'react-dom/client';

import JsBarcode from 'jsbarcode';

import { BrowserMultiFormatReader } from '@zxing/browser';

import QRCode from 'qrcode';

import './style.css';



const API = window.ERP_OPERATIONAL_API || import.meta.env.VITE_API_BASE || (
  window.location.port === '5174'
    ? `${window.location.protocol}//${window.location.hostname}:8091/api`
    : (window.location.pathname.startsWith('/operational/') ? '/api/operational/api' : '/api')
);
const PORTAL_OPERATIONAL_SESSION = window.ERP_PORTAL_OPERATIONAL_SESSION || (
  window.location.pathname.startsWith('/operational/')
    ? '/api/portal/operational-session'
    : ''
);
const LOCAL_MODE = Boolean(window.ERP_LOCAL_MODE);



const tabs = [

  ['formulas', 'فرمول تولید ماشین‌ها'],

  ['dashboard', 'داشبورد'],

  ['initial', 'اطلاعات اولیه'],

  ['nakh-vor', 'ورود نخ'],

  ['chelle', 'ورود چله'],

  ['gere', 'گره'],

  ['nakh-salon', 'ورود نخ سالن'],

  ['salon', 'سالن تولید'],

  ['consumption', 'مصرف تار و پود و ضایعات'],

  ['reports', 'گزارشات'],

  ['yarn-out', 'خروج نخ'],

  ['empty-beam-out', 'خروج نورد خالی'],

  ['out-invoice', 'فاکتور خروج'],

  ['expenses', 'هزینه‌ها']

];



const basicDefs = [

  ['customers', 'مشتری / شخص'],

  ['yarns', 'نوع نخ'],

  ['fabrics', 'نام کالا / پارچه'],

  ['warpers', 'چله پیچ'],

  ['beams', 'کد نورد'],

  ['tiers', 'گره زن'],

  ['operators', 'نام اپراتور'],

  ['drivers', 'نام راننده'],

  ['costs', 'عنوان هزینه'],

  ['weavers', 'نام بافنده'],

  ['serviceTypes', 'انواع خدمات سرویسکاری'],

  ['spareParts', 'قطعات مصرفی']

];



const sidebarTabs = [

  ['dashboard', 'داشبورد'],

  ['initial', 'اطلاعات اولیه'],

  ['nakh-vor', 'ورود نخ'],

  ['chelle', 'ورود چله'],

  ['gere', 'گره'],

  ['nakh-salon', 'ورود نخ سالن'],

  ['formulas', 'فرمول تولید ماشین‌ها'],

  ['salon', 'سالن تولید'],

  ['consumption', 'مصرف تار و پود و ضایعات'],

  ['yarn-out', 'خروج نخ'],

  ['empty-beam-out', 'خروج نورد خالی'],

  ['out-invoice', 'فاکتور خروج'],

  ['expenses', 'هزینه‌ها'],

  ['reports', 'گزارشات'],

  ['database', 'مدیریت دیتابیس'],

  ['machinery-services', 'خدمات ماشین‌آلات'],

  ['spare-parts', 'موجودی انبار قطعات'],

  ['users', 'مدیریت کاربران']

];



function App() {

  const [tab, setTab] = useState('dashboard');

  const [lookups, setLookups] = useState({});

  const [toast, setToast] = useState('');

  const [error, setError] = useState('');

  const [status, setStatus] = useState({ go: false, db: false, message: 'در حال بررسی ارتباط...' });

  const [authBooting, setAuthBooting] = useState(Boolean(PORTAL_OPERATIONAL_SESSION || localStorage.getItem('operationalUser')));

  const [session, setSession] = useState(() => {

    try { return JSON.parse(localStorage.getItem('operationalUser') || 'null'); } catch { return null; }

  });



  const refreshLookups = async () => {

    try {

      setLookups(await api('/lookups'));

      setError('');

    } catch (err) {

      setError(err.message);

    }

  };



  useEffect(() => {

    if (session) {

      refreshLookups();

      return;

    }

    setLookups({});

  }, [session]);

  useEffect(() => {

    let cancelled = false;



    const bootstrapAuth = async () => {

      const stored = localStorage.getItem('operationalUser');

      if (stored && !PORTAL_OPERATIONAL_SESSION) {

        try {

          const res = await api('/session');

          const next = { user: res.user, menus: res.menus || [] };

          if (cancelled) return;

          localStorage.setItem('operationalUser', JSON.stringify(next));

          setSession(next);

          setError('');

          setTab('dashboard');

        } catch (err) {

          localStorage.removeItem('operationalUser');

          if (!cancelled) {

            setSession(null);

            setError('');

          }

        } finally {

          if (!cancelled) {

            setAuthBooting(false);

          }

        }

        return;

      }

      if (!PORTAL_OPERATIONAL_SESSION) {

        setAuthBooting(false);

        return;

      }

      try {

        const res = await fetch(PORTAL_OPERATIONAL_SESSION, {

          credentials: 'same-origin',

          headers: { Accept: 'application/json' },

        });

        const data = await res.json().catch(() => ({}));

        if (!res.ok || !data.user) throw new Error(data.error || 'نشست عملیاتی پورتال در دسترس نیست');

        const next = { user: data.user, menus: data.menus || [] };

        if (cancelled) return;

        localStorage.setItem('operationalUser', JSON.stringify(next));

        setSession(next);

        setError('');

        setTab('dashboard');

      } catch (err) {

        if (!cancelled) {

          setError('');

        }

      } finally {

        if (!cancelled) {

          setAuthBooting(false);

        }

      }

    };



    bootstrapAuth();

    return () => {

      cancelled = true;

    };

  }, []);

  useEffect(() => {

    const check = async () => {

      try {

        const health = await api('/health');

        setStatus({ go: !!health.ok, db: !!health.ok, message: 'سرویس عملیاتی در دسترس است' });

      } catch (err) {

        setStatus({ go: false, db: false, message: err.message || 'ارتباط قطع است' });

      }

    };

    check();

    const timer = setInterval(check, 15000);

    return () => clearInterval(timer);

  }, []);

  useEffect(() => {

    const handleAuthExpired = () => {

      localStorage.removeItem('operationalUser');

      setSession(null);

    };

    window.addEventListener('operational-auth-expired', handleAuthExpired);

    return () => window.removeEventListener('operational-auth-expired', handleAuthExpired);

  }, []);



  const notify = (msg) => {

    setToast(msg);

    setTimeout(() => setToast(''), 2600);

  };

  const login = async (username, password) => {

    const res = await api('/login', { method: 'POST', body: { username, password } });

    const next = { user: res.user, menus: res.menus || [] };

    localStorage.setItem('operationalUser', JSON.stringify(next));

    setSession(next);

    setTab('dashboard');

  };

  const logout = async () => {

    try {

      await api('/logout', { method: 'POST' });

    } catch (err) {

    }

    localStorage.removeItem('operationalUser');

    setSession(null);

  };

  if (!session) {

    if (authBooting) return <div className="login-screen" dir="rtl">

      <div className="login-card">

        <h1>ERP نساجی</h1>

        <p>در حال اتصال خودکار به بخش عملیاتی...</p>

      </div>

    </div>;

    return <LoginPage onLogin={login} status={status} error={error} />;

  }

  const allowedTabs = new Set((session.menus || []).map(m => m.menu_key));

  const visibleTabs = sidebarTabs.filter(([id]) => allowedTabs.size === 0 || allowedTabs.has(id));

  if (visibleTabs.length && !visibleTabs.some(([id]) => id === tab)) {

    setTimeout(() => setTab(visibleTabs[0][0]), 0);

  }



  return (

    <div className="app-shell">

      <aside className="sidebar">

        <h1>ERP نساجی</h1>

        <p>سیکل عملیاتی Go</p>

        <nav>{visibleTabs.map(([id, label]) => <button key={id} className={tab === id ? 'active' : ''} onClick={() => setTab(id)}>{label}</button>)}</nav>

        <button className="ghost" onClick={logout}>خروج</button>

      </aside>

      <main className="workspace">

        <header className="topbar">

          <div><strong>وضعیت ارتباط</strong><span>{status.message}</span></div>

          <div className="connection-lights">

            <span className={status.go ? 'ok' : 'bad'}>Go</span>

            <span className={status.db ? 'ok' : 'bad'}>DB</span>

          </div>

          <button onClick={refreshLookups}>بروزرسانی لیست‌ها</button>

        </header>

        {error && <div className="error-box">{error}</div>}

        {toast && <div className="toast">{toast}</div>}

        <ActiveTab tab={tab} lookups={lookups} notify={notify} refreshLookups={refreshLookups} />

      </main>

    </div>

  );

}



function ActiveTab({ tab, lookups, notify, refreshLookups }) {

  switch (tab) {

    case 'initial': return <InitialData lookups={lookups} refresh={refreshLookups} notify={notify} />;

    case 'nakh-vor': return <NakhVor lookups={lookups} notify={notify} />;

    case 'chelle': return <Chelle lookups={lookups} notify={notify} />;

    case 'gere': return <Gere lookups={lookups} notify={notify} />;

    case 'nakh-salon': return <NakhSalon lookups={lookups} notify={notify} />;

    case 'salon': return <Salon lookups={lookups} notify={notify} />;

    case 'formulas': return <MachineFormulas notify={notify} />;

    case 'consumption': return <ConsumptionPro />;

    case 'reports': return <ReportsPro2 lookups={lookups} />;

    case 'yarn-out': return <YarnOut lookups={lookups} notify={notify} />;

    case 'empty-beam-out': return <EmptyBeamOut lookups={lookups} notify={notify} />;

    case 'out-invoice': return <OutInvoicePro lookups={lookups} notify={notify} />;

    case 'expenses': return <Expenses lookups={lookups} notify={notify} />;

    case 'database': return <DatabaseManager notify={notify} />;

    case 'machinery-services': return <MachineryServices lookups={lookups} notify={notify} refreshLookups={refreshLookups} />;

    case 'spare-parts': return <SparePartsInventory lookups={lookups} notify={notify} refreshLookups={refreshLookups} />;

    case 'users': return <UsersManager notify={notify} />;

    default: return <DashboardMother />;

  }

}



function LoginPage({ onLogin, status }) {

  const [username, setUsername] = useState('admin');

  const [password, setPassword] = useState('');

  const [error, setError] = useState('');

  const [loading, setLoading] = useState(false);

  const inviteContext = useMemo(() => {
    if (typeof window === 'undefined') return { username: '', companyName: '', entry: '' };
    const params = new URLSearchParams(window.location.search);
    return {
      username: (params.get('username') || '').trim(),
      companyName: (params.get('company_name') || '').trim(),
      entry: (params.get('entry') || '').trim(),
    };
  }, []);

  useEffect(() => {
    if (inviteContext.username) {
      setUsername(inviteContext.username);
    }
  }, [inviteContext.username]);

  const submit = async (e) => {

    e.preventDefault();

    setLoading(true);

    setError('');

    try {

      await onLogin(username, password);

    } catch (err) {

      setError(err.message || 'ورود ناموفق بود');

    } finally {

      setLoading(false);

    }

  };

  return <div className="login-screen" dir="rtl">

    <form className="login-card" onSubmit={submit}>

      <h1>ERP نساجی</h1>

      <p>ورود به بخش عملیاتی Go</p>

      <div className="connection-lights login-lights">

        <span className={status.go ? 'ok' : 'bad'}>Go</span>

        <span className={status.db ? 'ok' : 'bad'}>DB</span>

      </div>

      {error && <div className="error-box">{error}</div>}

      {inviteContext.entry && (
        <div className="info-box">
          <strong>{inviteContext.entry === 'invite-first-login' ? 'ورود اولیه آماده است' : 'این دسترسی قبلا فعال شده است'}</strong>
          <span>{inviteContext.entry === 'invite-first-login'
            ? 'ثبت‌نام انجام شده است. با نام کاربری آماده‌شده وارد شوید و سپس رمز عبور شخصی خود را تنظیم کنید.'
            : 'این لینک قبلا استفاده شده است. فقط با نام کاربری و رمز عبور خودتان وارد شوید.'}</span>
          {inviteContext.companyName && <span>شرکت: {inviteContext.companyName}</span>}
          {inviteContext.username && <span>نام کاربری: <bdi>{inviteContext.username}</bdi></span>}
        </div>
      )}

      <Input label="نام کاربری" value={username} onChange={setUsername} />

      <Input label="رمز عبور" value={password} onChange={setPassword} type="password" />

      <button className="primary" disabled={loading}>{loading ? 'در حال ورود...' : 'ورود'}</button>

      <small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small>

    </form>

  </div>;

}



function Dashboard() {

  const [data, setData] = useState({});

  useEffect(() => { api('/dashboard').then(setData).catch(() => {}); }, []);

  const cards = [

    ['ورود نخ', data.nakh_vor_count, 'رکورد'],

    ['چله ثبت شده', data.chelle_count, 'رکورد'],

    ['گره', data.gere_count, 'رکورد'],

    ['موجودی خالص نخ سالن', fmt(data.nakh_salon_net), 'کیلو'],

    ['طاقه تولید شده', data.salon_count, 'طاقه'],

    ['متراژ تولید', fmt(data.salon_metr), 'متر']

  ];

  return <Page title="داشبورد سیکل تولید">

    <div className="metrics">{cards.map(([t, v, u]) => <div className="metric" key={t}><span>{t}</span><b>{v ?? 0}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>)}</div>

    <section className="panel"><h2>مسیر تست</h2><p>ورود نخ، ورود چله، گره، ورود نخ سالن و سالن تولید اکنون چرخه اصلی تست هستند. تب‌های بعدی ایجاد شده‌اند تا مرحله به مرحله تکمیل و عیب‌یابی شوند.</p></section>

  </Page>;

}



function DashboardPro() {

  const [data, setData] = useState({});

  useEffect(() => { api('/dashboard').then(setData).catch(() => {}); }, []);

  const stock = data.stock || {};

  const today = data.today || {};

  const month = data.month || {};

  const cards = [

    ['ورود نخ', data.nakh_vor_count, 'رکورد'],

    ['خروج نخ', data.nakh_khor_count, 'رکورد'],

    ['فاکتور خروج', data.out_invoice_count, 'فاکتور'],

    ['هزینه‌ها', fmt(data.expense_total), 'تومان'],

    ['طاقه موجود', stock.total_taghe, 'طاقه'],

    ['موجودی نخ سالن', fmt(data.nakh_salon_net), 'کیلو']

  ];

  return <Page title="داشبورد عملیاتی">

    <div className="metrics">{cards.map(([t, v, u]) => <div className="metric" key={t}><span>{t}</span><b>{v ?? 0}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>)}</div>

    <div className="grid two">

      <section className="panel">

        <h2>تولید امروز و ماه جاری</h2>

        <div className="mini-metrics">

          <div><span>طاقه امروز</span><b>{fmt(today.pieces)}</b></div>

          <div><span>متراژ امروز</span><b>{fmt(today.metr)}</b></div>

          <div><span>وزن امروز</span><b>{fmt(today.weight)}</b></div>

          <div><span>طاقه ماه</span><b>{fmt(month.pieces)}</b></div>

          <div><span>متراژ ماه</span><b>{fmt(month.metr)}</b></div>

          <div><span>وزن ماه</span><b>{fmt(month.weight)}</b></div>

        </div>

      </section>

      <section className="panel">

        <h2>هشدارها</h2>

        {(data.notifications || []).length ? <div className="notice-list">{data.notifications.map((x, i) => <div key={i}><b>{x.title}</b><span>{x.message}</span></div>)}</div> : <div className="empty">هشدار مهمی وجود ندارد.</div>}

      </section>

    </div>

    <section className="panel">

      <h2>موجودی پارچه قابل خروج</h2>

      <Table rows={stock.items || []} columns={[['kala','کالا'],['taghe_count','تعداد طاقه'],['metr','متراژ'],['weight','وزن']]} hideActions />

    </section>

    <div className="grid two">

      <section className="panel"><h2>آخرین تولیدها</h2><Table rows={data.latest_production || []} columns={[['tarikh','تاریخ'],['id','کد طاقه'],['machine','ماشین'],['kala','کالا'],['metr','متراژ'],['weight','وزن']]} hideActions /></section>

      <section className="panel"><h2>آخرین فاکتورهای خروج</h2><Table rows={data.latest_invoices || []} columns={[['tarikh','تاریخ'],['invoice_no','شماره فاکتور'],['mosh','مشتری'],['kala','کالا'],['taghe_count','طاقه']]} hideActions /></section>

    </div>

    <section className="panel">

      <h2>موجودی نخ بر اساس مشتری و همبافت</h2>

      <Table rows={data.yarn_inventory || []} columns={[['mosh','مشتری'],['hambaft','همبافت'],['inventory','مانده'],['vorud','ورود'],['to_salon','ارسال سالن'],['khoroj','خروج']]} hideActions />

    </section>

  </Page>;

}



function DashboardMother() {

  const [data, setData] = useState({});

  useEffect(() => { api('/dashboard').then(setData).catch(() => {}); }, []);

  const machines = data.machines || [];

  const monthly = data.month_production || buildMonthlyMachineTrend(data.latest_production || []);

  const todayMachines = data.today_by_machine || [];

  const stock = data.stock || {};

  const totalMetr = monthly.reduce((s, x) => s + Number(x.metr || 0), 0);

  const totalWeight = monthly.reduce((s, x) => s + Number(x.weight || 0), 0);

  return <Page title="داشبورد">

    <div className="metrics">

      <div className="metric"><span>تعداد طاقه تولید</span><b>{fmt(data.salon_count)}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>

      <div className="metric"><span>متراژ تولید</span><b>{fmt(data.salon_metr)}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>

      <div className="metric"><span>وزن تولید</span><b>{fmt(data.salon_weight)}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>

      <div className="metric"><span>فاکتور خروج</span><b>{fmt(data.out_invoice_count)}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>

      <div className="metric"><span>هزینه‌ها</span><b>{fmt(data.expense_total)}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>

      <div className="metric"><span>موجودی طاقه</span><b>{fmt(data.stock?.total_taghe)}</b><small>{'ورود با نام کاربری و رمز ثبت‌شده انجام می‌شود.'}</small></div>

    </div>

    <section className="panel dashboard-panel">

      <div className="panel-title-row"><h2>📦 موجودی طاقه‌های انبار</h2><span>{fmt(stock.total_taghe)} طاقه</span></div>

      <div className="mini-metrics">

        <div><span>تعداد طاقه</span><b>{fmt(stock.total_taghe)}</b></div>

        <div><span>جمع متراژ</span><b>{fmt(stock.total_metr)} متر</b></div>

        <div><span>جمع وزن</span><b>{fmt(stock.total_weight)} kg</b></div>

      </div>

      <Table rows={stock.items || []} columns={[['kala','کالا'],['taghe_count','تعداد طاقه'],['metr','متراژ'],['weight','وزن']]} hideActions />

    </section>

    <section className="panel dashboard-panel">

      <div className="panel-title-row"><h2>📊 تولید امروز به تفکیک ماشین</h2><span>{data.date || ''}</span></div>

      <Table rows={todayMachines} columns={[['machine','ماشین'],['pieces','تعداد طاقه'],['metr','متراژ'],['weight','وزن']]} hideActions />

    </section>

    <section className="panel dashboard-panel">

      <div className="panel-title-row"><h2>وضعیت مصرف تار و پود ماشین‌ها</h2><button onClick={() => api('/dashboard').then(setData)}>بروزرسانی</button></div>

      <div className="machine-card-grid">{machines.map(m => {

        const pct = Math.max(0, Math.min(100, Number(m.remaining_percent || 0)));

        const status = pct <= 10 ? 'بحرانی' : pct <= 45 ? 'کم' : 'موجود';

        return <div className={`machine-status ${pct <= 10 ? 'danger' : pct <= 45 ? 'warn' : 'ok'}`} key={`${m.machine}-${m.shom_chelle}`}>

          <h3>ماشین {m.machine}</h3>

          <p>چله: {m.shom_chelle} | تولید: {fmt(m.total_weight)} kg</p>

          <div className="progress-line"><i style={{ width: `${pct}%` }} /></div>

          <div><span>{fmt(pct)}%</span><span>{status}</span></div>

        </div>;

      })}</div>

    </section>

    <section className="panel dashboard-panel">

      <div className="panel-title-row"><h2>روند تولید ماه جاری</h2><span>{data.date ? String(data.date).slice(0, 7) : ''}</span></div>

      <div className="trend-list">{monthly.map(x => <div className="trend-row" key={x.machine}><span>{x.machine}</span><div><i style={{ width: `${Math.min(100, (Number(x.metr || 0) / Math.max(1, totalMetr)) * 100)}%` }}>{fmt(x.metr)} m</i></div><b>{fmt(x.weight)} kg</b></div>)}</div>

      <div className="dashboard-total">متراژ کل: {fmt(totalMetr)} متر | وزن کل: {fmt(totalWeight)} kg</div>

    </section>

  </Page>;

}



function buildMonthlyMachineTrend(rows) {

  const byMachine = {};

  for (const r of rows || []) {

    const key = r.machine || '-';

    if (!byMachine[key]) byMachine[key] = { machine: key, metr: 0, weight: 0 };

    byMachine[key].metr += Number(r.metr || 0);

    byMachine[key].weight += Number(r.weight || 0);

  }

  return Object.values(byMachine).sort((a, b) => Number(a.machine) - Number(b.machine));

}



function InitialData({ lookups, refresh, notify }) {

  const [values, setValues] = useState({});

  const [confirm, setConfirm] = useState('');

  const add = async (kind) => {

    await api(`/basic/${kind}`, { method: 'POST', body: { name: values[kind] || '' } });

    setValues({ ...values, [kind]: '' });

    await refresh();

    notify('اطلاعات اولیه ثبت شد');

  };

  const reset = async () => {

    await api('/reset-cycle', { method: 'POST', body: { confirm } });

    notify('اطلاعات سیکل تولید پاک شد');

  };

  return <Page title="اطلاعات اولیه">

    <div className="grid two">{basicDefs.map(([kind, title]) => <section className="panel" key={kind}>

      <h2>{title}</h2>

      <div className="row"><input value={values[kind] || ''} onChange={e => setValues({ ...values, [kind]: e.target.value })} placeholder={`ثبت ${title}`} /><button onClick={() => add(kind)}>ثبت</button></div>

      <div className="chips">{(lookups[kind] || []).map(x => <span key={x.id}>{x.name}</span>)}</div>

    </section>)}</div>

    <section className="panel danger">

      <h2>پاک کردن اطلاعات سیکل</h2>

      <p>فقط رکوردهای ورود نخ، ورود چله، گره، نخ سالن و سالن تولید پاک می‌شود؛ اطلاعات پایه باقی می‌ماند.</p>

      <div className="row"><input value={confirm} onChange={e => setConfirm(e.target.value)} placeholder="برای تایید بنویس: پاک شود" /><button onClick={reset}>پاک کردن سیکل</button></div>

    </section>

  </Page>;

}



function NakhVor({ lookups, notify }) {

  return <CrudPage

    title="ورود نخ"

    endpoint="/nakh-vor"

    empty={{ hambaft: '', weight: '', mosh_id: '', nakh_id: '' }}

    notify={notify}

    filters={[['mosh','مشتری'],['nakh','نخ'],['hambaft','همبافت']]}

    mapEdit={row => ({ id: row.id, hambaft: row.hambaft, weight: Math.abs(Number(row.weight || 0)), mosh_id: row.mosh_id || '', nakh_id: row.nakh_id || '' })}

    renderForm={(form, set) => <>

      <Input label="همبافت نخ" value={form.hambaft} onChange={v => set('hambaft', v)} />

      <Input label="وزن" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />

      <Select label="مشتری" value={form.mosh_id} onChange={v => set('mosh_id', Number(v))} items={lookups.customers} />

      <Select label="نوع نخ" value={form.nakh_id} onChange={v => set('nakh_id', Number(v))} items={lookups.yarns} />

    </>}

    columns={[['tarikh','تاریخ'],['hambaft','همبافت'],['weight','وزن'],['mosh','مشتری'],['nakh','نخ']]}

  />;

}



function Chelle({ lookups, notify }) {

  return <CrudPage

    title="ورود چله"

    endpoint="/chelle"

    empty={{ shom_chelle: '', nakh_id: '', weight: '', pich_id: '', mosh_id: '', hambaft: '', kod_navard_id: '' }}

    notify={notify}

    filters={[['shom_chelle','شماره چله'],['pich','چله پیچ'],['mosh','مشتری'],['hambaft','همبافت']]}

    mapEdit={row => ({ id: row.id, shom_chelle: row.shom_chelle, nakh_id: row.nakh_id || '', weight: Number(row.weight || 0), pich_id: row.pich_id || '', mosh_id: row.mosh_id || '', hambaft: row.hambaft || '', kod_navard_id: row.kod_navard_id || '' })}

    renderForm={(form, set) => <>

      <Input label="شماره چله" value={form.shom_chelle} onChange={v => set('shom_chelle', v)} />

      <Select label="نوع نخ" value={form.nakh_id} onChange={v => set('nakh_id', Number(v))} items={lookups.yarns} />

      <Input label="وزن چله" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />

      <Select label="چله پیچ" value={form.pich_id} onChange={v => set('pich_id', Number(v))} items={lookups.warpers} />

      <Select label="مشتری" value={form.mosh_id} onChange={v => set('mosh_id', Number(v))} items={lookups.customers} />

      <Input label="همبافت چله" value={form.hambaft} onChange={v => set('hambaft', v)} />

      <Select label="کد نورد" value={form.kod_navard_id} onChange={v => set('kod_navard_id', Number(v))} items={lookups.beams} />

    </>}

    columns={[['tarikh','تاریخ'],['shom_chelle','شماره چله'],['nakh','نخ'],['weight','وزن'],['pich','چله پیچ'],['mosh','مشتری'],['hambaft','همبافت'],['kod_navard','نورد'],['machine','ماشین']]}

  />;

}



function Gere({ lookups, notify }) {

  const [chelles, setChelles] = useState([]);

  const load = () => api('/gere?available=1').then(setChelles).catch(() => setChelles([]));

  useEffect(() => { load(); }, []);

  return <CrudPage

    title="گره"

    endpoint="/gere"

    empty={{ gerezan_id: '', chelle_id: '', machine: '' }}

    notify={notify}

    afterSave={load}

    filters={[['name_gere','گره زن'],['shom_chelle','شماره چله'],['machine','ماشین']]}

    mapEdit={row => ({ id: row.id, gerezan_id: row.gerezan_id || '', chelle_id: row.chelle_id || '', machine: row.machine || '' })}

    renderForm={(form, set) => <>

      <Select label="گره زن" value={form.gerezan_id} onChange={v => set('gerezan_id', Number(v))} items={lookups.tiers} />

      <Select label="شماره چله" value={form.chelle_id} onChange={v => set('chelle_id', Number(v))} items={uniqueOptions(chelles.map(x => ({ id: x.id, name: `${x.shom_chelle} - ${x.hambaft} - ${fmt(x.weight)} کیلو` })), form.chelle_id)} />

      <Input label="شماره ماشین" value={form.machine} onChange={v => set('machine', v)} />

    </>}

    columns={[['tarikh','تاریخ'],['name_gere','گره زن'],['shom_chelle','شماره چله'],['machine','ماشین']]}

  />;

}



function NakhSalon({ lookups, notify }) {

  const [chelles, setChelles] = useState([]);

  const load = () => api('/nakh-salon?chelles=1').then(setChelles).catch(() => setChelles([]));

  useEffect(() => { load(); }, []);

  return <CrudPage

    title="ورود نخ سالن"

    endpoint="/nakh-salon"

    empty={{ machine: '', ham_nakh: '', weight: '', chelle_id: '', mosh_name: '', vor_khor: 'vorud' }}

    notify={notify}

    filters={[['machine','ماشین'],['ham_nakh','همبافت نخ'],['shom_chelle','چله'],['mosh_name','مشتری'],['vor_khor','نوع']]}

    mapEdit={row => ({ id: row.id, machine: row.machine || '', ham_nakh: row.ham_nakh || '', weight: Math.abs(Number(row.weight || 0)), chelle_id: row.chelle_id || '', mosh_name: row.mosh_name || '', vor_khor: row.vor_khor || 'vorud' })}

    renderForm={(form, set) => <>

      <Input label="شماره ماشین" value={form.machine} onChange={v => set('machine', v)} />

      <Select label="همبافت نخ" value={form.ham_nakh} onChange={v => set('ham_nakh', v)} items={(lookups.hambaftYarn || []).map(x => ({ id: x, name: x }))} />

      <Input label="وزن" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />

      <Select label="شماره چله روی ماشین" value={form.chelle_id} onChange={v => set('chelle_id', Number(v))} items={uniqueOptions(chelles.map(x => ({ id: x.id, name: `${x.shom_chelle} - ماشین ${x.machine}` })), form.chelle_id)} />

      <Input label="نام مشتری" value={form.mosh_name} onChange={v => set('mosh_name', v)} list={(lookups.customers || []).map(x => x.name)} />

      <Select label="نوع" value={form.vor_khor} onChange={v => set('vor_khor', v)} items={[{id:'vorud',name:'ورود'}, {id:'khoroj',name:'خروج'}]} />

    </>}

    columns={[['tarikh','تاریخ'],['machine','ماشین'],['ham_nakh','همبافت نخ'],['weight','وزن'],['shom_chelle','چله'],['mosh_name','مشتری'],['vor_khor','نوع']]}

  />;

}



function Salon({ lookups, notify }) {

  const [recent, setRecent] = useState([]);

  const [nextId, setNextId] = useState('');

  const [form, setForm] = useState(salonEmpty());

  const [items, setItems] = useState([]);

  const [filters, setFilters] = useState({});

  const [editing, setEditing] = useState(false);

  const [loading, setLoading] = useState(true);



  const load = async () => {

    setLoading(true);

    try {

      const [rows, next] = await Promise.all([api('/salon'), api('/next-salon-id')]);

      setItems(rows);

      setNextId(next.next_id);

    } finally {

      setLoading(false);

    }

  };



  useEffect(() => { load(); }, []);

  const set = (k, v) => setForm(s => ({ ...s, [k]: v }));



  const loadMachineDefaults = async (machine) => {

    set('machine', machine);

    if (!machine) return setRecent([]);

    const [recentData, defaults] = await Promise.all([

      api(`/salon/recent-chelles/${encodeURIComponent(machine)}`),

      api(`/salon/defaults/${encodeURIComponent(machine)}`)

    ]);

    const recentItems = recentData.items || [];

    setRecent(recentItems);

    const latest = recentItems[0];

    let selectedChelle = latest?.shom_chelle || (defaults.found ? defaults.shom_chelle : '');

    let selectedHambaft = latest?.hambaft || (defaults.found ? defaults.ham_chelle : '');

    const previousChelle = defaults.found ? defaults.shom_chelle : '';

    if (latest?.shom_chelle && previousChelle && latest.shom_chelle !== previousChelle) {

      const useNew = window.confirm(`برای ماشین ${machine} چله جدید ${latest.shom_chelle} گره خورده است. آیا این طاقه از چله جدید است؟\nOK = چله جدید\nCancel = چله قبلی ${previousChelle}`);

      if (!useNew) {

        selectedChelle = previousChelle;

        selectedHambaft = defaults.ham_chelle || selectedHambaft;

      } else {

        const carryKey = `pod-carryover:${machine}:${previousChelle}:${latest.shom_chelle}`;

        if (!localStorage.getItem(carryKey)) {

          try {

            const info = await api(`/salon/pod-carryover-info/${encodeURIComponent(machine)}/${encodeURIComponent(previousChelle)}`);

            const leftover = Number(info.leftover_pod || 0);

            if (leftover > 0) {

              const assignNew = window.confirm(`از چله قبلی حدود ${fmt(leftover)} کیلو پود باقیمانده محاسبه شد. آیا به چله جدید ${latest.shom_chelle} منتقل شود؟\nOK = انتقال به چله جدید\nCancel = مرجوع به انبار`);

              await api('/salon/pod-carryover', { method: 'POST', body: { machine, old_chelle: previousChelle, new_chelle: latest.shom_chelle, action: assignNew ? 'assign_new' : 'return_inventory' } });

              localStorage.setItem(carryKey, assignNew ? 'assign_new' : 'return_inventory');

              notify(assignNew ? 'پود باقیمانده به چله جدید منتقل شد' : 'پود باقیمانده به انبار مرجوع شد');

            }

          } catch {

            notify('امکان تعیین تکلیف پود باقیمانده وجود نداشت');

          }

        }

      }

    }

    setForm(s => {

      return {

        ...s,

        machine,

        kala_id: defaults.found && defaults.kala_id ? defaults.kala_id : s.kala_id,

        ham_pod: defaults.found ? (defaults.ham_pod || s.ham_pod) : s.ham_pod,

        ham_chelle: selectedHambaft || s.ham_chelle,

        shom_chelle: selectedChelle || s.shom_chelle

      };

    });

  };



  const save = async (printAfterSave = true) => {

    const savedLabel = labelData();

    await api('/salon', { method: 'POST', body: form });

    notify(editing ? 'طاقه ویرایش شد' : 'طاقه ثبت شد');

    if (!form.skip_print) printLabel(savedLabel);

    setForm(salonEmpty());

    setEditing(false);

    setRecent([]);

    await load();

  };



  const edit = async (row) => {

    setEditing(true);

    setForm({ id: row.id, metr: Number(row.metr || 0), weight: Number(row.weight || 0), machine: row.machine || '', kala_id: row.kala_id || '', ham_pod: row.ham_pod || '', ham_chelle: row.ham_chelle || '', shom_chelle: row.shom_chelle || '', user: row.user || 'admin', skip_print: true });

    if (row.machine) {

      const data = await api(`/salon/recent-chelles/${encodeURIComponent(row.machine)}`);

      setRecent(data.items || []);

    }

    window.scrollTo({ top: 0, behavior: 'smooth' });

  };

  const del = async (id) => { await api(`/salon/${id}`, { method: 'DELETE' }); await load(); notify('طاقه حذف شد'); };



  const labelData = () => ({

    id: editing ? form.id : nextId,

    metr: form.metr,

    weight: form.weight,

    kala: (lookups.fabrics || []).find(x => String(x.id) === String(form.kala_id))?.name || '',

    hamPod: form.ham_pod,

    hamChelle: form.ham_chelle,

    shomChelle: form.shom_chelle

  });

  const visible = filterRows(items, filters);

  const filterDefs = [['machine','ماشین'],['kala','کالا'],['shom_chelle','چله'],['ham_chelle','همبافت تار/چله'],['ham_pod','همبافت پود']];



  return <Page title="سالن تولید">

    <section className="panel salon-panel">

      <div className="taghe-card"><span>{editing ? 'ویرایش کد طاقه' : 'کد طاقه بعدی'}</span><strong>{labelData().id || '-'}</strong></div>

      <div className="salon-layout">

        <div className="form-grid salon-form">

          <Input label="شماره ماشین" value={form.machine} onChange={loadMachineDefaults} />

          <Select label="نام کالا" value={form.kala_id} onChange={v => set('kala_id', Number(v))} items={lookups.fabrics} />

          <Input label="متراژ" type="number" value={form.metr} onChange={v => set('metr', Number(v))} />

          <Input label="وزن" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />

          <Input label="همبافت پود" value={form.ham_pod} onChange={() => {}} disabled hint="از آخرین ورود نخ سالن برای همین ماشین پر می‌شود و قابل ویرایش دستی نیست." />

          <Select label="شماره چله" value={form.shom_chelle} onChange={v => {

            set('shom_chelle', v);

            const row = recent.find(x => x.shom_chelle === v);

            if (row) set('ham_chelle', row.hambaft || '');

          }} items={recent.map(x => ({ id: x.shom_chelle, name: `${x.shom_chelle} - ${x.hambaft || 'بدون همبافت'} - ${fmt(x.weight)} کیلو` }))} />

          <Input label="همبافت تار / چله" value={form.ham_chelle} onChange={v => set('ham_chelle', v)} />

          <Input label="کاربر" value={form.user} onChange={v => set('user', v)} />

          <label className="check-line"><input type="checkbox" checked={!!form.skip_print} onChange={e => set('skip_print', e.target.checked)} /> <span>بعد از ثبت لیبل چاپ نشود</span></label>

        </div>

        <LabelPreview data={labelData()} />

      </div>

      <div className="actions-row">

        <button className="primary" onClick={save}>{editing ? 'ثبت ویرایش طاقه' : 'ثبت طاقه'}</button>

        <button onClick={() => printLabel(labelData())}>چاپ لیبل و بارکد</button>

        {editing && <button className="ghost" onClick={() => { setEditing(false); setForm(salonEmpty()); }}>لغو ویرایش</button>}

      </div>

      <div className="hint">با وارد کردن شماره ماشین، آخرین کالا، همبافت پود، همبافت تار/چله و چله‌های آخر همان ماشین به صورت خودکار جایگذاری می‌شود.</div>

    </section>

    <section className="panel">

      <h2>لیست سالن تولید</h2>

      <Filters filters={filterDefs} rows={items} values={filters} setValues={setFilters} onPrint={() => printReport('لیست سالن تولید', visible, [['tarikh','تاریخ'],['id','کد طاقه'],['machine','ماشین'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['shom_chelle','چله'],['ham_chelle','همبافت تار/چله'],['ham_pod','همبافت پود']])} />

      {loading ? <div className="empty">در حال بارگذاری...</div> : <Table rows={visible} columns={[['tarikh','تاریخ'],['id','کد طاقه'],['machine','ماشین'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['shom_chelle','چله'],['ham_chelle','همبافت تار/چله'],['ham_pod','همبافت پود']]} onEdit={edit} onDelete={del} />}

    </section>

  </Page>;

}



function Consumption() {

  const [rows, setRows] = useState([]);

  useEffect(() => { api('/consumption/machines').then(setRows).catch(() => setRows([])); }, []);

  const cols = [['machine','شماره ماشین'],['shom_chelle','شماره چله'],['tar_used','مصرف تار (kg)'],['pod_used','مصرف پود (kg)'],['total_weight','کل تولید (kg)'],['remaining','مانده نخ (kg)'],['tarikh','آخرین بروزرسانی']];

  return <Page title="وضعیت مصرف تار و پود ماشین‌ها">

    <section className="panel green-head">

      <div className="panel-title-row"><h2>وضعیت مصرف تار و پود ماشین‌ها</h2><button onClick={() => api('/consumption/machines').then(setRows)}>بروزرسانی</button></div>

      <Table rows={rows} columns={cols} onEdit={() => {}} onDelete={() => {}} hideActions />

    </section>

  </Page>;

}



function Reports() {

  return <Page title="گزارشات عملیاتی">

      <section className="panel"><h2>گزارشات قابل چاپ</h2><p>در هر تب اصلی، فیلتر و دکمه چاپ گزارش فعال است و این بخش برای گزارشات ترکیبی و مدیریتی هم در همین ساختار توسعه داده می‌شود.</p></section>

  </Page>;

}



function YarnOut({ lookups, notify }) {

  const [yarnInRows, setYarnInRows] = useState([]);
  const [warperBalanceRows, setWarperBalanceRows] = useState([]);

  const loadWarperBalances = () => api('/warper-yarn-balance').then(setWarperBalanceRows).catch(() => setWarperBalanceRows([]));

  useEffect(() => {
    api('/nakh-vor').then(setYarnInRows).catch(() => setYarnInRows([]));
    loadWarperBalances();
  }, []);

  return <CrudPage

    title="خروج نخ"

    endpoint="/nakh-khor"

    empty={{ hambaft: '', weight: '', mosh_name: '', nakh_name: '' }}

    notify={notify}
    afterSave={loadWarperBalances}

    filters={[['mosh','مشتری'],['nakh','نخ'],['hambaft','همبافت']]}

    mapEdit={row => ({ id: row.id, hambaft: row.hambaft || '', weight: Math.abs(Number(row.weight || 0)), mosh_name: row.mosh || '', nakh_name: row.nakh || '' })}

    renderForm={(form, set) => {

      const relatedRows = yarnInRows.filter(r => (!form.nakh_name || r.nakh === form.nakh_name) && (!form.hambaft || r.hambaft === form.hambaft));

      const hambaftOptions = [...new Set(yarnInRows.filter(r => !form.nakh_name || r.nakh === form.nakh_name).map(r => r.hambaft).filter(Boolean))];

      const yarnOptions = [...new Set(yarnInRows.filter(r => !form.hambaft || r.hambaft === form.hambaft).map(r => r.nakh).filter(Boolean))];

      const recipientOptions = uniqueText([
        ...(lookups.customers || []).map(x => x.name),
        ...(lookups.warpers || []).map(x => x.name),
      ]);

      const setHambaft = v => {

        set('hambaft', v);

        const yarns = [...new Set(yarnInRows.filter(r => r.hambaft === v).map(r => r.nakh).filter(Boolean))];

        if (yarns.length === 1) set('nakh_name', yarns[0]);

      };

      const setYarn = v => {

        set('nakh_name', v);

        const hambafts = [...new Set(yarnInRows.filter(r => r.nakh === v).map(r => r.hambaft).filter(Boolean))];

        if (hambafts.length === 1) set('hambaft', hambafts[0]);

      };

      return <>

      <Input label="همبافت نخ" value={form.hambaft} onChange={setHambaft} list={hambaftOptions.length ? hambaftOptions : lookups.hambaftYarn} />

      <Input label="وزن خروج" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />

      <Input label="مشتری / چله‌پیچ" value={form.mosh_name} onChange={v => set('mosh_name', v)} list={recipientOptions} />

      <Input label="نوع نخ" value={form.nakh_name} onChange={setYarn} list={yarnOptions.length ? yarnOptions : (lookups.yarns || []).map(x => x.name)} />

      {(form.hambaft || form.nakh_name) && <div className="form-help">گزینه‌های نخ و همبافت بر اساس ورود نخ‌های ثبت‌شده فیلتر می‌شوند. موارد مرتبط: {relatedRows.length}</div>}

    </>;

    }}

    columns={[['tarikh','تاریخ'],['hambaft','همبافت'],['weight','وزن'],['mosh','مشتری'],['nakh','نخ']]}

    extraSections={<section className="panel">
      <div className="panel-title-row"><h2>گزارش مانده نخ نزد چله‌پیچ</h2><button onClick={loadWarperBalances}>بروزرسانی</button></div>
      <p className="hint">این گزارش وزن نخ ارسال‌شده به چله‌پیچی را با چله‌های برگشتی همان چله‌پیچ، همبافت و نوع نخ مقایسه می‌کند.</p>
      <Table rows={warperBalanceRows} columns={[
        ['warper','چله‌پیچ'],
        ['hambaft','همبافت'],
        ['yarn','نوع نخ'],
        ['sent_weight','ارسال به چله‌پیچی kg'],
        ['returned_weight','ورود چله kg'],
        ['balance_weight','مانده نزد چله‌پیچ kg'],
        ['chelle_count','تعداد چله ورودی'],
        ['last_sent_date','آخرین خروج'],
        ['last_return_date','آخرین ورود چله'],
      ]} hideActions />
    </section>}

  />;

}

function EmptyBeamOut({ lookups, notify }) {
  const beamNames = (lookups.beams || []).map(x => x.name).filter(Boolean);

  return <CrudPage
    title="خروج نورد خالی"
    endpoint="/empty-beam-out"
    empty={{ beam_id: '', warper_id: '', description: '' }}
    notify={notify}
    filters={[['beam','نورد'],['warper','چله‌پیچ'],['status','وضعیت']]}
    mapEdit={row => ({
      id: row.id,
      beam_id: row.beam_id || '',
      warper_id: row.warper_id || '',
      beam: row.beam || '',
      warper: row.warper || '',
      description: row.description || '',
    })}
    renderForm={(form, set) => <>
      <Select label="شماره نورد" value={form.beam_id} onChange={v => set('beam_id', Number(v))} items={lookups.beams} />
      <Select label="خروج جهت / چله‌پیچ" value={form.warper_id} onChange={v => set('warper_id', Number(v))} items={lookups.warpers} />
      <Input label="توضیحات" value={form.description} onChange={v => set('description', v)} hint="این ثبت فقط آماری است و اثر مالی ندارد." />
    </>}
    columns={[
      ['tarikh','تاریخ خروج'],
      ['beam','شماره نورد'],
      ['warper','چله‌پیچ'],
      ['status','وضعیت'],
      ['return_chelle','چله برگشتی'],
      ['return_date','تاریخ برگشت'],
      ['description','توضیحات'],
    ]}
    extraSections={({ items, load }) => {
      const active = (items || []).filter(x => x.status !== 'برگشته');
      const activeBeamSet = new Set(active.map(x => x.beam).filter(Boolean));
      const returned = (items || []).filter(x => x.status === 'برگشته');
      const inventoryRows = beamNames
        .filter(name => !activeBeamSet.has(name))
        .map((name, index) => ({ id: `inv-${index}`, beam: name, status: 'داخل انبار' }));
      const byWarper = Object.values(active.reduce((acc, row) => {
        const key = row.warper || 'نامشخص';
        if (!acc[key]) acc[key] = { id: key, warper: key, count: 0, beams: [] };
        acc[key].count += 1;
        if (row.beam) acc[key].beams.push(row.beam);
        return acc;
      }, {})).map(row => ({ ...row, beams: row.beams.join('، ') }));

      return <>
        <section className="panel">
          <div className="panel-title-row"><h2>خلاصه کنترل نوردهای خالی</h2><button onClick={load}>بروزرسانی</button></div>
          <div className="stats-grid">
            <div><span>داخل انبار</span><b>{inventoryRows.length}</b></div>
            <div><span>نزد چله‌پیچ</span><b>{active.length}</b></div>
            <div><span>برگشت‌خورده</span><b>{returned.length}</b></div>
          </div>
          <p className="hint">برگشت نورد به صورت خودکار از روی ورود چله با همان شماره نورد و همان چله‌پیچ تشخیص داده می‌شود.</p>
        </section>
        <section className="panel">
          <h2>نوردهای داخل انبار</h2>
          <Table rows={inventoryRows} columns={[['beam','شماره نورد'],['status','وضعیت']]} hideActions />
        </section>
        <section className="panel">
          <h2>مانده نورد نزد چله‌پیچ‌ها</h2>
          <Table rows={byWarper} columns={[['warper','چله‌پیچ'],['count','تعداد'],['beams','شماره نوردها']]} hideActions />
        </section>
      </>;
    }}
  />;
}



function Expenses({ lookups, notify }) {

  return <CrudPage

    title="هزینه‌ها"

    endpoint="/expenses"

    empty={{ hazine_id: '', operator_id: '', weaver_id: '', mablagh: '', description: '', sanad_no: '' }}

    notify={notify}

    filters={[['onvan_hazine','عنوان هزینه'],['operator_name','ثبت کننده'],['weaver_name','بافنده'],['shomare_sanad','شماره سند']]}

    mapEdit={row => ({ id: row.id, hazine_id: row.hazine_id || '', operator_id: row.operator_id || '', weaver_id: row.weaver_id || '', mablagh: Number(row.mablagh || 0), description: row.tozih || '', sanad_no: row.shomare_sanad || '' })}

    renderForm={(form, set) => <>

      <Select label="عنوان هزینه" value={form.hazine_id} onChange={v => set('hazine_id', Number(v))} items={lookups.costs} />

      <Select label="ثبت کننده" value={form.operator_id} onChange={v => set('operator_id', Number(v))} items={lookups.operators} />

      <Select label="بافنده / مرتبط با" value={form.weaver_id} onChange={v => set('weaver_id', Number(v))} items={lookups.weavers} />

      <Input label="مبلغ" type="number" value={form.mablagh} onChange={v => set('mablagh', Number(v))} />

      <Input label="شماره سند" value={form.sanad_no} onChange={v => set('sanad_no', v)} />

      <Input label="توضیحات" value={form.description} onChange={v => set('description', v)} />

    </>}

    columns={[['tarikh','تاریخ'],['onvan_hazine','عنوان هزینه'],['operator_name','ثبت کننده'],['weaver_name','بافنده'],['mablagh','مبلغ'],['shomare_sanad','شماره سند'],['tozih','توضیحات']]}

  />;

}



function OutInvoice({ lookups, notify }) {

  const [rows, setRows] = useState([]);

  const [filters, setFilters] = useState({});

  const [form, setForm] = useState({ invoice_no: '', sanad_no: '', customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' });

  const [taghe, setTaghe] = useState(null);

  const [editing, setEditing] = useState(false);

  const load = async () => setRows(await api('/out-invoice'));

  useEffect(() => { load(); api('/out-invoice/next-sanad').then(x => setForm(s => ({ ...s, sanad_no: x.sanad_number || '' }))).catch(() => {}); }, []);

  const set = (k, v) => setForm(s => ({ ...s, [k]: v }));

  const findTaghe = async () => {

    if (!form.taghe_code) return;

    const data = await api(`/out-invoice/taghe/${encodeURIComponent(form.taghe_code)}`);

    setTaghe(data);

    setForm(s => ({ ...s, kala: s.kala || data.kala || '', items: s.items.some(x => String(x.id) === String(data.id)) ? s.items : [...s.items, data], taghe_code: '' }));

  };

  const removeItem = (row) => setForm(s => ({ ...s, items: s.items.filter(x => String(x.id) !== String(row.id || row)) }));

  const edit = async (row) => {

    const detail = await api(`/out-invoice/details/${encodeURIComponent(row.invoice_no)}`);

    setForm({ invoice_no: row.invoice_no || '', sanad_no: row.sanad || '', customer: row.mosh || '', kala: row.kala || '', taghe_code: '', items: detail.items || [], old_invoice_no: row.invoice_no || '' });

    setEditing(true);

    window.scrollTo({ top: 0, behavior: 'smooth' });

  };

  const del = async (row) => {

    const invoiceNo = typeof row === 'object' ? (row.invoice_no || row.id) : row;

    await api(`/out-invoice/${encodeURIComponent(invoiceNo)}`, { method: 'DELETE' });

    await load();

    notify('فاکتور خروج حذف شد');

  };

  const save = async () => {

    await api('/out-invoice', { method: 'POST', body: { invoice_no: form.invoice_no, sanad_no: form.sanad_no, customer: form.customer, kala: form.kala, items: form.items.map(x => String(x.id)), old_invoice_no: form.old_invoice_no } });

    setForm({ invoice_no: '', sanad_no: '', customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' });

    setTaghe(null);

    setEditing(false);

    await load();

    notify(editing ? 'فاکتور خروج ویرایش شد' : 'فاکتور خروج ثبت شد');

  };

  const visible = filterRows(rows, filters);

  const invoiceCols = [['tarikh','تاریخ'],['invoice_no','شماره فاکتور'],['sanad','شماره سند'],['mosh','مشتری'],['kala','کالا'],['taghe_count','طاقه'],['metr','متراژ'],['weight','وزن'],['codes','کد طاقه‌ها']];

  return <Page title="فاکتور خروج">

    <section className="panel">

      <h2>{editing ? 'ویرایش فاکتور خروج' : 'ثبت فاکتور خروج'}</h2>

      <div className="form-grid">

        <Input label="شماره فاکتور" value={form.invoice_no} onChange={v => set('invoice_no', v)} />

        <Input label="شماره سند" value={form.sanad_no} onChange={v => set('sanad_no', v)} />

        <Input label="مشتری" value={form.customer} onChange={v => set('customer', v)} list={(lookups.customers || []).map(x => x.name)} />

        <Input label="کالا" value={form.kala} onChange={v => set('kala', v)} list={(lookups.fabrics || []).map(x => x.name)} />

        <Input label="کد طاقه" value={form.taghe_code} onChange={v => set('taghe_code', v)} />

        <label><span>&nbsp;</span><button type="button" onClick={findTaghe}>افزودن طاقه</button></label>

      </div>

      {taghe && <div className="taghe-info"><b>آخرین طاقه افزوده شده: {taghe.id}</b><span>کالا: {taghe.kala || '-'}</span><span>متراژ: {fmt(taghe.metr)}</span><span>وزن: {fmt(taghe.weight)}</span></div>}

      <Table rows={form.items} columns={[['id','کد طاقه'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['machine','ماشین'],['shom_chelle','چله']]} onEdit={() => {}} onDelete={removeItem} />

      <div className="summary-row">

        <div><span>تعداد طاقه</span><b>{fmt(form.items.length)}</b></div>

        <div><span>جمع متراژ</span><b>{fmt(form.items.reduce((s, x) => s + Number(x.metr || 0), 0))}</b></div>

        <div><span>جمع وزن</span><b>{fmt(form.items.reduce((s, x) => s + Number(x.weight || 0), 0))}</b></div>

      </div>

      <div className="actions-row"><button className="primary" onClick={save}>{editing ? 'ثبت ویرایش فاکتور خروج' : 'ثبت فاکتور خروج'}</button>{editing && <button className="ghost" onClick={() => { setEditing(false); setForm({ invoice_no: '', sanad_no: '', customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' }); }}>لغو ویرایش</button>}</div>

    </section>

    <section className="panel">

      <h2>لیست فاکتورهای خروج</h2>

      <Filters filters={[['invoice_no','شماره فاکتور'],['mosh','مشتری'],['kala','کالا'],['sanad','شماره سند']]} values={filters} setValues={setFilters} onPrint={() => printReport('لیست فاکتورهای خروج', visible, invoiceCols)} />

      <Table rows={visible} columns={invoiceCols} onEdit={edit} onDelete={del} />

    </section>

  </Page>;

}



function ReportsPro() {

  const [data, setData] = useState({ yarnOut: [], invoices: [], expenses: [] });

  const [filters, setFilters] = useState({});

  useEffect(() => {

    Promise.all([api('/nakh-khor'), api('/out-invoice'), api('/expenses')]).then(([yarnOut, invoices, expenses]) => setData({ yarnOut, invoices, expenses })).catch(() => {});

  }, []);

  const yarnOut = filterRows(data.yarnOut, filters);

  const invoices = filterRows(data.invoices, filters);

  const expenses = filterRows(data.expenses, filters);

  return <Page title="گزارشات عملیاتی">

    <section className="panel">

      <h2>فیلتر مشترک گزارشات</h2>

      <Filters filters={[['tarikh','تاریخ'],['mosh','مشتری'],['kala','کالا'],['hambaft','همبافت'],['onvan_hazine','هزینه']]} values={filters} setValues={setFilters} onPrint={() => printReport('گزارش ترکیبی عملیاتی', [...invoices, ...yarnOut, ...expenses], [['tarikh','تاریخ'],['invoice_no','فاکتور'],['mosh','مشتری'],['kala','کالا'],['hambaft','همبافت'],['weight','وزن'],['mablagh','مبلغ'],['onvan_hazine','عنوان هزینه']])} />

    </section>

    <section className="panel"><h2>گزارش فاکتور خروج</h2><Table rows={invoices} columns={[['tarikh','تاریخ'],['invoice_no','شماره فاکتور'],['sanad','شماره سند'],['mosh','مشتری'],['kala','کالا'],['taghe_count','طاقه'],['metr','متراژ'],['weight','وزن'],['codes','کد طاقه‌ها']]} hideActions /></section>

    <section className="panel"><h2>گزارش خروج نخ</h2><Table rows={yarnOut} columns={[['tarikh','تاریخ'],['hambaft','همبافت'],['weight','وزن'],['mosh','مشتری'],['nakh','نخ']]} hideActions /></section>

    <section className="panel"><h2>گزارش هزینه‌ها</h2><Table rows={expenses} columns={[['tarikh','تاریخ'],['onvan_hazine','عنوان هزینه'],['operator_name','ثبت کننده'],['weaver_name','بافنده'],['mablagh','مبلغ'],['shomare_sanad','شماره سند'],['tozih','توضیحات']]} hideActions /></section>

  </Page>;

}



function ComingSoon({ title }) {

  return <Page title={title}>

    <section className="panel"><h2>{title}</h2><p>این تب برای توسعه‌ی منطق‌های بعدی در همین ساختار Go/React آماده است و به‌صورت مرحله‌ای تکمیل می‌شود.</p></section>

  </Page>;

}



function ConsumptionPro() {

  const [rows, setRows] = useState([]);

  const load = () => api('/consumption/machines').then(setRows).catch(() => setRows([]));

  useEffect(() => { load(); }, []);

  const activeRows = rows.map(r => ({ machine: r.machine, shom_chelle: r.shom_chelle, chelle_weight: r.chelle_weight, pod_assigned: r.pod_assigned, tar_used: r.tar_used, pod_used: r.pod_used, total_weight: r.total_weight, remaining: r.remaining, remaining_percent: r.remaining_percent, tarikh: r.tarikh }));

  const wasteRows = rows.map(r => ({ machine: r.machine, shom_chelle: r.shom_chelle, total_meter: r.total_meter, total_weight: r.total_weight, waste_weight: r.waste_weight, waste_per_meter: r.waste_per_meter, waste_per_kg: r.waste_per_kg, waste_percent_per_kg: r.waste_percent_per_kg, tarikh: r.tarikh }));

  return <Page title="مصرف تار و پود و ضایعات">

    <section className="panel green-head">

      <div className="panel-title-row"><h2>آخرین چله فعال هر ماشین</h2><button onClick={load}>بروزرسانی</button></div>

      <Table rows={activeRows} columns={[

        ['machine','ماشین'],

        ['shom_chelle','شماره چله'],

        ['chelle_weight','وزن چله kg'],

        ['pod_assigned','پود اختصاصی kg'],

        ['tar_used','مصرف تار kg'],

        ['pod_used','مصرف پود kg'],

        ['total_weight','وزن پارچه kg'],

        ['remaining','مانده نخ kg'],

        ['remaining_percent','درصد مانده'],

        ['tarikh','آخرین بروزرسانی']

      ]} hideActions />

    </section>

    <section className="panel">

      <div className="panel-title-row"><h2>فرم محاسبه ضایعات چله</h2><button onClick={load}>بروزرسانی</button></div>

      <Table rows={wasteRows} columns={[

        ['machine','ماشین'],

        ['shom_chelle','شماره چله'],

        ['total_meter','متراژ تولید'],

        ['total_weight','وزن پارچه kg'],

        ['waste_weight','وزن ضایعات/مانده kg'],

        ['waste_per_meter','ضایعات kg/m'],

        ['waste_per_kg','ضایعات kg/kg'],

        ['waste_percent_per_kg','درصد ضایعات وزنی'],

        ['tarikh','آخرین بروزرسانی']

      ]} hideActions />

    </section>

  </Page>;

}

function MachineFormulas({ notify }) {

  return <CrudPage

    title="فرمول تولید ماشین‌ها"

    endpoint="/formulas"

    empty={{ machine: '', tar_percent: 50, pod_percent: 50, tozih: '' }}

    notify={notify}

    filters={[['machine','ماشین']]}

    mapEdit={row => ({ id: row.id, machine: row.machine || '', tar_percent: Number(row.tar_percent || 0), pod_percent: Number(row.pod_percent || 0), tozih: row.tozih || '' })}

    renderForm={(form, set) => <>

      <Input label="شماره ماشین" value={form.machine} onChange={v => set('machine', v)} />

      <Input label="درصد مصرف تار" type="number" value={form.tar_percent} onChange={v => set('tar_percent', Number(v))} />

      <Input label="درصد مصرف پود" type="number" value={form.pod_percent} onChange={v => set('pod_percent', Number(v))} />

      <Input label="توضیحات" value={form.tozih} onChange={v => set('tozih', v)} />

    </>}

    columns={[['machine','ماشین'],['tar_percent','درصد تار'],['pod_percent','درصد پود'],['tozih','توضیحات']]}

  />;

}



function MobileBarcodeScanner({ onCode, onClose }) {

  const videoRef = React.useRef(null);

  const streamRef = React.useRef(null);

  const imageInputRef = React.useRef(null);

  const [message, setMessage] = useState('در حال فعال کردن دوربین...');

  const decodeImage = async event => {

    const file = event.target.files?.[0];

    event.target.value = '';

    if (!file) return;

    const imageURL = URL.createObjectURL(file);

    try {

      setMessage('در حال خواندن بارکد از تصویر...');

      const image = new Image();

      image.src = imageURL;

      await image.decode();

      const result = await new BrowserMultiFormatReader().decodeFromImageElement(image);

      const code = String(result?.getText?.() || '').trim();

      if (!code) throw new Error('بارکدی در تصویر پیدا نشد. عکس واضح‌تر و نزدیک‌تر بگیرید.');

      streamRef.current?.getTracks().forEach(track => track.stop());

      onCode(code);

    } catch (err) {

      setMessage(err.message || 'بارکدی در تصویر پیدا نشد. دوباره عکس بگیرید.');

    } finally {

      URL.revokeObjectURL(imageURL);

    }

  };

  useEffect(() => {

    let active = true;

    let timer = 0;

    const stop = () => {

      window.clearTimeout(timer);

      streamRef.current?.getTracks().forEach(track => track.stop());

      streamRef.current = null;

    };

    const start = async () => {

      try {

        if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia) {

          throw new Error('برای شبکه محلی از دکمه «گرفتن عکس بارکد» استفاده کنید. اسکن زنده فقط روی HTTPS یا localhost فعال است.');

        }

        if (!window.BarcodeDetector) {

          throw new Error('اسکن زنده در این مرورگر فعال نیست؛ از دکمه «گرفتن عکس بارکد» استفاده کنید.');

        }

        const detector = new window.BarcodeDetector({ formats: ['code_128', 'code_39', 'ean_13', 'ean_8', 'qr_code'] });

        const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: 'environment' } }, audio: false });

        if (!active) {

          stream.getTracks().forEach(track => track.stop());

          return;

        }

        streamRef.current = stream;

        videoRef.current.srcObject = stream;

        await videoRef.current.play();

        setMessage('بارکد طاقه را داخل کادر قرار دهید.');

        const scan = async () => {

          if (!active) return;

          try {

            const results = await detector.detect(videoRef.current);

            const code = String(results?.[0]?.rawValue || '').trim();

            if (code) {

              stop();

              onCode(code);

              return;

            }

          } catch {

          }

          timer = window.setTimeout(scan, 240);

        };

        scan();

      } catch (err) {

        setMessage(err.message || 'دوربین در دسترس نیست.');

      }

    };

    start();

    return () => {

      active = false;

      stop();

    };

  }, []);

  return <div className="mobile-scanner" role="dialog" aria-modal="true" aria-label="اسکن بارکد طاقه">

    <div className="mobile-scanner-card">

      <div className="mobile-scanner-head"><strong>بارگیری با موبایل</strong><button type="button" className="ghost" onClick={onClose}>بستن</button></div>

      <div className="mobile-scanner-frame"><video ref={videoRef} playsInline muted /></div>

      <p>{message}</p>

      <input ref={imageInputRef} className="mobile-scanner-file" type="file" accept="image/*" capture="environment" onChange={decodeImage} />

      <button type="button" className="primary mobile-scanner-photo" onClick={() => imageInputRef.current?.click()}>گرفتن عکس بارکد / انتخاب تصویر</button>

    </div>

  </div>;

}

function MobileLoadingDialog({ session, onClose }) {

  return <div className="mobile-loading-dialog" role="dialog" aria-modal="true" aria-label="اتصال بارگیری موبایل">

    <div className="mobile-loading-dialog-card">

      <div className="mobile-scanner-head"><strong>بارگیری با موبایل</strong><button type="button" className="ghost" onClick={onClose}>بستن اتصال</button></div>

      <p className="mobile-loading-help">این QR را با دوربین گوشی اسکن کنید. پنل بارگیری روی گوشی باز می‌شود و طاقه‌های ثبت‌شده به‌صورت خودکار به همین فاکتور اضافه می‌شوند.</p>

      <div className="mobile-loading-qr"><img src={session.qrImage} alt="QR اتصال بارگیری موبایل" /></div>

      <div className="mobile-loading-status"><span className="connection-dot" /> اتصال فعال است</div>

      <div className="mobile-loading-stats"><b>{fmt(session.count || 0)}</b><span>طاقه از موبایل دریافت شد</span></div>

      <div className="mobile-loading-url" dir="ltr">{session.url}</div>

      <button type="button" onClick={() => navigator.clipboard?.writeText(session.url)}>کپی لینک برای ارسال به گوشی</button>

      <small>اعتبار اتصال تا {new Date(session.expires_at).toLocaleTimeString('fa-IR')} است.</small>

    </div>

  </div>;

}

function MobileLoadingPage() {

  const token = new URLSearchParams(window.location.search).get('token') || '';

  const [session, setSession] = useState(null);

  const [code, setCode] = useState('');

  const [message, setMessage] = useState('در حال اتصال به فاکتور...');

  const [error, setError] = useState('');

  const [scannerOpen, setScannerOpen] = useState(false);

  const [pendingItem, setPendingItem] = useState(null);

  const [busy, setBusy] = useState(false);

  const refresh = async () => {

    if (!token) throw new Error('کد اتصال در لینک وجود ندارد. QR را دوباره اسکن کنید.');

    const data = await api(`/mobile-loading/${encodeURIComponent(token)}`);

    setSession(data);

    setMessage('اتصال برقرار است. بارکد طاقه بعدی را اسکن کنید.');

    return data;

  };

  useEffect(() => {

    let active = true;

    const load = async () => {

      try {

        const data = await api(`/mobile-loading/${encodeURIComponent(token)}`);

        if (active) {

          setSession(data);

          setMessage(current => current === 'در حال اتصال به فاکتور...' ? 'اتصال برقرار است. بارکد طاقه بعدی را اسکن کنید.' : current);

        }

      } catch (err) {

        if (active) setError(err.message);

      }

    };

    load();

    const timer = window.setInterval(load, 2500);

    return () => { active = false; window.clearInterval(timer); };

  }, [token]);

  const previewCode = async rawCode => {

    const value = String(rawCode || code || '').trim();

    if (!value) return;

    setError('');

    setPendingItem(null);

    setBusy(true);

    setMessage(`در حال دریافت اطلاعات طاقه ${value}...`);

    try {

      const result = await api(`/mobile-loading/${encodeURIComponent(token)}/preview`, { method: 'POST', body: { code: value } });

      setCode(value);

      setPendingItem(result.item);

      setMessage('اطلاعات طاقه را بررسی کنید؛ در صورت صحت، تأیید و ثبت را بزنید.');

      if (navigator.vibrate) navigator.vibrate(60);

    } catch (err) {

      setError(err.message);

      setMessage('اطلاعات طاقه دریافت نشد. کد را بررسی و دوباره تلاش کنید.');

      if (navigator.vibrate) navigator.vibrate([100, 80, 100]);

    } finally {

      setBusy(false);

    }

  };

  const confirmItem = async () => {

    if (!pendingItem || busy) return;

    const value = String(pendingItem.id);

    setError('');

    setBusy(true);

    setMessage(`در حال ثبت نهایی طاقه ${value}...`);

    try {

      await api(`/mobile-loading/${encodeURIComponent(token)}/items`, { method: 'POST', body: { code: value } });

      setPendingItem(null);

      setCode('');

      const data = await refresh();

      setMessage(`طاقه ${value} با تأیید شما ثبت شد. تعداد کل: ${fmt(data.count)}`);

      if (navigator.vibrate) navigator.vibrate(120);

    } catch (err) {

      setError(err.message);

      setMessage('ثبت نهایی انجام نشد. اطلاعات را بررسی و دوباره تلاش کنید.');

      if (navigator.vibrate) navigator.vibrate([100, 80, 100]);

    } finally {

      setBusy(false);

    }

  };

  const cancelPreview = () => {

    setPendingItem(null);

    setCode('');

    setError('');

    setMessage('ثبت لغو شد. بارکد طاقه بعدی را اسکن کنید.');

  };

  return <main className="mobile-loading-page" dir="rtl">

    <header><span>Textile ERP</span><h1>پنل بارگیری فاکتور خروج</h1><p>این صفحه فقط برای بارگیری موقت همین فاکتور فعال است.</p></header>

    <section className="mobile-loading-summary">

      <div><span>شماره فاکتور</span><b>{session?.invoice_no || 'ثبت نشده'}</b></div>

      <div><span>مشتری</span><b>{session?.customer || 'ثبت نشده'}</b></div>

      <div><span>تعداد طاقه</span><b>{fmt(session?.count || 0)}</b></div>

      <div><span>جمع متراژ</span><b>{fmt(session?.total_metr || 0)}</b></div>

    </section>

    <section className="mobile-loading-action">

      <button type="button" className="primary mobile-camera-button" disabled={!session || busy || pendingItem} onClick={() => setScannerOpen(true)}>اسکن بارکد با دوربین</button>

      <div className="mobile-code-entry"><input inputMode="numeric" autoComplete="off" placeholder="کد طاقه" value={code} disabled={busy || pendingItem} onChange={event => setCode(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') previewCode(); }} /><button type="button" disabled={busy || pendingItem} onClick={() => previewCode()}>{busy && !pendingItem ? 'در حال بررسی...' : 'نمایش اطلاعات'}</button></div>

      <p className="mobile-loading-message">{message}</p>

      {error && <p className="mobile-loading-error">{error}</p>}

      {pendingItem && <div className="mobile-taghe-confirmation">

        <div className="mobile-taghe-confirmation-head"><span>اطلاعات طاقه برای تأیید</span><b>کد {pendingItem.id}</b></div>

        <dl>

          <div><dt>کالا</dt><dd>{pendingItem.kala || '-'}</dd></div>

          <div><dt>ماشین</dt><dd>{pendingItem.machine || '-'}</dd></div>

          <div><dt>متراژ</dt><dd>{fmt(pendingItem.metr || 0)} متر</dd></div>

          <div><dt>وزن</dt><dd>{fmt(pendingItem.weight || 0)} کیلوگرم</dd></div>

          <div><dt>شماره چله</dt><dd>{pendingItem.shom_chelle || '-'}</dd></div>

          <div><dt>هم‌پود</dt><dd>{pendingItem.ham_pod || '-'}</dd></div>

          <div><dt>هم‌چله</dt><dd>{pendingItem.ham_chelle || '-'}</dd></div>

        </dl>

        <div className="mobile-taghe-confirmation-actions"><button type="button" className="primary" disabled={busy} onClick={confirmItem}>{busy ? 'در حال ثبت...' : 'تأیید و ثبت طاقه'}</button><button type="button" className="ghost" disabled={busy} onClick={cancelPreview}>لغو</button></div>

      </div>}

    </section>

    <section className="mobile-loaded-items">

      <h2>طاقه‌های بارگیری‌شده</h2>

      {(session?.items || []).length ? (session.items || []).slice().reverse().map(item => <article key={item.id}><b>{item.id}</b><span>{fmt(item.metr)} متر</span><span>{fmt(item.weight)} کیلوگرم</span><small>{item.kala || '-'}</small></article>) : <p>هنوز طاقه‌ای ثبت نشده است.</p>}

    </section>

    {scannerOpen && <MobileBarcodeScanner onClose={() => setScannerOpen(false)} onCode={value => { setScannerOpen(false); previewCode(value); }} />}

  </main>;

}



function OutInvoicePro({ lookups, notify }) {

  const inputRef = React.useRef(null);

  const [rows, setRows] = useState([]);

  const [filters, setFilters] = useState({});

  const [form, setForm] = useState({ invoice_no: '', sanad_no: '', customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' });

  const [editing, setEditing] = useState(false);

  const [mobileLoading, setMobileLoading] = useState(null);

  const [printers, setPrinters] = useState([]);

  const [reportPrinter, setReportPrinter] = useState(() => localStorage.getItem('operational-report-printer') || '');

  const [printerLoading, setPrinterLoading] = useState(false);

  const [saving, setSaving] = useState(false);

  const load = async () => setRows(await api('/out-invoice'));

  useEffect(() => { load(); api('/out-invoice/next-sanad').then(x => setForm(s => ({ ...s, sanad_no: x.sanad_number || '' }))).catch(() => {}); }, []);

  const loadPrinters = async () => {

    if (!LOCAL_MODE) return;

    setPrinterLoading(true);

    try {

      const data = await api('/local/printers');

      const available = data.printers || [];

      setPrinters(available);

      if (reportPrinter && !available.some(item => item.name === reportPrinter)) {

        setReportPrinter('');

        localStorage.removeItem('operational-report-printer');

      }

    } catch (err) {

      notify(err.message);

    } finally {

      setPrinterLoading(false);

    }

  };

  useEffect(() => { if (LOCAL_MODE) loadPrinters(); }, []);

  const selectReportPrinter = name => {

    setReportPrinter(name);

    if (name) localStorage.setItem('operational-report-printer', name);

    else localStorage.removeItem('operational-report-printer');

  };

  const invoiceValidationError = invoice => {

    const missing = [];

    if (!String(invoice.invoice_no || '').trim()) missing.push('شماره فاکتور');

    if (!String(invoice.customer || '').trim()) missing.push('مشتری');

    if (!String(invoice.kala || '').trim()) missing.push('نام کالا');

    if (!(invoice.items || []).length) missing.push('حداقل یک طاقه');

    return missing.length ? `اطلاعات فاکتور کامل نیست: ${missing.join('، ')}` : '';

  };

  const printOutgoingInvoice = async (invoice, existingWindow = null) => {

    const validationError = invoiceValidationError(invoice);

    if (validationError) {

      if (existingWindow) existingWindow.close();

      notify(validationError);

      return false;

    }

    if (printAfterSave && LOCAL_MODE && !reportPrinter) {

      if (existingWindow) existingWindow.close();

      notify('ابتدا چاپگر گزارش را انتخاب کنید تا فاکتور به لیبل‌پرینتر ارسال نشود');

      return false;

    }

    const printWindow = existingWindow || window.open('', '_blank', 'width=1100,height=800');

    if (!printWindow) {

      notify('مرورگر پنجره چاپ را مسدود کرده است');

      return false;

    }

    if (!existingWindow) {

      printWindow.document.write('<!doctype html><html lang="fa" dir="rtl"><meta charset="UTF-8"><body style="font-family:Tahoma;padding:30px">در حال آماده‌سازی فاکتور...</body></html>');

      printWindow.document.close();

    }

    let previousPrinter = '';

    try {

      if (LOCAL_MODE) {

        const result = await api('/local/printers', { method: 'POST', body: { name: reportPrinter } });

        previousPrinter = result.previous_printer || '';

      }

      printInvoiceTaghes(invoice, printWindow);

      if (LOCAL_MODE && previousPrinter && previousPrinter !== reportPrinter) {

        window.setTimeout(() => {

          api('/local/printers', { method: 'POST', body: { name: previousPrinter } }).catch(() => {});

        }, 20000);

      }

      return true;

    } catch (err) {

      printWindow.close();

      notify(`آماده‌سازی چاپ انجام نشد: ${err.message}`);

      return false;

    }

  };

  const printOutgoingReport = async (title, rowsOrLoader, columns) => {

    if (LOCAL_MODE && !reportPrinter) {

      notify('ابتدا چاپگر گزارش را انتخاب کنید تا چاپ به لیبل‌پرینتر ارسال نشود');

      return;

    }

    const printWindow = printAfterSave ? window.open('', '_blank', 'width=1100,height=800') : null;

    if (printAfterSave && !printWindow) {

      notify('مرورگر پنجره چاپ را مسدود کرده است');

      return;

    }

    printWindow.document.write('<!doctype html><html lang="fa" dir="rtl"><meta charset="UTF-8"><body style="font-family:Tahoma;padding:30px">در حال آماده‌سازی چاپ...</body></html>');

    printWindow.document.close();

    let previousPrinter = '';

    try {

      const reportRows = typeof rowsOrLoader === 'function' ? await rowsOrLoader() : rowsOrLoader;

      if (LOCAL_MODE) {

        const result = await api('/local/printers', { method: 'POST', body: { name: reportPrinter } });

        previousPrinter = result.previous_printer || '';

      }

      printReport(title, reportRows, columns, printWindow);

      notify(LOCAL_MODE ? `چاپگر گزارش آماده شد: ${reportPrinter}` : 'پنجره چاپ آماده شد');

      if (LOCAL_MODE && previousPrinter && previousPrinter !== reportPrinter) {

        window.setTimeout(() => {

          api('/local/printers', { method: 'POST', body: { name: previousPrinter } }).catch(() => {});

        }, 20000);

      }

    } catch (err) {

      printWindow.close();

      notify(err.message);

    }

  };

  const set = (k, v) => setForm(s => ({ ...s, [k]: v }));

  const addTaghe = async (scannedCode = '') => {

    const code = String(scannedCode || form.taghe_code || '').trim();

    if (!code) return;

    if (form.items.some(x => String(x.id) === code)) {

      notify('این طاقه در همین فاکتور اضافه شده است');

      setForm(s => ({ ...s, taghe_code: '' }));

      setTimeout(() => inputRef.current?.focus(), 30);

      return;

    }

    try {

      const data = await api(`/out-invoice/taghe/${encodeURIComponent(code)}`);

      setForm(s => ({ ...s, kala: s.kala || data.kala || '', taghe_code: '', items: s.items.some(x => String(x.id) === String(data.id)) ? s.items : [...s.items, data] }));

    } catch (err) {

      notify(err.message);

      setForm(s => ({ ...s, taghe_code: '' }));

    }

    setTimeout(() => inputRef.current?.focus(), 30);

  };

  const startMobileLoading = async () => {

    try {

      const data = await api('/out-invoice/mobile-sessions', { method: 'POST', body: { invoice_no: form.invoice_no, customer: form.customer, kala: form.kala } });

      const origin = String(window.ERP_LAN_ORIGIN || window.location.origin).replace(/\/$/, '');

      const url = `${origin}/operational/mobile-load?token=${encodeURIComponent(data.token)}`;

      const qrImage = await QRCode.toDataURL(url, { width: 380, margin: 2, errorCorrectionLevel: 'M' });

      setMobileLoading({ ...data, url, qrImage, count: 0 });

    } catch (err) {

      notify(err.message);

    }

  };

  useEffect(() => {

    if (!mobileLoading?.token) return undefined;

    let active = true;

    const poll = async () => {

      try {

        const data = await api(`/mobile-loading/${encodeURIComponent(mobileLoading.token)}`);

        if (!active) return;

        setMobileLoading(current => current ? { ...current, count: data.count || 0 } : current);

        setForm(current => {

          const known = new Set(current.items.map(item => String(item.id)));

          const received = (data.items || []).filter(item => !known.has(String(item.id)));

          if (!received.length) return current;

          return { ...current, kala: current.kala || data.kala || received[0]?.kala || '', items: [...current.items, ...received] };

        });

      } catch (err) {

        if (active && /منقضی|بسته/.test(err.message)) setMobileLoading(null);

      }

    };

    poll();

    const timer = window.setInterval(poll, 1200);

    return () => { active = false; window.clearInterval(timer); };

  }, [mobileLoading?.token]);

  const closeMobileLoading = async () => {

    const token = mobileLoading?.token;

    setMobileLoading(null);

    if (token) await api(`/mobile-loading/${encodeURIComponent(token)}`, { method: 'DELETE' }).catch(() => {});

  };

  const edit = async (row) => {

    const detail = await api(`/out-invoice/details/${encodeURIComponent(row.invoice_no)}`);

    setForm({ invoice_no: row.invoice_no || '', sanad_no: row.sanad || '', customer: row.mosh || '', kala: row.kala || '', taghe_code: '', items: detail.items || [], old_invoice_no: row.invoice_no || '' });

    setEditing(true);

    window.scrollTo({ top: 0, behavior: 'smooth' });

    setTimeout(() => inputRef.current?.focus(), 80);

  };

  const del = async (row) => {

    const invoiceNo = typeof row === 'object' ? (row.invoice_no || row.id) : row;

    await api(`/out-invoice/${encodeURIComponent(invoiceNo)}`, { method: 'DELETE' });

    await load();

    notify('فاکتور خروج حذف شد');

  };

  const removeItem = (row) => {

    const id = typeof row === 'object' ? row.id : row;

    setForm(s => ({ ...s, items: s.items.filter(x => String(x.id) !== String(id)) }));

  };

  const clearInvoice = () => {

    setForm({ invoice_no: '', sanad_no: form.sanad_no, customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' });

    setEditing(false);

    setTimeout(() => inputRef.current?.focus(), 30);

  };

  const save = async () => {

    if (saving) return;

    const snapshot = { ...form, items: form.items.map(item => ({ ...item })) };

    const validationError = invoiceValidationError(snapshot);

    if (validationError) {

      notify(validationError);

      return;

    }

    if (LOCAL_MODE && !reportPrinter) {

      notify('ابتدا چاپگر گزارش را انتخاب کنید؛ سپس فاکتور ذخیره و چاپ می‌شود');

      return;

    }

    const printWindow = window.open('', '_blank', 'width=1100,height=800');

    if (!printWindow) {

      notify('مرورگر پنجره چاپ را مسدود کرده است؛ اجازه Pop-up را فعال کنید');

      return;

    }

    if (printWindow) printWindow.document.write('<!doctype html><html lang="fa" dir="rtl"><meta charset="UTF-8"><body style="font-family:Tahoma;padding:30px">در حال ذخیره فاکتور و آماده‌سازی چاپ...</body></html>');

    if (printWindow) printWindow.document.close();

    const wasEditing = editing;

    setSaving(true);

    try {

      const saved = await api('/out-invoice', { method: 'POST', body: { invoice_no: snapshot.invoice_no, sanad_no: snapshot.sanad_no, customer: snapshot.customer, kala: snapshot.kala, items: snapshot.items.map(x => String(x.id)), old_invoice_no: snapshot.old_invoice_no } });

      const printed = printAfterSave ? await printOutgoingInvoice({ ...snapshot, tarikh: saved.tarikh || snapshot.tarikh }, printWindow) : false;

      const token = mobileLoading?.token;

      if (token) await api(`/mobile-loading/${encodeURIComponent(token)}`, { method: 'DELETE' }).catch(() => {});

      setMobileLoading(null);

      clearInvoice();

      const next = await api('/out-invoice/next-sanad').catch(() => null);

      if (next?.sanad_number) setForm(s => ({ ...s, sanad_no: next.sanad_number }));

      await load();

      notify(printAfterSave ? (printed ? (wasEditing ? 'فاکتور ویرایش، ذخیره و برای چاپ آماده شد' : 'فاکتور ذخیره و برای چاپ آماده شد') : 'فاکتور ذخیره شد؛ چاپ انجام نشد') : (wasEditing ? 'ویرایش فاکتور با موفقیت ذخیره شد' : 'فاکتور با موفقیت ذخیره شد'));

    } catch (err) {

      if (printWindow) printWindow.close();

      notify(`ذخیره فاکتور انجام نشد: ${err.message}`);

    } finally {

      setSaving(false);

    }

  };

  const visible = filterRows(rows, filters);

  const totalMetr = form.items.reduce((s, x) => s + Number(x.metr || 0), 0);

  const totalWeight = form.items.reduce((s, x) => s + Number(x.weight || 0), 0);

  const invoiceCols = [['sanad','شماره سند'],['invoice_no','شماره فاکتور'],['tarikh','تاریخ'],['mosh','مشتری'],['kala','نام کالا'],['taghe_count','تعداد طاقه'],['metr','مجموع متراژ (m)'],['weight','مجموع وزن (kg)']];

  return <Page title="فاکتور خروج">

    <section className="panel">

      <h2>{editing ? 'ویرایش فاکتور خروج' : 'ثبت فاکتور خروج'}</h2>

      <div className="form-grid">

        <Input label="شماره سند" value={form.sanad_no} onChange={v => set('sanad_no', v)} />

        <Input label="شماره فاکتور" value={form.invoice_no} onChange={v => set('invoice_no', v)} />

        <Input label="مشتری" value={form.customer} onChange={v => set('customer', v)} list={(lookups.customers || []).map(x => x.name)} />

        <Input label="نام کالا" value={form.kala} onChange={v => set('kala', v)} list={(lookups.fabrics || []).map(x => x.name)} />

        <label><span>کد طاقه / بارکد</span><input ref={inputRef} value={form.taghe_code} onChange={e => set('taghe_code', e.target.value)} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addTaghe(); } }} /></label>

        <label><span>&nbsp;</span><button type="button" onClick={() => addTaghe()}>افزودن طاقه</button></label>

        <label><span>بارگیری با موبایل</span><button type="button" className="mobile-load-button" onClick={startMobileLoading}>ساخت QR بارگیری با موبایل</button></label>

      </div>

      <section className="invoice-taghe-list">

        <div className="invoice-taghe-head"><h2>لیست طاقه‌های خروجی</h2></div>

        <Table rows={form.items.map((x, i) => ({ ...x, row: i + 1 }))} columns={[['row','ردیف'],['id','کد طاقه'],['metr','متراژ (m)'],['weight','وزن (kg)'],['ham_chelle','همبافت تار'],['ham_pod','همبافت پود'],['shom_chelle','شماره چله'],['kala','کالا']]} onEdit={() => {}} onDelete={removeItem} />

        <div className="invoice-total"><b>جمع کل:</b><span>{fmt(form.items.length)} عدد طاقه</span><span>{fmt(totalMetr)} متر</span><span>{fmt(totalWeight)} kg</span></div>

      </section>

      {LOCAL_MODE && <section className="report-printer-picker">

        <div><strong>چاپگر گزارش و فاکتور</strong><span>این انتخاب از چاپگر لیبل جداست و فقط برای چاپ‌های همین صفحه استفاده می‌شود.</span></div>

        <label><span>چاپگر معمولی</span><select value={reportPrinter} onChange={event => selectReportPrinter(event.target.value)}>

          <option value="">انتخاب چاپگر گزارش</option>

          {printers.map(printer => <option key={printer.name} value={printer.name}>{printer.name}{printer.is_default ? ' (پیش‌فرض فعلی)' : ''}</option>)}

        </select></label>

        <button type="button" className="ghost" disabled={printerLoading} onClick={loadPrinters}>{printerLoading ? 'در حال دریافت...' : 'به‌روزرسانی چاپگرها'}</button>

        <small>پنجره چاپ مرورگر نیز باز می‌شود؛ قبل از تأیید نهایی، نام همین چاپگر را کنترل کنید. چاپگر پیش‌فرض قبلی ویندوز پس از چاپ بازگردانده می‌شود.</small>

      </section>}

      <div className="actions-row invoice-actions">

        <button onClick={() => printOutgoingReport('موجودی طاقه‌های خروج‌نخورده', () => api('/out-invoice/stock'), [['id','کد طاقه'],['tarikh','تاریخ'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['machine','ماشین'],['shom_chelle','چله']])}>گزارش موجودی انبار</button>

        <button onClick={() => printOutgoingInvoice(form)}>چاپ لیست طاقه‌ها</button>

        <button onClick={() => printOutgoingReport('گزارش فاکتور خروج', visible, invoiceCols)}>گزارش فاکتورها</button>

        <button className="primary" disabled={saving} onClick={() => save(true)}>{saving ? 'در حال ذخیره...' : 'اتمام فاکتور، ذخیره و چاپ'}</button>

        <button className="ghost" disabled={saving} onClick={() => save(false)}>فقط ذخیره</button>

        <button className="ghost" onClick={clearInvoice}>پاک کردن فاکتور</button>

      </div>

      {mobileLoading && <MobileLoadingDialog session={mobileLoading} onClose={closeMobileLoading} />}

    </section>

    <section className="panel">

      <h2>فاکتورهای ثبت شده قبلی</h2>

      <Filters filters={[['invoice_no','شماره فاکتور'],['mosh','مشتری'],['kala','نام کالا'],['sanad','شماره سند']]} rows={rows} values={filters} setValues={setFilters} onPrint={() => printOutgoingReport('فاکتورهای ثبت شده قبلی', visible, invoiceCols)} />

      <Table rows={visible} columns={invoiceCols} onEdit={edit} onDelete={del} />

    </section>

  </Page>;

}



function ReportsPro2() {

  const [data, setData] = useState({ yarnOut: [], invoices: [], expenses: [] });

  const [filters, setFilters] = useState({});

  useEffect(() => {

    Promise.all([api('/nakh-khor'), api('/out-invoice'), api('/expenses')]).then(([yarnOut, invoices, expenses]) => setData({ yarnOut, invoices, expenses })).catch(() => {});

  }, []);

  const allRows = [...data.yarnOut, ...data.invoices, ...data.expenses];

  const yarnOut = filterRows(data.yarnOut, filters);

  const invoices = filterRows(data.invoices, filters);

  const expenses = filterRows(data.expenses, filters);

  return <Page title="گزارشات عملیاتی">

    <section className="panel">

      <h2>فیلتر مشترک گزارشات</h2>

      <Filters filters={[['tarikh','تاریخ'],['mosh','مشتری'],['kala','کالا'],['hambaft','همبافت'],['onvan_hazine','هزینه']]} rows={allRows} values={filters} setValues={setFilters} onPrint={() => printReport('گزارش ترکیبی عملیاتی', [...invoices, ...yarnOut, ...expenses], [['tarikh','تاریخ'],['invoice_no','فاکتور'],['mosh','مشتری'],['kala','کالا'],['hambaft','همبافت'],['weight','وزن'],['mablagh','مبلغ'],['onvan_hazine','عنوان هزینه']])} />

    </section>

    <section className="panel report-card"><h2>گزارش فاکتور خروج</h2><Table rows={invoices} columns={[['tarikh','تاریخ'],['invoice_no','شماره فاکتور'],['sanad','شماره سند'],['mosh','مشتری'],['kala','کالا'],['taghe_count','تعداد طاقه'],['metr','متراژ'],['weight','وزن']]} hideActions /></section>

    <section className="panel report-card"><h2>گزارش خروج نخ</h2><Table rows={yarnOut} columns={[['tarikh','تاریخ'],['hambaft','همبافت'],['weight','وزن'],['mosh','مشتری'],['nakh','نخ']]} hideActions /></section>

    <section className="panel report-card"><h2>گزارش هزینه‌ها</h2><Table rows={expenses} columns={[['tarikh','تاریخ'],['onvan_hazine','عنوان هزینه'],['operator_name','ثبت کننده'],['weaver_name','بافنده'],['mablagh','مبلغ'],['shomare_sanad','شماره سند'],['tozih','توضیحات']]} hideActions /></section>

  </Page>;

}



function salonEmpty() {

  return { metr: '', weight: '', machine: '', kala_id: '', ham_pod: '', ham_chelle: '', shom_chelle: '', user: 'admin', skip_print: false };

}



function LabelPreview({ data }) {

  return <div className="label-preview">

    <h3>بافندگی پرگل</h3>

    <div><span>کد طاقه:</span><b>{data.id || '-'}</b></div>

    <div><span>متراژ:</span><b>{data.metr || '-'}</b></div>

    <div><span>وزن:</span><b>{data.weight || '-'}</b></div>

    <div><span>همبافت چله:</span><b>{data.hamChelle || '-'}</b></div>

    <div><span>همبافت پود:</span><b>{data.hamPod || '-'}</b></div>

    <div><span>شماره چله:</span><b>{data.shomChelle || '-'}</b></div>

    <div className="fake-barcode">{String(data.id || '-').split('').map((ch, i) => <i key={`${ch}-${i}`} />)}</div>

  </div>;

}



function DatabaseManager({ notify }) {

  const [summary, setSummary] = useState({ tables: [] });

  const [backups, setBackups] = useState([]);

  const [importing, setImporting] = useState(false);

  const load = async () => {

    const [s, b] = await Promise.all([api('/database/summary'), api('/database/backups')]);

    setSummary(s);

    setBackups(b.backups || []);

  };

  useEffect(() => { load().catch(() => {}); }, []);

  const backup = async () => {

    const res = await api('/database/backup', { method: 'POST' });

    notify(`بک‌آپ ساخته شد: ${res.file}`);

    await load();

  };

  const exportExcel = () => {

    window.location.href = `${API}/database/export-xlsx`;

  };

  const restoreBackup = async (file) => {

    if (!file || !file.endsWith('.json')) {

      notify('فقط بکاپ‌های JSON جدید قابل بازگردانی هستند.');

      return;

    }

    if (!window.confirm(`بکاپ ${file} روی دیتابیس فعلی بازگردانی شود؟ این کار داده‌های فعلی را جایگزین می‌کند.`)) return;

    await api('/database/restore', { method: 'POST', body: { file } });

    notify(`بکاپ ${file} بازگردانی شد.`);

    await load();

  };

  const importExcel = async (file) => {

    if (!file) return;

    setImporting(true);

    try {

      const fd = new FormData();

      fd.append('file', file);

      const res = await fetch(`${API}/database/import-xlsx`, { method: 'POST', body: fd });

      const data = await res.json();

      if (!res.ok || data.success === false) throw new Error(data.error || 'خطا در بارگذاری اکسل');

      setSummary(data);

      notify('فایل اکسل بارگذاری شد و خلاصه جدول‌ها بروزرسانی شد');

    } finally {

      setImporting(false);

    }

  };

  return <Page title="مدیریت دیتابیس">

    <section className="panel">

      <div className="panel-title-row"><h2>خروجی و ورودی اکسل</h2><button className="primary" onClick={exportExcel}>دانلود فایل اکسل Export</button></div>

      <p>تمام جدول‌های دیتابیس در یک فایل Excel خروجی گرفته می‌شوند؛ هر جدول در یک شیت جداگانه و هر فیلد در ستون خودش قرار می‌گیرد.</p>

      <div className="actions-row">

        <label className="file-button">

          <span>{importing ? 'در حال بارگذاری...' : 'بارگذاری فایل اکسل و جایگذاری در دیتابیس'}</span>

          <input type="file" accept=".xlsx" disabled={importing} onChange={e => importExcel(e.target.files?.[0])} />

        </label>

      </div>

    </section>

    <section className="panel">

      <div className="panel-title-row"><h2>وضعیت پایگاه داده</h2><button onClick={load}>بروزرسانی</button></div>

      <div className="mini-metrics">

        <div><span>مسیر دیتابیس</span><b className="small-value">{summary.database || '-'}</b></div>

        <div><span>تعداد جدول‌ها</span><b>{summary.tables?.length || 0}</b></div>

        <div><span>تعداد بک‌آپ‌ها</span><b>{backups.length}</b></div>

      </div>

    </section>

    <section className="panel">

      <div className="panel-title-row"><h2>پشتیبان‌گیری</h2><button className="primary" onClick={backup}>ایجاد بک‌آپ جدید</button></div>

      <div className="table-wrap"><table><thead><tr><th>نام فایل</th><th>حجم بایت</th><th>تاریخ ایجاد</th><th>عملیات</th></tr></thead><tbody>{backups.length ? backups.map(row => <tr key={row.file}><td>{row.file}</td><td>{display(row.size)}</td><td>{row.date}</td><td><button disabled={!String(row.file).endsWith('.json')} onClick={() => restoreBackup(row.file)}>بازگردانی</button></td></tr>) : <tr><td colSpan="4">بکاپی وجود ندارد.</td></tr>}</tbody></table></div>

    </section>

    <section className="panel">

      <h2>خلاصه جدول‌ها</h2>

      <Table rows={(summary.tables || []).map((x, i) => ({ ...x, id: `${x.table}-${i}` }))} hideActions columns={[['table','نام جدول'],['count','تعداد رکورد']]} />

    </section>

  </Page>;

}



function SparePartsInventory({ lookups, notify, refreshLookups }) {

  const vendorOptions = uniqueText([

    ...(lookups.customers || []).map(x => x.name),

    ...(lookups.drivers || []).map(x => x.name),

    ...(lookups.operators || []).map(x => x.name)

  ]);

  return <CrudPage

    title="موجودی انبار قطعات"

    endpoint="/spare-parts"

    empty={{ part_name: '', part_number: '', quantity: '', condition_status: 'سالم', vendor_name: '', description: '' }}

    notify={notify}

    afterSave={refreshLookups}

    mapEdit={r => ({

      id: r.id,

      spare_part_id: r.spare_part_id,

      part_name: r.part_name,

      part_number: r.part_number,

      quantity: r.quantity,

      condition_status: r.condition_status,

      vendor_name: r.vendor_name,

      description: r.description

    })}

    renderForm={(f, set) => <>

      <Input label="نام قطعه" value={f.part_name} onChange={v => set('part_name', v)} list={(lookups.spareParts || []).map(x => x.name)} />

      <Input label="شماره قطعه" value={f.part_number} onChange={v => set('part_number', v)} />

      <Input label="موجودی" type="number" value={f.quantity} onChange={v => set('quantity', Number(v))} />

      <Select label="وضعیت قطعه" value={f.condition_status || 'سالم'} onChange={v => set('condition_status', v || 'سالم')} items={[
        { id: 'سالم', name: 'سالم' },
        { id: 'دست دوم', name: 'دست دوم' },
        { id: 'کارکرده', name: 'کارکرده' },
        { id: 'مصرفی', name: 'مصرفی' },
        { id: 'تعمیراتی', name: 'تعمیراتی' },
        { id: 'اسقاط', name: 'اسقاط' },
      ]} />

      <Input label="تامین‌کننده" value={f.vendor_name} onChange={v => set('vendor_name', v)} list={vendorOptions} />

      <Input label="توضیحات" value={f.description} onChange={v => set('description', v)} />

    </>}

    columns={[

      ['part_name','نام قطعه'], ['part_number','شماره قطعه'], ['quantity','موجودی'], ['condition_status','وضعیت'],

      ['vendor_name','تامین‌کننده'], ['total_used','مصرف شده'], ['last_machine','آخرین ماشین'], ['last_operator','آخرین تحویل‌گیرنده'], ['last_use_date','آخرین مصرف']

    ]}

    filters={[['part_name','نام قطعه'], ['part_number','شماره قطعه'], ['vendor_name','تامین‌کننده'], ['last_machine','ماشین']]}

  />;

}



function MachineryServices({ lookups, notify, refreshLookups }) {

  const [spareParts, setSpareParts] = useState([]);

  const loadSpareParts = async () => setSpareParts(await api('/spare-parts').catch(() => []));

  useEffect(() => { loadSpareParts(); }, []);

  const spareOptions = spareParts.map(x => ({ id: x.spare_part_id || x.id, name: `${x.part_name || '-'} | موجودی: ${display(x.quantity)}` }));

  return <CrudPage

    title="خدمات ماشین‌آلات"

    endpoint="/machinery-services"

    empty={{ machinery_name: '', service_date: todayJalali(), service_type_id: '', spare_part_id: '', quantity: 1, description: '', operator_name: '' }}

    notify={notify}

    afterSave={async () => { await loadSpareParts(); refreshLookups && await refreshLookups(); }}

    mapEdit={r => ({

      id: r.id,

      machinery_name: r.machinery_name,

      service_date: r.service_date,

      service_type_id: r.service_type_id,

      spare_part_id: r.spare_part_id,

      quantity: r.quantity,

      description: r.description,

      operator_name: r.operator_name

    })}

    renderForm={(f, set) => <>

      <Input label="شماره / نام ماشین" value={f.machinery_name} onChange={v => set('machinery_name', v)} />

      <Input label="تاریخ سرویس" value={f.service_date} onChange={v => set('service_date', v)} />

      <Select label="نوع سرویس" value={f.service_type_id} onChange={v => set('service_type_id', Number(v))} items={lookups.serviceTypes} />

      <Select label="قطعه مصرفی" value={f.spare_part_id} onChange={v => set('spare_part_id', Number(v))} items={spareOptions} />

      <Input label="تعداد مصرف قطعه" type="number" value={f.quantity} onChange={v => set('quantity', Number(v))} />

      <Select label="تحویل‌گیرنده / اپراتور" value={f.operator_name} onChange={v => set('operator_name', v)} items={(lookups.operators || []).map(x => ({ id: x.name, name: x.name }))} />

      <Input label="شرح سرویس" value={f.description} onChange={v => set('description', v)} />

    </>}

    columns={[

      ['machinery_name','ماشین'], ['service_date','تاریخ'], ['service_type','نوع سرویس'], ['spare_part','قطعه'],

      ['quantity','تعداد'], ['operator_name','تحویل‌گیرنده'], ['description','شرح']

    ]}

    filters={[['machinery_name','ماشین'], ['service_type','نوع سرویس'], ['spare_part','قطعه'], ['operator_name','تحویل‌گیرنده']]}

  />;

}



function UsersManager({ notify }) {

  const [users, setUsers] = useState([]);

  const [form, setForm] = useState({ username: '', password: '', role: 'operator' });

  const [accessUser, setAccessUser] = useState(null);

  const [menus, setMenus] = useState([]);

  const load = async () => setUsers(await api('/users'));

  useEffect(() => { load().catch(() => {}); }, []);

  const set = (k, v) => setForm(s => ({ ...s, [k]: v }));

  const save = async () => {

    await api('/users', { method: 'POST', body: form });

    setForm({ username: '', password: '', role: 'operator' });

    notify('کاربر ثبت شد');

    await load();

  };

  const toggle = async (id) => { await api(`/users/${id}/toggle`, { method: 'POST' }); await load(); notify('وضعیت کاربر تغییر کرد'); };

  const del = async (id) => { await api(`/users/${id}`, { method: 'DELETE' }); await load(); notify('کاربر حذف شد'); };

  const loadAccess = async (user) => {

    setAccessUser(user);

    setMenus(await api(`/users/${user.id}/menu-access`));

  };

  const setAccess = (key, checked) => setMenus(list => list.map(m => m.menu_key === key ? { ...m, has_access: checked ? 1 : 0 } : m));

  const saveAccess = async () => {

    const menu_access = {};

    menus.forEach(m => { menu_access[m.menu_key] = Number(m.has_access) === 1 ? 1 : 0; });

    await api(`/users/${accessUser.id}/menu-access`, { method: 'POST', body: { menu_access } });

    notify('دسترسی‌های کاربر ذخیره شد');

  };

  const rows = users.map(u => ({ ...u, status: Number(u.is_active) === 1 ? 'فعال' : 'غیرفعال', role_fa: roleName(u.role) }));

  return <Page title="مدیریت کاربران">

    <section className="panel">

      <h2>ثبت کاربر جدید</h2>

      <div className="form-grid">

        <Input label="نام کاربری" value={form.username} onChange={v => set('username', v)} />

        <Input label="رمز عبور" value={form.password} onChange={v => set('password', v)} type="password" />

        <label><span>نقش کاربر</span><select value={form.role} onChange={e => set('role', e.target.value)}>

          <option value="admin">مدیر سیستم</option>

          <option value="manager">مدیر</option>

          <option value="operator">اپراتور</option>

          <option value="viewer">مشاهده‌گر</option>

        </select></label>

      </div>

      <div className="actions-row"><button className="primary" onClick={save}>ثبت کاربر</button></div>

    </section>

    <section className="panel">

      <h2>لیست کاربران</h2>

      <div className="table-wrap"><table><thead><tr><th>نام کاربری</th><th>نقش</th><th>وضعیت</th><th>تاریخ ایجاد</th><th>عملیات</th></tr></thead><tbody>

        {rows.map(u => <tr key={u.id}><td>{u.username}</td><td>{u.role_fa}</td><td>{u.status}</td><td>{u.created_at}</td><td className="table-actions"><button onClick={() => loadAccess(u)}>دسترسی</button><button onClick={() => toggle(u.id)}>{Number(u.is_active) === 1 ? 'غیرفعال' : 'فعال'}</button><button className="ghost" onClick={() => del(u.id)}>حذف</button></td></tr>)}

      </tbody></table></div>

    </section>

    {accessUser && <section className="panel">

      <div className="panel-title-row"><h2>دسترسی تب‌ها برای {accessUser.username}</h2><button className="primary" onClick={saveAccess}>ذخیره دسترسی</button></div>

      <div className="access-grid">

        {menus.map(m => <label className="access-item" key={m.menu_key}>

          <span>{m.icon} {m.menu_name}</span>

          <input type="checkbox" checked={Number(m.has_access) === 1} onChange={e => setAccess(m.menu_key, e.target.checked)} />

        </label>)}

      </div>

    </section>}

  </Page>;

}



function CrudPage({ title, endpoint, empty, renderForm, columns, notify, afterSave, mapEdit, filters = [], extraSections = null }) {

  const [items, setItems] = useState([]);

  const [filterValues, setFilterValues] = useState({});

  const [form, setForm] = useState(empty);

  const [loading, setLoading] = useState(true);

  const [editing, setEditing] = useState(false);



  const load = async () => {

    setLoading(true);

    try { setItems(await api(endpoint)); } finally { setLoading(false); }

  };

  useEffect(() => { load(); }, [endpoint]);

  const set = (k, v) => setForm(s => ({ ...s, [k]: v }));

  const save = async () => {

    await api(endpoint, { method: 'POST', body: form });

    setForm(empty);

    setEditing(false);

    await load();

    afterSave && afterSave();

    notify(editing ? 'ویرایش انجام شد' : 'ثبت انجام شد');

  };

  const del = async (id) => {

    await api(`${endpoint}/${id}`, { method: 'DELETE' });

    await load();

    afterSave && afterSave();

    notify('رکورد حذف شد');

  };

  const edit = (row) => { setForm(mapEdit ? mapEdit(row) : row); setEditing(true); window.scrollTo({ top: 0, behavior: 'smooth' }); };

  const visible = filterRows(items, filterValues);



  return <Page title={title}>

    <section className="panel">

      <h2>{editing ? `ویرایش ${title}` : `ثبت ${title}`}</h2>

      <div className="form-grid">{renderForm(form, set)}</div>

      <div className="actions-row"><button className="primary" onClick={save}>{editing ? 'ثبت ویرایش' : 'ثبت'}</button>{editing && <button className="ghost" onClick={() => { setForm(empty); setEditing(false); }}>لغو ویرایش</button>}</div>

    </section>

    <section className="panel">

      <h2>لیست {title}</h2>

      <Filters filters={filters} rows={items} values={filterValues} setValues={setFilterValues} onPrint={() => printReport(`لیست ${title}`, visible, columns)} />

      {loading ? <div className="empty">در حال بارگذاری...</div> : <Table rows={visible} columns={columns} onEdit={edit} onDelete={del} />}

    </section>

    {typeof extraSections === 'function' ? extraSections({ items, visible, load }) : extraSections}

  </Page>;

}



function Filters({ filters, values, setValues, onPrint, rows = [] }) {

  if (!filters?.length) return <div className="actions-row report-actions"><button onClick={onPrint}>چاپ گزارش</button></div>;

  return <div className="filters">

    {filters.map(([key, label]) => <label key={key}><span>{label}</span><select value={values[key] || ''} onChange={e => setValues(s => ({ ...s, [key]: e.target.value }))}>

      <option value="">همه</option>

      {filterOptions(rows, key).map(x => <option key={x} value={x}>{x}</option>)}

    </select></label>)}

    <button onClick={() => setValues({})}>پاک کردن فیلتر</button>

    <button onClick={onPrint}>چاپ گزارش</button>

  </div>;

}



function Page({ title, children }) { return <div className="page"><h1>{title}</h1>{children}</div>; }



function filterOptions(rows, key) {

  return [...new Set((rows || []).map(r => r?.[key]).filter(v => String(v ?? '').trim() !== '').map(v => String(v)))].sort();

}



function Table({ rows, columns, onEdit, onDelete, hideActions = false }) {

  if (!rows?.length) return <div className="empty">داده‌ای برای نمایش وجود ندارد.</div>;

  return <div className="table-wrap"><table><thead><tr>{columns.map(c => <th key={c[0]}>{c[1]}</th>)}{!hideActions && <th>عملیات</th>}</tr></thead><tbody>{rows.map((r, i) => <tr key={r.id || `${r.machine}-${r.shom_chelle}-${i}`}>{columns.map(c => <td key={c[0]}>{display(r[c[0]])}</td>)}{!hideActions && <td className="table-actions"><button onClick={() => onEdit(r)}>ویرایش</button><button className="ghost" onClick={() => onDelete(r.id)}>حذف</button></td>}</tr>)}</tbody></table></div>;

}



function Input({ label, value, onChange, type = 'text', list, disabled = false, hint = '' }) {

  const id = `list-${label.replaceAll(' ', '-')}`;

  return <label><span>{label}</span><input type={type} value={value ?? ''} disabled={disabled} onChange={e => onChange(e.target.value)} list={list ? id : undefined} />{list && <datalist id={id}>{list.map(x => <option key={x} value={x} />)}</datalist>}{hint && <small className="field-hint">{hint}</small>}</label>;

}



function Select({ label, value, onChange, items = [] }) {

  return <label><span>{label}</span><select value={value ?? ''} onChange={e => onChange(e.target.value)}><option value="">انتخاب کنید</option>{items.map(x => <option key={x.id} value={x.id}>{x.name}</option>)}</select></label>;

}



function printLabel(data) {

  if (!data.id) { alert('کد طاقه مشخص نیست'); return; }

  const html = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="UTF-8"><title>لیبل ${data.id}</title><style>

@page { size: 50mm 80mm; margin: 0; } * { box-sizing: border-box; }

html, body { width: 50mm; height: 80mm; margin: 0; background: #fff; color: #000; font-family: Tahoma, Arial, sans-serif; overflow: hidden; }

.label { width: 50mm; height: 80mm; border: 1.1mm solid #000; display: flex; flex-direction: column; }

h1 { margin: 0; height: 9mm; line-height: 8.5mm; text-align: center; font-size: 19px; font-weight: 900; border-bottom: .5mm solid #000; }

table { width: 100%; border-collapse: collapse; table-layout: fixed; } td { border-bottom: .4mm solid #000; padding: 0 1.2mm; height: 7mm; overflow: hidden; white-space: nowrap; }

td.label-cell { width: 44%; text-align: right; font-size: 12px; font-weight: 900; } td.value-cell { text-align: center; font-size: 18px; font-weight: 900; direction: ltr; }

tr.big td.value-cell { font-size: 20px; } .barcode-box { flex: 1; min-height: 19mm; display: flex; align-items: center; justify-content: center; padding: 1mm 1.5mm 0; }

#barcode { width: 45mm; height: 18mm; } .code { text-align: center; font-size: 12px; font-weight: 900; height: 6mm; line-height: 5mm; }

</style></head><body><div class="label"><h1>بافندگی پرگل</h1><table>

<tr class="big"><td class="label-cell">کد طاقه:</td><td class="value-cell">${safe(data.id)}</td></tr>

<tr class="big"><td class="label-cell">متراژ:</td><td class="value-cell">${safe(data.metr || '-')}</td></tr>

<tr class="big"><td class="label-cell">وزن:</td><td class="value-cell">${safe(data.weight || '-')}</td></tr>

<tr><td class="label-cell">همبافت چله:</td><td class="value-cell">${safe(data.hamChelle || '-')}</td></tr>

<tr><td class="label-cell">همبافت پود:</td><td class="value-cell">${safe(data.hamPod || '-')}</td></tr>

<tr><td class="label-cell">شماره چله:</td><td class="value-cell">${safe(data.shomChelle || '-')}</td></tr>

</table><div class="barcode-box"><svg id="barcode"></svg></div><div class="code">${safe(data.id)}</div></div></body></html>`;

  const win = window.open('', '_blank', 'width=520,height=820');

  win.document.write(html);

  win.document.close();

  setTimeout(() => {

    const svg = win.document.getElementById('barcode');

    try { JsBarcode(svg, String(data.id), { format: 'CODE128', width: 1.35, height: 55, displayValue: false, margin: 0, background: '#fff', lineColor: '#000' }); } catch { win.document.querySelector('.barcode-box').textContent = String(data.id); }

    win.focus();

    win.print();

  }, 250);

}



function printReport(title, rows, columns, targetWindow = null) {

  const head = columns.map(c => `<th>${safe(c[1])}</th>`).join('');

  const body = rows.map(r => `<tr>${columns.map(c => `<td>${safe(display(r[c[0]]))}</td>`).join('')}</tr>`).join('');

  const html = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="UTF-8"><title>${safe(title)}</title><style>body{font-family:Tahoma;padding:20px;color:#111}h1{text-align:center}table{width:100%;border-collapse:collapse}th,td{border:1px solid #999;padding:8px;text-align:right}th{background:#e5e7eb}@media print{button{display:none}}</style></head><body><button onclick="window.print()">چاپ</button><h1>${safe(title)}</h1><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></body></html>`;

  const win = targetWindow || window.open('', '_blank', 'width=1100,height=800');

  if (!win) {

    alert('مرورگر پنجره چاپ را مسدود کرده است');

    return;

  }

  win.document.open();

  win.document.write(html);

  win.document.close();

  setTimeout(() => win.print(), 300);

}



function printInvoiceTaghes(form, targetWindow = null) {

  const rows = (form.items || []).map((x, i) => ({ ...x, row: i + 1 }));

  const totalMetr = rows.reduce((s, x) => s + Number(x.metr || 0), 0);

  const totalWeight = rows.reduce((s, x) => s + Number(x.weight || 0), 0);

  const columns = [['row','ردیف'],['id','کد طاقه'],['metr','متراژ'],['weight','وزن'],['ham_chelle','همبافت تار'],['ham_pod','همبافت پود'],['shom_chelle','شماره چله'],['kala','کالا']];

  const head = columns.map(c => `<th>${safe(c[1])}</th>`).join('');

  const body = rows.map(r => `<tr>${columns.map(c => `<td>${safe(display(r[c[0]]))}</td>`).join('')}</tr>`).join('');

  const meta = [

    ['شماره سند', form.sanad_no],

    ['شماره فاکتور', form.invoice_no],

    ['تاریخ فاکتور', form.tarikh || 'روز جاری'],

    ['مشتری', form.customer],

    ['نام کالا', form.kala],

    ['تعداد طاقه', rows.length],

    ['جمع متراژ', `${display(totalMetr)} متر`],

    ['جمع وزن', `${display(totalWeight)} کیلوگرم`]

  ];

  const html = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="UTF-8"><title>لیست طاقه‌های خروجی</title><style>body{font-family:Tahoma;padding:20px;color:#111}h1{text-align:center}.meta{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin:16px 0}.meta div{border:1px solid #999;padding:8px;border-radius:6px}table{width:100%;border-collapse:collapse}th,td{border:1px solid #999;padding:8px;text-align:center}th{background:#e5e7eb}.total{background:#dcfce7;font-weight:900}@media print{button{display:none}}</style></head><body><button onclick="window.print()">چاپ</button><h1>لیست طاقه‌های خروجی</h1><div class="meta">${meta.map(([k,v]) => `<div><b>${safe(k)}:</b> ${safe(display(v))}</div>`).join('')}</div><table><thead><tr>${head}</tr></thead><tbody>${body}<tr class="total"><td colspan="2">جمع کل</td><td>${safe(display(totalMetr))}</td><td>${safe(display(totalWeight))}</td><td colspan="4">${safe(display(rows.length))} عدد طاقه</td></tr></tbody></table></body></html>`;

  const win = targetWindow || window.open('', '_blank', 'width=1100,height=800');

  if (!win) {

    alert('مرورگر پنجره چاپ را مسدود کرده است');

    return;

  }

  win.document.open();

  win.document.write(html);

  win.document.close();

  setTimeout(() => win.print(), 300);

}



async function api(path, opts = {}) {

  const request = () => fetch(`${API}${path}`, {
    method: opts.method || 'GET',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: opts.body ? JSON.stringify(opts.body) : undefined
  });

  let res = await request();

  if (res.status === 401 && path !== '/login' && PORTAL_OPERATIONAL_SESSION) {
    const refreshed = await fetch(PORTAL_OPERATIONAL_SESSION, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    if (refreshed.ok) res = await request();
  }

  const text = await res.text();

  const data = text ? JSON.parse(text) : {};

  if (res.status === 401 && path !== '/login') {
    try { localStorage.removeItem('operationalUser'); } catch {}
    if (typeof window !== 'undefined') {
      window.dispatchEvent(new Event('operational-auth-expired'));
    }
  }

  if (!res.ok || data.success === false) throw new Error(data.error || 'خطا در ارتباط با سرور');

  return data;

}



function filterRows(rows, filters) {

  return rows.filter(row => Object.entries(filters || {}).every(([key, val]) => !String(val || '').trim() || String(row[key] ?? '').toLowerCase().includes(String(val).trim().toLowerCase())));

}

function uniqueOptions(items, selected) {

  if (!selected || items.some(x => String(x.id) === String(selected))) return items;

  return [{ id: selected, name: `رکورد انتخابی ${selected}` }, ...items];

}

function uniqueText(items) {

  return [...new Set((items || []).filter(v => String(v ?? '').trim() !== '').map(v => String(v).trim()))].sort();

}

function fmt(n) { return Number(n || 0).toLocaleString('fa-IR'); }

function todayJalali() { return new Date().toLocaleDateString('fa-IR-u-ca-persian').replace(/\u200e/g, ''); }

function roleName(role) {

  return ({ admin: 'مدیر سیستم', manager: 'مدیر', operator: 'اپراتور', viewer: 'مشاهده‌گر' })[role] || role || '';

}

function display(v) {

  if (v === 'vorud' || v === 'ورودي') return 'ورود';

  if (v === 'khoroj' || v === 'خروجي') return 'خروج';

  if (typeof v === 'number') return v.toLocaleString('fa-IR');

  return v ?? '';

}

function safe(value) {

  return String(value ?? '').replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' }[ch]));

}



const isMobileLoadingPage = /\/(operational\/)?mobile-load\/?$/.test(window.location.pathname);

createRoot(document.getElementById('root')).render(isMobileLoadingPage ? <MobileLoadingPage /> : <App />);
