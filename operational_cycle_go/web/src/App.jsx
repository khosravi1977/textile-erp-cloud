import React, { useEffect, useMemo, useState } from 'react';

import { createRoot } from 'react-dom/client';

import JsBarcode from 'jsbarcode';

import QRCode from 'qrcode';

import { BrowserMultiFormatReader } from '@zxing/browser';

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



const tabs = [

  ['formulas', 'فرمول پیش‌فرض ماشین‌ها'],

  ['dashboard', 'داشبورد'],

  ['advisor', 'تحلیل و مشاور هوشمند'],

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

  ['advisor', 'تحلیل و مشاور هوشمند'],

  ['initial', 'اطلاعات اولیه'],

  ['nakh-vor', 'ورود نخ'],

  ['chelle', 'ورود چله'],

  ['gere', 'گره'],

  ['nakh-salon', 'ورود نخ سالن'],

  ['formulas', 'فرمول پیش‌فرض ماشین‌ها'],

  ['salon', 'سالن تولید'],

  ['consumption', 'مصرف تار و پود و ضایعات'],

  ['yarn-out', 'خروج نخ'],

  ['empty-beam-out', 'خروج نورد خالی'],

  ['out-invoice', 'فاکتور خروج'],

  ['expenses', 'هزینه‌ها'],

  ['reports', 'گزارشات'],

  ['database', 'مدیریت دیتابیس'],

  ['machinery-services', 'خدمات ماشین‌آلات'],

  ['spare-parts', 'موجودی انبار قطعات']

];



function App() {

  const loadingPathMatch = window.location.pathname.match(/\/operational\/loading\/([^/?#]+)/);

  const loadingToken = loadingPathMatch ? decodeURIComponent(loadingPathMatch[1]) : '';

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

    if (window.ERP_PORTAL_PREFIX) window.location.assign('/module-logout?module=operational&login=1');

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

  if (loadingToken) {

    return <LoadingMobilePage token={loadingToken} session={session} onLogout={logout} />;

  }

  const allowedTabs = new Set((session.menus || []).map(m => m.menu_key));

  const visibleTabs = sidebarTabs.filter(([id]) => allowedTabs.has(id));

  if (visibleTabs.length && !visibleTabs.some(([id]) => id === tab)) {

    setTimeout(() => setTab(visibleTabs[0][0]), 0);

  }



  return (

    <div className="app-shell">

      <aside className="sidebar">

        <h1>ERP نساجی</h1>

        <p>سیکل عملیاتی Go</p>

        <nav>{visibleTabs.map(([id, label]) => <button key={id} className={tab === id ? 'active' : ''} onClick={() => setTab(id)}>{label}</button>)}</nav>

        <button className="ghost" onClick={logout}>خروج و ورود کاربر دیگر</button>

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

    case 'advisor': return <OperationalAdvisor />;

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

      <h2>موجودی نخ بر اساس مالک، هم‌بافت و نوع نخ</h2>

      <Table rows={data.yarn_inventory || []} columns={[['mosh','مالک نخ'],['hambaft','همبافت'],['yarn','نوع نخ'],['inventory','مانده'],['vorud','ورود'],['to_salon','خالص ارسال سالن'],['khoroj','سایر خروج']]} hideActions />

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

  const [inventory, setInventory] = useState([]);

  const [movements, setMovements] = useState([]);

  const load = () => Promise.all([api('/nakh-salon?chelles=1'), api('/nakh-khor?inventory=1'), api('/nakh-salon')])
    .then(([activeChelles, yarnInventory, salonMovements]) => {
      setChelles(activeChelles);
      setInventory(yarnInventory);
      setMovements(salonMovements);
    })
    .catch(() => { setChelles([]); setInventory([]); setMovements([]); });

  useEffect(() => { load(); }, []);

  return <CrudPage

    title="ورود نخ سالن"

    endpoint="/nakh-salon"

    empty={{ machine: '', ham_nakh: '', weight: '', chelle_id: '', mosh_name: '', nakh_name: '', vor_khor: 'vorud' }}

    notify={notify}

    afterSave={load}

    filters={[['machine','ماشین'],['ham_nakh','همبافت نخ'],['nakh_name','نوع نخ'],['shom_chelle','چله'],['mosh_name','مالک نخ'],['vor_khor','نوع']]}

    mapEdit={row => ({ id: row.id, machine: row.machine || '', ham_nakh: row.ham_nakh || '', weight: Math.abs(Number(row.weight || 0)), chelle_id: row.chelle_id || '', mosh_name: row.mosh_name || '', nakh_name: row.nakh_name || '', vor_khor: row.vor_khor || 'vorud' })}

    renderForm={(form, set) => {
      const current = movements.find(x => Number(x.id) === Number(form.id));
      const inventoryRow = inventory.find(x => x.mosh === form.mosh_name && x.hambaft === form.ham_nakh && x.yarn === form.nakh_name);
      const warehouseAvailable = Number(inventoryRow?.inventory || 0)
        + (current?.vor_khor === 'vorud' ? Math.abs(Number(current.weight || 0)) : 0)
        - (current?.vor_khor === 'khoroj' ? Math.abs(Number(current.weight || 0)) : 0);
      const returnable = movements
        .filter(x => Number(x.id) !== Number(form.id)
          && Number(x.chelle_id) === Number(form.chelle_id)
          && x.machine === form.machine
          && x.mosh_name === form.mosh_name
          && x.ham_nakh === form.ham_nakh
          && x.nakh_name === form.nakh_name)
        .reduce((sum, x) => sum + (x.vor_khor === 'khoroj' ? -Math.abs(Number(x.weight || 0)) : Math.abs(Number(x.weight || 0))), 0);
      const chooseChelle = value => {
        const id = Number(value);
        const selected = chelles.find(x => Number(x.id) === id);
        set('chelle_id', id);
        if (selected) {
          set('machine', selected.machine || '');
          set('mosh_name', selected.mosh_name || form.mosh_name || '');
        }
      };
      return <>

      <Input label="شماره ماشین" value={form.machine} onChange={v => set('machine', v)} hint="باید با ماشین چله فعال یکسان باشد." />

      <Select label="هم‌بافت نخ پود" value={form.ham_nakh} onChange={v => set('ham_nakh', v)} items={(lookups.hambaftYarn || []).map(x => ({ id: x, name: x }))} />

      <Input label="وزن" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />

      <Select label="شماره چله فعال روی ماشین" value={form.chelle_id} onChange={chooseChelle} items={uniqueOptions(chelles.map(x => ({ id: x.id, name: `${x.shom_chelle} - ماشین ${x.machine}` })), form.chelle_id)} />

      <Select label="مالک نخ / مشتری" value={form.mosh_name} onChange={v => set('mosh_name', v)} items={(lookups.customers || []).map(x => ({ id: x.name, name: x.name }))} />

      <Select label="نوع نخ پود" value={form.nakh_name} onChange={v => set('nakh_name', v)} items={(lookups.yarns || []).map(x => ({ id: x.name, name: x.name }))} />

      <Select label="نوع" value={form.vor_khor} onChange={v => set('vor_khor', v)} items={[{id:'vorud',name:'ورود'}, {id:'khoroj',name:'خروج'}]} />

      {(form.chelle_id && form.mosh_name && form.ham_nakh && form.nakh_name) && <div className="form-help">
        موجودی دقیق قابل تخصیص از انبار: {fmt(warehouseAvailable)} کیلو — مقدار خالص قابل مرجوع از این ماشین و چله: {fmt(Math.max(0, returnable))} کیلو
      </div>}

    </>;
    }}

    columns={[['tarikh','تاریخ'],['machine','ماشین'],['ham_nakh','همبافت نخ'],['nakh_name','نوع نخ'],['weight','وزن'],['shom_chelle','چله'],['mosh_name','مالک نخ'],['vor_khor','نوع']]}

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

  const [formulaLoading, setFormulaLoading] = useState(false);

  const [formulaConfigured, setFormulaConfigured] = useState(false);

  const [formulaSource, setFormulaSource] = useState('');

  const [podOptions, setPodOptions] = useState([]);

  const [saving, setSaving] = useState(false);

  const [formError, setFormError] = useState('');



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

  const fetchBeamFormula = async (machine, shomChelle, kalaId, hamChelle, hamPod) => {

    if (!machine || !shomChelle || !kalaId) return null;

    setFormulaLoading(true);

    try {

      const query = new URLSearchParams({
        machine,
        shom_chelle: shomChelle,
        kala_id: String(kalaId),
        ham_chelle: hamChelle || '',
        ham_pod: hamPod || ''
      });

      return await api(`/salon/formula?${query.toString()}`);

    } finally {

      setFormulaLoading(false);

    }

  };

  const applyBeamFormula = async (machine, shomChelle, kalaId, hamChelle, hamPod) => {

    const formula = await fetchBeamFormula(machine, shomChelle, kalaId, hamChelle, hamPod);

    if (!formula) {

      setFormulaConfigured(false);
      setFormulaSource('');

      return;

    }

    setFormulaConfigured(Boolean(formula.configured));
    setFormulaSource(formula.source || '');

    setForm(s => ({

      ...s,

      tar_percent: Number(formula.tar_percent ?? 50),

      pod_percent: Number(formula.pod_percent ?? 50)

    }));

  };



  const loadMachineDefaults = async (machine) => {

    set('machine', machine);

    setFormError('');

    if (!machine) {

      setPodOptions([]);

      return setRecent([]);

    }

    const [recentData, defaults] = await Promise.all([

      api(`/salon/recent-chelles/${encodeURIComponent(machine)}`),

      api(`/salon/defaults/${encodeURIComponent(machine)}`)

    ]);

    const recentItems = recentData.items || [];

    setRecent(recentItems);

    const latest = recentItems[0];

    let selectedChelle = latest?.shom_chelle || (defaults.found ? defaults.shom_chelle : '');

    let selectedChelleID = latest?.chelle_id || (defaults.found ? defaults.chelle_id : '');

    let selectedHambaft = latest?.hambaft || (defaults.found ? defaults.ham_chelle : '');

    const previousChelle = defaults.found ? defaults.shom_chelle : '';

    if (latest?.shom_chelle && previousChelle && latest.shom_chelle !== previousChelle) {

      const useNew = window.confirm(`برای ماشین ${machine} چله جدید ${latest.shom_chelle} گره خورده است. آیا این طاقه از چله جدید است؟\nOK = چله جدید\nCancel = چله قبلی ${previousChelle}`);

      if (!useNew) {

        selectedChelle = previousChelle;

        selectedChelleID = defaults.chelle_id || selectedChelleID;

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

    const selectedKalaId = defaults.found && defaults.kala_id ? defaults.kala_id : form.kala_id;

    let selectedHamPod = '';

    if (selectedChelleID) {

      try {

        const podData = await api(`/salon/pod-options/${encodeURIComponent(machine)}/${encodeURIComponent(selectedChelleID)}`);

        const options = podData.items || [];

        setPodOptions(options);

        selectedHamPod = options.find(x => x.hambaft === form.ham_pod)?.hambaft || options[0]?.hambaft || '';

      } catch {

        setPodOptions([]);

      }

    } else {

      setPodOptions([]);

    }

    setForm(s => {

      return {

        ...s,

        machine,

        kala_id: selectedKalaId || s.kala_id,

        ham_pod: selectedHamPod,

        ham_chelle: selectedHambaft || s.ham_chelle,

        shom_chelle: selectedChelle || s.shom_chelle,

        chelle_id: selectedChelleID || s.chelle_id

      };

    });

    if (selectedChelle && selectedKalaId) {

      try {

        await applyBeamFormula(machine, selectedChelle, selectedKalaId, selectedHambaft, selectedHamPod);

      } catch {

        setFormulaConfigured(false);
        setFormulaSource('');

        notify('فرمول این چله خوانده نشد؛ درصد تار و پود را کنترل کنید');

      }

    } else {

      setFormulaConfigured(false);
      setFormulaSource('');

    }

  };



  const save = async () => {

    if (saving) return;

    setFormError('');

    const required = [

      [form.machine, 'شماره ماشین'], [form.kala_id, 'نام کالا'], [Number(form.metr) > 0, 'متراژ'],

      [Number(form.weight) > 0, 'وزن'], [form.chelle_id, 'شماره چله فعال'],

      [form.ham_chelle, 'هم‌بافت تار'], [form.ham_pod, 'هم‌بافت نخ پود']

    ];

    const missing = required.filter(([value]) => !value).map(([, label]) => label);

    if (missing.length) {

      const message = `این موارد را کامل کنید: ${missing.join('، ')}`;

      setFormError(message);

      notify(message);

      return;

    }

    const formulaTotal = Number(form.tar_percent || 0) + Number(form.pod_percent || 0);

    if (Number(form.tar_percent) < 0 || Number(form.tar_percent) > 100 || Number(form.pod_percent) < 0 || Number(form.pod_percent) > 100 || Math.abs(formulaTotal - 100) > 0.001) {

      const message = 'هر درصد باید بین صفر تا صد باشد و جمع تار و پود دقیقاً ۱۰۰ شود';

      setFormError(message);

      notify(message);

      return;

    }

    const savedLabel = labelData();

    setSaving(true);

    try {

      await api('/salon', { method: 'POST', body: { ...form, formula_confirmed: true } });

      notify(editing ? 'طاقه ویرایش شد' : 'طاقه ثبت شد');

      if (!form.skip_print) printLabel(savedLabel);

      setForm(salonEmpty());

      setEditing(false);

      setRecent([]);

      setPodOptions([]);

      setFormulaConfigured(false);

      setFormulaSource('');

      await load();

    } catch (err) {

      const message = err?.message || 'ثبت طاقه انجام نشد؛ اطلاعات فرم را کنترل کنید.';

      setFormError(message);

      notify(message);

    } finally {

      setSaving(false);

    }

  };



  const edit = async (row) => {

    setEditing(true);

    setForm({ id: row.id, metr: Number(row.metr || 0), weight: Number(row.weight || 0), machine: row.machine || '', kala_id: row.kala_id || '', ham_pod: row.ham_pod || '', ham_chelle: row.ham_chelle || '', shom_chelle: row.shom_chelle || '', chelle_id: row.chelle_id || '', user: row.user || 'admin', tar_percent: Number(row.tar_percent ?? 50), pod_percent: Number(row.pod_percent ?? 50), skip_print: true });

    setFormulaConfigured(true);
    setFormulaSource('beam');

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

          <Select label="نام کالا" value={form.kala_id} onChange={async v => {

            const kalaId = Number(v);

            set('kala_id', kalaId);

            if (form.machine && form.shom_chelle) {

              try { await applyBeamFormula(form.machine, form.shom_chelle, kalaId, form.ham_chelle, form.ham_pod); }

              catch { setFormulaConfigured(false); setFormulaSource(''); }

            }

          }} items={lookups.fabrics} />

          <Input label="متراژ" type="number" value={form.metr} onChange={v => set('metr', Number(v))} />

          <Input label="وزن" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />

          <Input label="درصد مصرف تار همین چله" type="number" value={form.tar_percent} onChange={v => {

            const tar = Number(v);

            setForm(s => ({ ...s, tar_percent: tar, pod_percent: Math.max(0, 100 - tar) }));

            setFormulaConfigured(true);
            setFormulaSource('manual');

          }} hint="این درصد فقط برای همین چله و همین پارچه ذخیره می‌شود." />

          <Input label="درصد مصرف پود همین چله" type="number" value={form.pod_percent} onChange={v => {

            const pod = Number(v);

            setForm(s => ({ ...s, pod_percent: pod, tar_percent: Math.max(0, 100 - pod) }));

            setFormulaConfigured(true);
            setFormulaSource('manual');

          }} hint="جمع درصد تار و پود باید ۱۰۰ باشد." />

          <Select label="هم‌بافت نخ پود" value={form.ham_pod} onChange={v => {
            set('ham_pod', v);
            setFormulaConfigured(false);
            setFormulaSource('');
          }} items={uniqueOptions(podOptions.map(x => ({ id: x.hambaft, name: `${x.hambaft}${x.yarn ? ` - ${x.yarn}` : ''} - مانده ${fmt(x.balance)} کیلو` })), form.ham_pod)} hint="فقط از نخ پود تخصیص‌یافته به همین ماشین و همین چله انتخاب می‌شود." />

          <Select label="شماره چله فعال" value={form.chelle_id} onChange={async v => {

            const chelleID = Number(v);

            const row = recent.find(x => Number(x.chelle_id) === chelleID);
            const selectedHambaft = row?.hambaft || '';

            set('chelle_id', chelleID);

            if (row) {

              set('shom_chelle', row.shom_chelle || '');

              set('ham_chelle', selectedHambaft);

            }

            let selectedHamPod = '';

            if (row && form.machine) {

              try {

                const podData = await api(`/salon/pod-options/${encodeURIComponent(form.machine)}/${encodeURIComponent(chelleID)}`);

                const options = podData.items || [];

                setPodOptions(options);

                selectedHamPod = options.find(x => x.hambaft === form.ham_pod)?.hambaft || options[0]?.hambaft || '';

                set('ham_pod', selectedHamPod);

              } catch {

                setPodOptions([]);

                set('ham_pod', '');

              }

            }

            if (row && form.machine && form.kala_id) {

              try { await applyBeamFormula(form.machine, row.shom_chelle || '', form.kala_id, selectedHambaft, selectedHamPod); }

              catch { setFormulaConfigured(false); setFormulaSource(''); }

            }

            if (row) {

              set('shom_chelle', row.shom_chelle || '');

              set('ham_chelle', row.hambaft || '');

            }

          }} items={recent.map(x => ({ id: x.chelle_id, name: `${x.shom_chelle} - ${x.hambaft || 'بدون همبافت'} - ${fmt(x.weight)} کیلو` }))} />

          <Input label="همبافت تار / چله" value={form.ham_chelle} onChange={v => {
            set('ham_chelle', v);
            setFormulaConfigured(false);
            setFormulaSource('');
          }} onBlur={async v => {
            if (form.machine && form.shom_chelle && form.kala_id && v) {
              try { await applyBeamFormula(form.machine, form.shom_chelle, form.kala_id, v, form.ham_pod); }
              catch { setFormulaConfigured(false); setFormulaSource(''); }
            }
          }} />

          <Input label="ثبت‌کننده" value="از نشست کاربر واردشده ثبت می‌شود" onChange={() => {}} disabled />

          <label className="check-line"><input type="checkbox" checked={!!form.skip_print} onChange={e => set('skip_print', e.target.checked)} /> <span>بعد از ثبت لیبل چاپ نشود</span></label>

        </div>

        <LabelPreview data={labelData()} />

        <div className={`beam-formula-note ${formulaConfigured ? 'configured' : 'needs-confirmation'}`}>

          <strong>{formulaLoading
            ? 'در حال بررسی فرمول هم‌بافت...'
            : formulaSource === 'same_hambaft'
              ? 'فرمول قبلی همین هم‌بافت و پارچه بازیابی شد'
              : formulaSource === 'beam'
                ? 'فرمول ثبت‌شده این چله آماده است'
                : formulaSource === 'manual'
                  ? 'درصدها توسط شما تعیین شد'
                  : 'هم‌بافت یا پارچه جدید؛ درصدها را تأیید کنید'}</strong>

          <span>برای ترکیب یکسان هم‌بافت تار، هم‌بافت پود و پارچه دوباره سؤال نمی‌شود. درصد نهایی همراه هر طاقه نگهداری می‌شود و آمار گذشته تغییر نمی‌کند.</span>

          {!formulaLoading && !formulaConfigured && <button type="button" onClick={() => {
            const tar = Number(form.tar_percent);
            const pod = Number(form.pod_percent);
            if (tar < 0 || tar > 100 || pod < 0 || pod > 100 || Math.abs(tar + pod - 100) > 0.001) {
              notify('هر درصد باید بین صفر تا صد باشد و جمع تار و پود دقیقاً ۱۰۰ شود');
              return;
            }
            setFormulaConfigured(true);
            setFormulaSource('manual');
            notify('درصد مصرف این هم‌بافت و پارچه تأیید شد');
          }}>تأیید درصد تار و پود</button>}

        </div>

      </div>

      <div className="actions-row">

        <button className="primary" onClick={save} disabled={saving || formulaLoading}>{saving ? 'در حال ثبت...' : editing ? 'ثبت ویرایش طاقه' : formulaConfigured ? 'ثبت طاقه' : 'تأیید درصد و ثبت طاقه'}</button>

        <button onClick={() => printLabel(labelData())}>چاپ لیبل و بارکد</button>

        {editing && <button className="ghost" onClick={() => { setEditing(false); setForm(salonEmpty()); setFormulaConfigured(false); setFormulaSource(''); }}>لغو ویرایش</button>}

      </div>

      {formError && <div className="error-box" role="alert">{formError}</div>}

      <div className="hint">با وارد کردن شماره ماشین، آخرین کالا، همبافت پود، همبافت تار/چله و چله‌های آخر همان ماشین به صورت خودکار جایگذاری می‌شود.</div>

    </section>

    <section className="panel">

      <h2>لیست سالن تولید</h2>

      <Filters filters={filterDefs} rows={items} values={filters} setValues={setFilters} onPrint={() => printReport('لیست سالن تولید', visible, [['tarikh','تاریخ'],['id','کد طاقه'],['machine','ماشین'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['shom_chelle','چله'],['tar_percent','درصد تار'],['pod_percent','درصد پود'],['ham_chelle','همبافت تار/چله'],['ham_pod','همبافت پود']])} onExcel={() => exportExcel('لیست سالن تولید', visible, [['tarikh','تاریخ'],['id','کد طاقه'],['machine','ماشین'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['shom_chelle','چله'],['tar_percent','درصد تار'],['pod_percent','درصد پود'],['ham_chelle','همبافت تار/چله'],['ham_pod','همبافت پود']])} />

      {loading ? <div className="empty">در حال بارگذاری...</div> : <Table rows={visible} columns={[['tarikh','تاریخ'],['id','کد طاقه'],['machine','ماشین'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['shom_chelle','چله'],['tar_percent','درصد تار'],['pod_percent','درصد پود'],['ham_chelle','همبافت تار/چله'],['ham_pod','همبافت پود']]} onEdit={edit} onDelete={del} />}

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
  const [inventoryRows, setInventoryRows] = useState([]);

  const loadWarperBalances = () => api('/warper-yarn-balance').then(setWarperBalanceRows).catch(() => setWarperBalanceRows([]));
  const loadInventory = () => api('/nakh-khor?inventory=1').then(setInventoryRows).catch(() => setInventoryRows([]));

  useEffect(() => {
    api('/nakh-vor').then(setYarnInRows).catch(() => setYarnInRows([]));
    loadWarperBalances();
    loadInventory();
  }, []);

  return <CrudPage

    title="خروج نخ"

    endpoint="/nakh-khor"

    empty={{ hambaft: '', weight: '', owner_mosh: '', mosh_name: '', nakh_name: '', destination_type: 'warper' }}

    notify={notify}
    afterSave={() => { loadWarperBalances(); loadInventory(); }}

    filters={[['owner_mosh','مالک نخ'],['mosh','مقصد'],['nakh','نخ'],['hambaft','همبافت']]}

    mapEdit={row => ({ id: row.id, hambaft: row.hambaft || '', weight: Math.abs(Number(row.weight || 0)), owner_mosh: row.owner_mosh || '', mosh_name: row.mosh || '', nakh_name: row.nakh || '', destination_type: row.destination_type || 'warper' })}

    renderForm={(form, set) => {

      const relatedRows = yarnInRows.filter(r => (!form.nakh_name || r.nakh === form.nakh_name) && (!form.hambaft || r.hambaft === form.hambaft));

      const hambaftOptions = [...new Set(yarnInRows.filter(r => !form.nakh_name || r.nakh === form.nakh_name).map(r => r.hambaft).filter(Boolean))];

      const yarnOptions = [...new Set(yarnInRows.filter(r => !form.hambaft || r.hambaft === form.hambaft).map(r => r.nakh).filter(Boolean))];

      const recipientOptions = form.destination_type === 'warper'
        ? (lookups.warpers || []).map(x => x.name)
        : uniqueText([...(lookups.customers || []).map(x => x.name), ...(lookups.warpers || []).map(x => x.name)]);

      const exactInventory = inventoryRows.find(x => x.mosh === form.owner_mosh && x.hambaft === form.hambaft && x.yarn === form.nakh_name);

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

      <Select label="مالک نخ / مشتری" value={form.owner_mosh} onChange={v => set('owner_mosh', v)} items={(lookups.customers || []).map(x => ({ id: x.name, name: x.name }))} />

      <Select label="نوع مقصد" value={form.destination_type} onChange={v => { set('destination_type', v); set('mosh_name', ''); }} items={[{id:'warper',name:'چله‌پیچ'}, {id:'other',name:'مشتری / مصرف دیگر'}]} />

      <Input label="مقصد خروج" value={form.mosh_name} onChange={v => set('mosh_name', v)} list={recipientOptions} />

      <Input label="نوع نخ" value={form.nakh_name} onChange={setYarn} list={yarnOptions.length ? yarnOptions : (lookups.yarns || []).map(x => x.name)} />

      {(form.hambaft || form.nakh_name) && <div className="form-help">گزینه‌ها بر اساس ورود نخ فیلتر شده‌اند. موارد مرتبط: {relatedRows.length} — موجودی دقیق قابل خروج: {exactInventory ? `${fmt(exactInventory.inventory)} کیلو` : 'انتخاب مالک، هم‌بافت و نوع نخ'}</div>}

    </>;

    }}

    columns={[['tarikh','تاریخ'],['owner_mosh','مالک نخ'],['hambaft','همبافت'],['nakh','نخ'],['weight','وزن'],['mosh','مقصد'],['destination_type','نوع مقصد']]}

    extraSections={<section className="panel">
      <div className="panel-title-row"><h2>گزارش مانده نخ نزد چله‌پیچ</h2><button onClick={loadWarperBalances}>بروزرسانی</button></div>
      <p className="hint">این گزارش وزن نخ ارسال‌شده به چله‌پیچی را با چله‌های برگشتی همان چله‌پیچ، همبافت و نوع نخ مقایسه می‌کند.</p>
      <Table rows={warperBalanceRows} columns={[
        ['warper','چله‌پیچ'],
        ['owner','مالک نخ'],
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

      <Filters filters={[['invoice_no','شماره فاکتور'],['mosh','مشتری'],['kala','کالا'],['sanad','شماره سند']]} rows={rows} values={filters} setValues={setFilters} onPrint={() => printReport('لیست فاکتورهای خروج', visible, invoiceCols)} onExcel={() => exportExcel('لیست فاکتورهای خروج', visible, invoiceCols)} />

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

      <Filters filters={[['tarikh','تاریخ'],['mosh','مشتری'],['kala','کالا'],['hambaft','همبافت'],['onvan_hazine','هزینه']]} rows={[...invoices, ...yarnOut, ...expenses]} values={filters} setValues={setFilters} onPrint={() => printReport('گزارش ترکیبی عملیاتی', [...invoices, ...yarnOut, ...expenses], [['tarikh','تاریخ'],['invoice_no','فاکتور'],['mosh','مشتری'],['kala','کالا'],['hambaft','همبافت'],['weight','وزن'],['mablagh','مبلغ'],['onvan_hazine','عنوان هزینه']])} onExcel={() => exportExcel('گزارش ترکیبی عملیاتی', [...invoices, ...yarnOut, ...expenses], [['tarikh','تاریخ'],['invoice_no','فاکتور'],['mosh','مشتری'],['kala','کالا'],['hambaft','همبافت'],['weight','وزن'],['mablagh','مبلغ'],['onvan_hazine','عنوان هزینه']])} />

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
  const [waste, setWaste] = useState([]);
  const [form, setForm] = useState({ machine: '', shom_chelle: '', chelle_id: '', waste_type: 'tar', weight: '', reason: '', description: '', corrective_action: '' });
  const [editing, setEditing] = useState(false);

  const load = () => Promise.all([api('/consumption/machines'), api('/production-waste')])
    .then(([machines, wasteRows]) => { setRows(machines); setWaste(wasteRows); })
    .catch(() => { setRows([]); setWaste([]); });

  useEffect(() => { load(); }, []);

  const set = (key, value) => setForm(s => ({ ...s, [key]: value }));
  const chooseMachine = machine => {
    const current = rows.find(r => r.machine === machine);
    setForm(s => ({ ...s, machine, shom_chelle: current?.shom_chelle || '', chelle_id: current?.chelle_id || '' }));
  };
  const saveWaste = async () => {
    await api('/production-waste', { method: 'POST', body: form });
    setForm({ machine: '', shom_chelle: '', chelle_id: '', waste_type: 'tar', weight: '', reason: '', description: '', corrective_action: '' });
    setEditing(false);
    await load();
  };
  const editWaste = row => {
    setForm({ id: row.id, machine: row.machine || '', shom_chelle: row.shom_chelle || '', chelle_id: row.chelle_id || '', waste_type: row.waste_type || 'tar', weight: Number(row.weight || 0), reason: row.reason || '', description: row.description || '', corrective_action: row.corrective_action || '' });
    setEditing(true);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };
  const deleteWaste = async id => { await api(`/production-waste/${id}`, { method: 'DELETE' }); await load(); };

  const activeRows = rows.map(r => ({ machine: r.machine, shom_chelle: r.shom_chelle, chelle_weight: r.chelle_weight, pod_assigned: r.pod_assigned, tar_used: r.tar_used, pod_used: r.pod_used, tar_remaining: r.tar_remaining, pod_remaining: r.pod_remaining, actual_waste: r.actual_waste, total_weight: r.total_weight, remaining: r.remaining, remaining_percent: r.remaining_percent, material_shortage: r.material_shortage, tarikh: r.tarikh }));

  const wasteRows = rows.map(r => ({ machine: r.machine, shom_chelle: r.shom_chelle, total_meter: r.total_meter, total_weight: r.total_weight, actual_waste: r.actual_waste, waste_per_meter: r.waste_per_meter, waste_per_kg: r.waste_per_kg, waste_percent_input: r.waste_percent_input, waste_percent_output: r.waste_percent_output, tarikh: r.tarikh }));
  const wasteHistory = waste.map(r => ({ ...r, waste_type_fa: ({ tar: 'تار', pod: 'پود', fabric: 'پارچه معیوب', selvage: 'کناره', other: 'سایر' })[r.waste_type] || r.waste_type }));

  return <Page title="مصرف تار و پود و ضایعات">

    <section className="panel">
      <h2>{editing ? 'ویرایش ضایعات واقعی' : 'ثبت ضایعات واقعی تولید'}</h2>
      <p className="hint">مانده نخ، ضایعات نیست. فقط وزن فیزیکی دورریز یا پارچه معیوب را در این فرم ثبت کنید.</p>
      <div className="form-grid">
        <Select label="ماشین" value={form.machine} onChange={chooseMachine} items={rows.map(r => ({ id: r.machine, name: `ماشین ${r.machine} — چله ${r.shom_chelle}` }))} />
        <Input label="شماره چله" value={form.shom_chelle} onChange={() => {}} disabled hint="از چله فعال همان ماشین انتخاب می‌شود." />
        <Select label="نوع ضایعات" value={form.waste_type} onChange={v => set('waste_type', v)} items={[{id:'tar',name:'تار'}, {id:'pod',name:'پود'}, {id:'fabric',name:'پارچه معیوب'}, {id:'selvage',name:'کناره'}, {id:'other',name:'سایر'}]} />
        <Input label="وزن ضایعات (kg)" type="number" value={form.weight} onChange={v => set('weight', Number(v))} />
        <Input label="علت ضایعات" value={form.reason} onChange={v => set('reason', v)} />
        <Input label="توضیحات" value={form.description} onChange={v => set('description', v)} />
        <Input label="اقدام اصلاحی" value={form.corrective_action} onChange={v => set('corrective_action', v)} />
      </div>
      <div className="actions-row"><button className="primary" onClick={saveWaste}>{editing ? 'ثبت ویرایش' : 'ثبت ضایعات'}</button>{editing && <button className="ghost" onClick={() => { setEditing(false); setForm({ machine: '', shom_chelle: '', chelle_id: '', waste_type: 'tar', weight: '', reason: '', description: '', corrective_action: '' }); }}>لغو</button>}</div>
    </section>

    <section className="panel green-head">

      <div className="panel-title-row"><h2>آخرین چله فعال هر ماشین</h2><button onClick={load}>بروزرسانی</button></div>

      <Table rows={activeRows} columns={[

        ['machine','ماشین'],

        ['shom_chelle','شماره چله'],

        ['chelle_weight','وزن چله kg'],

        ['pod_assigned','پود اختصاصی kg'],

        ['tar_used','مصرف تار kg'],

        ['pod_used','مصرف پود kg'],

        ['tar_remaining','مانده تار kg'],

        ['pod_remaining','مانده پود kg'],

        ['actual_waste','ضایعات واقعی kg'],

        ['total_weight','وزن پارچه kg'],

        ['remaining','مانده نخ kg'],

        ['remaining_percent','درصد مانده'],

        ['material_shortage','کسری ثبت مواد kg'],

        ['tarikh','آخرین بروزرسانی']

      ]} hideActions />

    </section>

    <section className="panel">

      <div className="panel-title-row"><h2>شاخص واقعی ضایعات به تفکیک ماشین و چله</h2><button onClick={load}>بروزرسانی</button></div>

      <Table rows={wasteRows} columns={[

        ['machine','ماشین'],

        ['shom_chelle','شماره چله'],

        ['total_meter','متراژ تولید'],

        ['total_weight','وزن پارچه kg'],

        ['actual_waste','وزن ضایعات واقعی kg'],

        ['waste_per_meter','ضایعات kg/m'],

        ['waste_per_kg','ضایعات kg/kg'],

        ['waste_percent_input','درصد ضایعات از ورودی مواد'],

        ['waste_percent_output','درصد ضایعات نسبت به خروجی'],

        ['tarikh','آخرین بروزرسانی']

      ]} hideActions />

    </section>

    <section className="panel">
      <h2>دفتر ثبت ضایعات</h2>
      <Table rows={wasteHistory} columns={[["tarikh","تاریخ"],["machine","ماشین"],["shom_chelle","چله"],["waste_type_fa","نوع"],["weight","وزن kg"],["reason","علت"],["operator_name","ثبت‌کننده"],["description","توضیحات"],["corrective_action","اقدام اصلاحی"]]} onEdit={editWaste} onDelete={deleteWaste} />
    </section>

  </Page>;

}

function MachineFormulas({ notify }) {

  return <CrudPage

    title="فرمول پیش‌فرض ماشین‌ها"

    endpoint="/formulas"

    empty={{ machine: '', tar_percent: 50, pod_percent: 50, tozih: '' }}

    notify={notify}

    filters={[['machine','ماشین']]}

    mapEdit={row => ({ id: row.id, machine: row.machine || '', tar_percent: Number(row.tar_percent || 0), pod_percent: Number(row.pod_percent || 0), tozih: row.tozih || '' })}

    renderForm={(form, set) => <>

      <Input label="شماره ماشین" value={form.machine} onChange={v => set('machine', v)} />

      <Input label="درصد پیش‌فرض تار" type="number" value={form.tar_percent} onChange={v => set('tar_percent', Number(v))} hint="فقط پیشنهاد اولیه برای چله جدید است و آمار تولیدهای قبلی را تغییر نمی‌دهد." />

      <Input label="درصد پیش‌فرض پود" type="number" value={form.pod_percent} onChange={v => set('pod_percent', Number(v))} hint="نسبت نهایی در سالن تولید برای هر چله و پارچه تأیید می‌شود." />

      <Input label="توضیحات" value={form.tozih} onChange={v => set('tozih', v)} />

    </>}

    columns={[['machine','ماشین'],['tar_percent','درصد تار'],['pod_percent','درصد پود'],['tozih','توضیحات']]}

  />;

}



function LoadingMobilePage({ token, session, onLogout }) {

  const videoRef = React.useRef(null);

  const controlsRef = React.useRef(null);

  const scanLockRef = React.useRef(false);

  const [state, setState] = useState(null);

  const [candidate, setCandidate] = useState(null);

  const [manualCode, setManualCode] = useState('');

  const [cameraOn, setCameraOn] = useState(false);

  const [busy, setBusy] = useState(false);

  const [error, setError] = useState('');

  const loadState = async () => {

    try { setState(await api(`/loading/${encodeURIComponent(token)}`)); setError(''); }

    catch (err) { setError(err.message); }

  };

  useEffect(() => {

    loadState();

    const timer = setInterval(loadState, 2000);

    return () => {

      clearInterval(timer);

      controlsRef.current?.stop();

    };

  }, [token]);

  const inspectCode = async rawCode => {

    const code = String(rawCode || '').trim();

    if (!code || scanLockRef.current) return;

    scanLockRef.current = true;

    setBusy(true); setError('');

    try {

      const result = await api(`/loading/${encodeURIComponent(token)}/scan`, { method: 'POST', body: { code } });

      controlsRef.current?.stop();

      controlsRef.current = null;

      setCameraOn(false);

      setCandidate(result.item);

      setManualCode('');

    } catch (err) {

      setError(err.message);

    } finally {

      setBusy(false);

      scanLockRef.current = false;

    }

  };

  const startCamera = async () => {

    setError(''); setCandidate(null);

    controlsRef.current?.stop();

    try {

      const reader = new BrowserMultiFormatReader();

      const controls = await reader.decodeFromConstraints({

        audio: false,

        video: { facingMode: { ideal: 'environment' } }

      }, videoRef.current, (result, err) => {

        if (result && !scanLockRef.current) {

          if (navigator.vibrate) navigator.vibrate(80);

          inspectCode(result.getText());

        }

      });

      controlsRef.current = controls;

      setCameraOn(true);

    } catch (err) {

      setCameraOn(false);

      setError('دوربین باز نشد. اجازه Camera را فعال کنید یا کد را دستی وارد کنید.');

    }

  };

  const confirm = async () => {

    if (!candidate?.id || candidate.matches === false) return;

    setBusy(true); setError('');

    try {

      await api(`/loading/${encodeURIComponent(token)}/confirm`, { method: 'POST', body: { code: String(candidate.id) } });

      setCandidate(null);

      await loadState();

      await startCamera();

    } catch (err) { setError(err.message); }

    finally { setBusy(false); }

  };

  const item = candidate || {};

  const rejectCandidate = async () => {

    setCandidate(null);

    await startCamera();

  };

  return <div className="loading-mobile-page" dir="rtl">

    <header className="loading-mobile-header">

      <div><h1>اسکن بارگیری فاکتور خروج</h1><p>کاربر: {session?.user?.username || session?.user?.name || '-'}</p></div>

      <button className="ghost" onClick={onLogout}>خروج و ورود کاربر دیگر</button>

    </header>

    {state?.session && <section className="loading-mobile-summary">

      <div><span>فاکتور</span><b>{state.session.invoice_no}</b></div>

      <div><span>مشتری</span><b>{state.session.customer}</b></div>

      <div><span>کالا</span><b>{state.session.kala}</b></div>

      <div><span>تعداد تأیید</span><b>{state.totals?.count || 0}</b></div>

    </section>}

    {error && <div className="error-box">{error}</div>}

    <section className="panel loading-scanner-card">

      <video ref={videoRef} className={cameraOn ? 'loading-camera active' : 'loading-camera'} muted playsInline />

      <div className="actions-row">

        <button className="primary" type="button" onClick={startCamera}>{cameraOn ? 'شروع مجدد دوربین' : 'باز کردن دوربین و اسکن بارکد'}</button>

      </div>

      <small className="loading-camera-hint">دوربین پشت گوشی به‌صورت زنده بارکد روی لیبل را می‌خواند. پس از تأیید یا رد هر طاقه، اسکن بعدی خودکار آغاز می‌شود.</small>

      <div className="loading-manual">

        <input inputMode="numeric" placeholder="یا کد طاقه را دستی وارد کنید" value={manualCode} onChange={e => setManualCode(e.target.value)} onKeyDown={e => e.key === 'Enter' && inspectCode(manualCode)} />

        <button type="button" onClick={() => inspectCode(manualCode)}>بررسی</button>

      </div>

    </section>

    {candidate && <section className={`panel loading-candidate ${item.matches === false ? 'mismatch' : 'match'}`}>

      <h2>{item.matches === false ? 'مغایرت مشخصات؛ تأیید مجاز نیست' : 'مشخصات لیبل را کنترل و تأیید کنید'}</h2>

      {item.mismatch_reason && <div className="error-box">{item.mismatch_reason}</div>}

      <div className="loading-detail-grid">

        <FieldView label="کد طاقه" value={item.id} /><FieldView label="کالا" value={item.kala} />

        <FieldView label="متراژ" value={item.metr} /><FieldView label="وزن" value={item.weight} />

        <FieldView label="ماشین" value={item.machine} /><FieldView label="شماره چله" value={item.shom_chelle} />

        <FieldView label="همبافت تار" value={item.ham_chelle} /><FieldView label="همبافت پود" value={item.ham_pod} />

      </div>

      <div className="actions-row">

        <button className="primary" disabled={busy || item.matches === false} onClick={confirm}>{busy ? 'در حال ثبت...' : 'تأیید و افزودن به فاکتور'}</button>

        <button className="ghost" onClick={rejectCandidate}>رد کردن و اسکن بعدی</button>

      </div>

    </section>}

    <section className="panel"><h2>طاقه‌های تأییدشده</h2>

      <Table rows={state?.items || []} columns={[["id","کد"],["metr","متراژ"],["weight","وزن"],["kala","کالا"],["confirmed_by","تأییدکننده"]]} hideActions />

      <div className="invoice-total"><b>جمع:</b><span>{fmt(state?.totals?.count || 0)} طاقه</span><span>{fmt(state?.totals?.metr || 0)} متر</span><span>{fmt(state?.totals?.weight || 0)} کیلو</span></div>

    </section>

  </div>;

}

function FieldView({ label, value }) {

  return <div><span>{label}</span><b>{display(value)}</b></div>;

}

function OutInvoicePro({ lookups, notify }) {

  const inputRef = React.useRef(null);

  const mobileCountRef = React.useRef(0);

  const mobileIdsRef = React.useRef(new Set());

  const [rows, setRows] = useState([]);

  const [filters, setFilters] = useState({});

  const [form, setForm] = useState({ invoice_no: '', sanad_no: '', customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' });

  const [editing, setEditing] = useState(false);

  const [loadingSession, setLoadingSession] = useState(null);

  const [loadingQr, setLoadingQr] = useState('');

  const [loadingBusy, setLoadingBusy] = useState(false);

  const [mobilePanelOpen, setMobilePanelOpen] = useState(false);

  const [mobileItems, setMobileItems] = useState([]);

  const load = async () => setRows(await api('/out-invoice'));

  useEffect(() => { load(); api('/out-invoice/next-sanad').then(x => setForm(s => ({ ...s, sanad_no: x.sanad_number || '' }))).catch(() => {}); }, []);

  useEffect(() => {

    if (!loadingSession?.token) return undefined;

    let cancelled = false;

    const sync = async () => {

      try {

        const state = await api(`/loading/${encodeURIComponent(loadingSession.token)}`);

        if (!cancelled) {

          const incoming = state.items || [];

          setMobileItems(incoming);

          setForm(current => {

            const incomingIds = new Set(incoming.map(item => String(item.id)));

            const merged = current.items.filter(item => !mobileIdsRef.current.has(String(item.id)) || incomingIds.has(String(item.id)));

            incoming.forEach(item => {

              const index = merged.findIndex(existing => String(existing.id) === String(item.id));

              if (index >= 0) merged[index] = { ...merged[index], ...item };

              else merged.push(item);

            });

            mobileIdsRef.current = incomingIds;

            return { ...current, items: merged };

          });

          if (incoming.length > mobileCountRef.current) notify(`${incoming.length - mobileCountRef.current} طاقه جدید از موبایل به فاکتور اضافه شد`);

          mobileCountRef.current = incoming.length;

        }

      } catch (err) {

        if (!cancelled && !String(err.message || '').includes('پایان')) notify(err.message);

      }

    };

    sync();

    const timer = setInterval(sync, 2000);

    return () => { cancelled = true; clearInterval(timer); };

  }, [loadingSession?.token]);

  const set = (k, v) => setForm(s => ({ ...s, [k]: v }));

  const addTaghe = async () => {

    const code = String(form.taghe_code || '').trim();

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

  const removeItem = async (row) => {

    const id = typeof row === 'object' ? row.id : row;

    if (loadingSession?.token) {

      await api(`/loading/${encodeURIComponent(loadingSession.token)}/items/${encodeURIComponent(id)}`, { method: 'DELETE' }).catch(err => notify(err.message));

      setMobileItems(items => {

        const next = items.filter(x => String(x.id) !== String(id));

        mobileCountRef.current = next.length;

        return next;

      });

    }

    setForm(s => ({ ...s, items: s.items.filter(x => String(x.id) !== String(id)) }));

  };

  const stopLoading = async () => {

    const token = loadingSession?.token;

    setLoadingSession(null); setLoadingQr('');

    if (token) await api(`/out-invoice/loading/${encodeURIComponent(token)}`, { method: 'DELETE' }).catch(() => {});

  };

  const startLoading = async () => {

    if (!String(form.invoice_no || '').trim() || !String(form.customer || '').trim() || !String(form.kala || '').trim()) {

      notify('برای ساخت QR، شماره فاکتور، مشتری و نام کالا را وارد کنید');

      return;

    }

    setLoadingBusy(true);

    try {

      if (loadingSession?.token) await stopLoading();

      setMobileItems([]);

      mobileCountRef.current = 0;

      mobileIdsRef.current = new Set();

      const result = await api('/out-invoice/loading', { method: 'POST', body: { invoice_no: form.invoice_no, sanad_no: form.sanad_no, customer: form.customer, kala: form.kala, items: form.items.map(item => String(item.id)), old_invoice_no: form.old_invoice_no } });

      setLoadingSession(result);

      setLoadingQr(await QRCode.toDataURL(result.url, { width: 420, margin: 2, errorCorrectionLevel: 'M' }));

      notify('QR بارگیری ساخته شد؛ آن را با دوربین موبایل اسکن کنید');

    } catch (err) { notify(err.message); }

    finally { setLoadingBusy(false); }

  };

  const clearInvoice = () => {

    stopLoading();

    setMobilePanelOpen(false);

    setMobileItems([]);

    mobileCountRef.current = 0;

    mobileIdsRef.current = new Set();

    setForm({ invoice_no: '', sanad_no: form.sanad_no, customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' });

    setEditing(false);

    setTimeout(() => inputRef.current?.focus(), 30);

  };

  const save = async () => {

    await api('/out-invoice', { method: 'POST', body: { invoice_no: form.invoice_no, sanad_no: form.sanad_no, customer: form.customer, kala: form.kala, items: form.items.map(x => String(x.id)), old_invoice_no: form.old_invoice_no, loading_session_token: loadingSession?.token || '' } });

    setLoadingSession(null); setLoadingQr('');

    setMobilePanelOpen(false); setMobileItems([]); mobileCountRef.current = 0; mobileIdsRef.current = new Set();

    setForm({ invoice_no: '', sanad_no: form.sanad_no, customer: '', kala: '', taghe_code: '', items: [], old_invoice_no: '' });

    setEditing(false);

    const next = await api('/out-invoice/next-sanad').catch(() => null);

    if (next?.sanad_number) setForm(s => ({ ...s, sanad_no: next.sanad_number }));

    await load();

    notify(editing ? 'فاکتور خروج ویرایش شد' : 'فاکتور خروج ثبت شد');

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

        <Select label="مشتری" value={form.customer} onChange={v => set('customer', v)} items={(lookups.customers || []).map(x => ({ id: x.name, name: x.name }))} />

        <Select label="نام کالا" value={form.kala} onChange={v => set('kala', v)} items={(lookups.fabrics || []).map(x => ({ id: x.name, name: x.name }))} />

        <label><span>کد طاقه / بارکد</span><input ref={inputRef} value={form.taghe_code} onChange={e => set('taghe_code', e.target.value)} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addTaghe(); } }} /></label>

        <label><span>&nbsp;</span><button type="button" onClick={addTaghe}>افزودن طاقه</button></label>

      </div>

      <div className="mobile-feature-toggle">

        <button className={mobilePanelOpen ? 'ghost' : 'primary'} type="button" onClick={async () => {

          if (mobilePanelOpen) { await stopLoading(); setMobilePanelOpen(false); }

          else setMobilePanelOpen(true);

        }}>{mobilePanelOpen ? 'بستن بارگیری با موبایل' : 'بارگیری با موبایل (اختیاری)'}</button>

        <small>در بارگیری دستی نیازی به فعال‌کردن این بخش نیست.</small>

      </div>

      {mobilePanelOpen && <section className="invoice-mobile-sync">

        <div className="invoice-mobile-copy">

          <h2>بارکدخوان موبایل برای فاکتور خروج</h2>

          <p>پس از تکمیل شماره فاکتور، مشتری و کالا، QR را بسازید. کارمند QR را با دوربین گوشی باز می‌کند، با نام کاربری خودش وارد می‌شود و بارکد لیبل طاقه‌ها را یکی‌یکی تأیید می‌کند.</p>

          <div className="actions-row">

            <button className="primary" type="button" disabled={loadingBusy} onClick={startLoading}>{loadingBusy ? 'در حال ساخت...' : loadingSession ? 'ساخت QR جدید' : 'ساخت QR اتصال موبایل'}</button>

            {loadingSession && <button className="ghost" type="button" onClick={stopLoading}>پایان اتصال موبایل</button>}

          </div>

          {loadingSession && <small>لیست این فاکتور هر ۲ ثانیه با تأییدهای موبایل به‌روز می‌شود. تاکنون {fmt(mobileItems.length)} طاقه از موبایل تأیید شده است.</small>}

          {mobileItems.length > 0 && <div className="mobile-confirmed-list">

            <b>آخرین رکوردهای تأییدشده در موبایل</b>

            <Table rows={mobileItems.map((x, i) => ({ ...x, row: i + 1 }))} columns={[["row","ردیف"],["id","کد طاقه"],["metr","متراژ"],["weight","وزن"],["confirmed_by","کاربر موبایل"]]} hideActions />

          </div>}

        </div>

        {loadingQr && <div className="invoice-mobile-qr"><img src={loadingQr} alt="QR اتصال بارکدخوان موبایل" /><a href={loadingSession.url} target="_blank" rel="noreferrer">باز کردن صفحه موبایل برای تست</a></div>}

      </section>}

      <section className="invoice-taghe-list">

        <div className="invoice-taghe-head"><h2>لیست طاقه‌های خروجی</h2></div>

        <Table rows={form.items.map((x, i) => ({ ...x, row: i + 1, entry_source: x.confirmed_by ? `موبایل (${x.confirmed_by})` : 'ثبت دستی' }))} columns={[['row','ردیف'],['id','کد طاقه'],['metr','متراژ (m)'],['weight','وزن (kg)'],['ham_chelle','همبافت تار'],['ham_pod','همبافت پود'],['shom_chelle','شماره چله'],['kala','کالا'],['entry_source','روش ثبت']]} onEdit={() => {}} onDelete={removeItem} />

        <div className="invoice-total"><b>جمع کل:</b><span>{fmt(form.items.length)} عدد طاقه</span><span>{fmt(totalMetr)} متر</span><span>{fmt(totalWeight)} kg</span></div>

      </section>

      <div className="actions-row invoice-actions">

        <button onClick={async () => printReport('موجودی طاقه‌های خروج‌نخورده', await api('/out-invoice/stock'), [['id','کد طاقه'],['tarikh','تاریخ'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['machine','ماشین'],['shom_chelle','چله']])}>گزارش موجودی انبار</button>

        <button onClick={async () => exportExcel('موجودی طاقه‌های خروج‌نخورده', await api('/out-invoice/stock'), [['id','کد طاقه'],['tarikh','تاریخ'],['kala','کالا'],['metr','متراژ'],['weight','وزن'],['machine','ماشین'],['shom_chelle','چله']])}>اکسل موجودی انبار</button>

        <button className="printer-picker" title="پنجره چاپ ویندوز/مرورگر باز می‌شود و می‌توانید چاپگر A4 را انتخاب کنید" onClick={() => printInvoiceTaghes(form)}>انتخاب چاپگر و چاپ فاکتور</button>

        <button onClick={() => exportExcel('لیست طاقه‌های فاکتور خروج', form.items.map((x, i) => ({ ...x, row: i + 1 })), [['row','ردیف'],['id','کد طاقه'],['metr','متراژ'],['weight','وزن'],['ham_chelle','همبافت تار'],['ham_pod','همبافت پود'],['shom_chelle','شماره چله'],['kala','کالا']])}>اکسل فاکتور</button>

        <button onClick={() => printReport('گزارش فاکتور خروج', visible, invoiceCols)}>گزارش فاکتورها</button>

        <button onClick={() => exportExcel('گزارش فاکتور خروج', visible, invoiceCols)}>اکسل فاکتورها</button>

        <button className="primary" onClick={save}>اتمام فاکتور و ذخیره</button>

        <button className="ghost" onClick={clearInvoice}>پاک کردن فاکتور</button>

      </div>

    </section>

    <section className="panel">

      <h2>فاکتورهای ثبت شده قبلی</h2>

      <Filters filters={[['invoice_no','شماره فاکتور'],['mosh','مشتری'],['kala','نام کالا'],['sanad','شماره سند']]} rows={rows} values={filters} setValues={setFilters} onPrint={() => printReport('فاکتورهای ثبت شده قبلی', visible, invoiceCols)} onExcel={() => exportExcel('فاکتورهای ثبت شده قبلی', visible, invoiceCols)} />

      <Table rows={visible} columns={invoiceCols} onEdit={edit} onDelete={del} />

    </section>

  </Page>;

}



function ReportTable({ title, rows = [], columns, filterKeys }) {

  const [values, setValues] = useState({});

  const defs = (filterKeys || columns.slice(0, 3).map(([key, label]) => [key, label]));

  const visible = filterRows(rows, values);

  return <section className="panel report-card">

    <h2>{title}</h2>

    <Filters filters={defs} rows={rows} values={values} setValues={setValues} onPrint={() => printReport(title, visible, columns)} onExcel={() => exportExcel(title, visible, columns)} />

    <Table rows={visible} columns={columns} hideActions />

  </section>;

}



function ReportsPro2() {

  const [data, setData] = useState({ yarnOut: [], invoices: [], expenses: [], management: {} });

  useEffect(() => {

    Promise.all([api('/nakh-khor'), api('/out-invoice'), api('/expenses'), api('/management-report')]).then(([yarnOut, invoices, expenses, management]) => setData({ yarnOut, invoices, expenses, management })).catch(() => {});

  }, []);

  const management = data.management || {};
  const today = management.today || {};
  const month = management.month || {};
  const stock = management.stock || {};

  return <Page title="گزارشات عملیاتی">

    <div className="metrics">
      {[
        ['تولید امروز', fmt(today.metr), 'متر'],
        ['وزن تولید امروز', fmt(today.weight), 'کیلو'],
        ['تولید ماه', fmt(month.metr), 'متر'],
        ['وزن تولید ماه', fmt(month.weight), 'کیلو'],
        ['طاقه آماده خروج', fmt(stock.total_taghe), 'طاقه'],
        ['وزن پارچه موجود', fmt(stock.total_weight), 'کیلو'],
      ].map(([title, value, unit]) => <div className="metric" key={title}><span>{title}</span><b>{value}</b><small>{unit}</small></div>)}
    </div>

    <section className="panel report-card">
      <h2>هشدارهای مدیریتی و کنترلی</h2>
      {(management.notifications || []).length ? <div className="notice-list">{management.notifications.map((x, i) => <div key={`${x.code || 'n'}-${i}`}><b>{x.title}</b><span>{x.message}</span></div>)}</div> : <div className="empty">هشدار فعالی وجود ندارد.</div>}
    </section>

    <ReportTable title="کنترل کیفیت داده‌های سیکل" rows={management.data_quality || []} columns={[["title","کنترل"],["count","تعداد مورد"],["status","وضعیت"]]} />

    <ReportTable title="دفتر موجودی نخ انبار" rows={management.yarn_inventory || []} columns={[["mosh","مالک نخ"],["hambaft","هم‌بافت"],["yarn","نوع نخ"],["vorud","ورود kg"],["to_salon","خالص ارسال سالن kg"],["khoroj","سایر خروج kg"],["inventory","مانده انبار kg"]]} />

    <ReportTable title="مانده نخ نزد چله‌پیچ" rows={management.warper_balances || []} columns={[["warper","چله‌پیچ"],["owner","مالک نخ"],["hambaft","هم‌بافت"],["yarn","نوع نخ"],["sent_weight","ارسال kg"],["returned_weight","چله برگشتی kg"],["balance_weight","مانده kg"],["chelle_count","تعداد چله"],["last_sent_date","آخرین ارسال"],["last_return_date","آخرین برگشت"]]} />

    <ReportTable title="وضعیت چله و مواد روی ماشین" rows={management.machines || []} columns={[["machine","ماشین"],["shom_chelle","چله"],["chelle_weight","تار اولیه kg"],["pod_assigned","پود تخصیصی kg"],["tar_used","تار مصرفی kg"],["pod_used","پود مصرفی kg"],["tar_remaining","مانده تار kg"],["pod_remaining","مانده پود kg"],["remaining","مانده کل kg"],["actual_waste","ضایعات واقعی kg"],["remaining_percent","درصد مانده"],["material_shortage","کسری مواد kg"]]} />

    <ReportTable title="تولید ماه به تفکیک ماشین" rows={management.month_by_machine || []} columns={[["machine","ماشین"],["pieces","طاقه"],["metr","متراژ"],["weight","وزن kg"]]} />

    <ReportTable title="دفتر ضایعات واقعی" rows={management.waste || []} columns={[["tarikh","تاریخ"],["machine","ماشین"],["shom_chelle","چله"],["waste_type","نوع"],["weight","وزن kg"],["reason","علت"],["operator_name","ثبت‌کننده"],["description","توضیحات"],["corrective_action","اقدام اصلاحی"]]} />

    <ReportTable title="موجودی پارچه آماده خروج" rows={stock.items || []} columns={[["kala","کالا"],["taghe_count","تعداد طاقه"],["metr","متراژ"],["weight","وزن kg"]]} />

    <ReportTable title="فاکتورهای خروج" rows={management.out_invoices || []} columns={[["invoice_no","شماره فاکتور"],["tarikh","تاریخ"],["mosh","مشتری"],["sanad","شماره سند"],["kala","کالا"],["taghe_count","تعداد طاقه"]]} />

    <ReportTable title="گزارش فاکتور خروج" rows={data.invoices} columns={[['tarikh','تاریخ'],['invoice_no','شماره فاکتور'],['sanad','شماره سند'],['mosh','مشتری'],['kala','کالا'],['taghe_count','تعداد طاقه'],['metr','متراژ'],['weight','وزن']]} />

    <ReportTable title="گزارش خروج نخ" rows={data.yarnOut} columns={[['tarikh','تاریخ'],['owner_mosh','مالک نخ'],['hambaft','همبافت'],['nakh','نخ'],['weight','وزن'],['mosh','مقصد']]} />

    <ReportTable title="گزارش هزینه‌ها" rows={data.expenses} columns={[['tarikh','تاریخ'],['onvan_hazine','عنوان هزینه'],['operator_name','ثبت کننده'],['weaver_name','بافنده'],['mablagh','مبلغ'],['shomare_sanad','شماره سند'],['tozih','توضیحات']]} />

  </Page>;

}


function OperationalAdvisor() {

  const [data, setData] = useState({});

  const [loading, setLoading] = useState(true);

  const [error, setError] = useState('');

  const load = async () => {

    setLoading(true);

    setError('');

    try {

      setData(await api('/advisor'));

    } catch (err) {

      setError(err.message || 'تحلیل عملیات دریافت نشد.');

    } finally {

      setLoading(false);

    }

  };

  useEffect(() => { load(); }, []);

  const analysis = useMemo(() => buildOperationalAdvice(data), [data]);

  const today = data.today || {};

  const month = data.month || {};

  const stock = data.stock || {};

  return <Page title="تحلیل و مشاور هوشمند">

    <section className="panel advisor-hero">

      <div>

        <span className="advisor-kicker">دستیار تصمیم‌یار عملیات</span>

        <h2>{analysis.headline}</h2>

        <p>تحلیل با داده‌های همین شرکت و قواعد کنترلی تولید انجام می‌شود؛ هیچ داده‌ای برای سرویس خارجی ارسال نمی‌شود.</p>

      </div>

      <div className={`advisor-score ${analysis.score < 55 ? 'danger' : analysis.score < 80 ? 'warn' : 'ok'}`}>

        <span>امتیاز سلامت عملیات</span>

        <strong>{fmt(analysis.score)}</strong>

        <small>از ۱۰۰</small>

      </div>

      <button onClick={load} disabled={loading}>{loading ? 'در حال تحلیل...' : 'به‌روزرسانی تحلیل'}</button>

    </section>

    {error && <div className="error-box">{error}</div>}

    <div className="metrics advisor-metrics">

      {[

        ['تولید امروز', today.metr, 'متر'],

        ['تولید ماه', month.metr, 'متر'],

        ['طاقه آماده خروج', stock.total_taghe, 'طاقه'],

        ['ریسک فوری', analysis.criticalCount, 'مورد'],

        ['نیازمند توجه', analysis.warningCount, 'مورد'],

        ['خطای کیفیت داده', analysis.dataIssueCount, 'رکورد'],

      ].map(([title, value, unit]) => <div className="metric" key={title}><span>{title}</span><b>{fmt(value)}</b><small>{unit}</small></div>)}

    </div>

    <div className="advisor-grid">

      <section className="panel advisor-actions">

        <div className="panel-title-row"><h2>اقدام‌های پیشنهادی به ترتیب اولویت</h2><span>{analysis.actions.length} پیشنهاد</span></div>

        {analysis.actions.length ? analysis.actions.map((item, index) => <article className={`advisor-action ${item.level}`} key={`${item.code}-${index}`}>

          <div className="advisor-action-rank">{fmt(index + 1)}</div>

          <div><h3>{item.title}</h3><p>{item.message}</p><small>{item.source}</small></div>

          <b>{item.level === 'critical' ? 'فوری' : item.level === 'warning' ? 'مهم' : 'پیشنهاد'}</b>

        </article>) : <div className="empty">در حال حاضر اقدام اصلاحی فوری شناسایی نشد.</div>}

      </section>

      <section className="panel advisor-summary">

        <h2>جمع‌بندی مدیریتی</h2>

        <div><span>وضعیت امروز</span><b>{Number(today.metr || 0) > 0 ? 'تولید ثبت شده است' : 'تولید امروز ثبت نشده'}</b></div>

        <div><span>وضعیت انبار محصول</span><b>{Number(stock.total_taghe || 0) > 30 ? 'نیازمند برنامه خروج' : 'در محدوده کنترل'}</b></div>

        <div><span>کیفیت داده‌ها</span><b>{analysis.dataIssueCount ? 'نیازمند اصلاح' : 'قابل اتکا'}</b></div>

        <div><span>زمان تحلیل</span><b>{data.date || todayJalali()}</b></div>

        <p className="advisor-disclaimer">این بخش ابزار پشتیبان تصمیم است. اقدام نهایی، توقف ماشین یا سفارش مواد باید با تأیید مدیر انجام شود.</p>

      </section>

    </div>

    <section className="panel report-card">

      <h2>وضعیت ماشین‌ها و پیشنهاد کنترل</h2>

      <Table rows={analysis.machineRows} columns={[["machine","ماشین"],["shom_chelle","چله"],["remaining_percent","مانده مواد %"],["material_shortage","کسری مواد kg"],["waste_percent_input","ضایعات %"],["advice","اقدام پیشنهادی"]]} hideActions />

    </section>

    <section className="panel report-card">

      <h2>کنترل کیفیت داده‌های مبنای تحلیل</h2>

      <Table rows={data.data_quality || []} columns={[["title","کنترل"],["count","تعداد مورد"],["status","وضعیت"]]} hideActions />

    </section>

  </Page>;

}



function buildOperationalAdvice(data = {}) {

  const notifications = Array.isArray(data.notifications) ? data.notifications : [];

  const quality = Array.isArray(data.data_quality) ? data.data_quality : [];

  const machines = Array.isArray(data.machines) ? data.machines : [];

  const actions = notifications.map((item, index) => ({

    code: item.code || `notification-${index}`,

    title: item.title || 'کنترل عملیات',

    message: item.message || 'این مورد نیازمند بررسی مدیر عملیات است.',

    source: 'هشدار کنترلی سیستم',

    level: ['critical', 'warning'].includes(item.type) ? item.type : 'info',

  }));

  const qualityIssues = quality.filter(item => Number(item.count || 0) > 0);

  qualityIssues.forEach(item => actions.push({

    code: `quality-${item.code || item.title}`,

    title: `اصلاح داده: ${item.title}`,

    message: `${fmt(item.count)} رکورد باعث کاهش دقت گزارش و تصمیم‌گیری شده است.`,

    source: 'کنترل کیفیت داده',

    level: 'warning',

  }));

  const todayMetr = Number(data.today?.metr || 0);

  const monthMetr = Number(data.month?.metr || 0);

  const stockCount = Number(data.stock?.total_taghe || 0);

  if (todayMetr <= 0 && monthMetr > 0) actions.push({

    code: 'today-production',

    title: 'بررسی توقف یا ثبت‌نشدن تولید امروز',

    message: 'برای امروز متراژ تولید ثبت نشده است؛ وضعیت ماشین‌ها و ثبت اپراتورها کنترل شود.',

    source: 'مقایسه تولید روز و ماه',

    level: 'warning',

  });

  if (stockCount > 0 && !actions.some(item => item.code === 'stock-unshipped')) actions.push({

    code: 'stock-plan',

    title: 'برنامه‌ریزی خروج محصول آماده',

    message: `${fmt(stockCount)} طاقه آماده خروج است؛ سفارش مشتری و ظرفیت بارگیری با انبار تطبیق داده شود.`,

    source: 'موجودی پارچه آماده خروج',

    level: stockCount > 30 ? 'warning' : 'info',

  });

  if (!machines.length) actions.push({

    code: 'machine-data',

    title: 'تکمیل وضعیت ماشین‌ها',

    message: 'ماشین فعال یا چله جاری برای تحلیل مصرف مواد ثبت نشده است.',

    source: 'داده‌های سالن تولید',

    level: 'info',

  });

  const priority = { critical: 0, warning: 1, info: 2 };

  actions.sort((a, b) => priority[a.level] - priority[b.level]);

  const criticalCount = actions.filter(item => item.level === 'critical').length;

  const warningCount = actions.filter(item => item.level === 'warning').length;

  const dataIssueCount = qualityIssues.reduce((sum, item) => sum + Number(item.count || 0), 0);

  const score = Math.max(0, Math.min(100, 100 - criticalCount * 18 - warningCount * 7 - Math.min(dataIssueCount, 10) * 3));

  const machineRows = machines.map(machine => {

    const shortage = Number(machine.material_shortage || 0);

    const remaining = Number(machine.remaining_percent || 0);

    const waste = Number(machine.waste_percent_input || 0);

    let advice = 'ادامه تولید و پایش عادی';

    if (shortage > 0) advice = 'کنترل فوری کسری ثبت مواد';

    else if (remaining <= 10) advice = 'آماده‌سازی فوری مواد یا چله بعدی';

    else if (remaining <= 25) advice = 'برنامه‌ریزی تأمین مواد';

    else if (waste > 5) advice = 'بررسی علت ضایعات و اقدام اصلاحی';

    return { ...machine, advice };

  });

  const headline = criticalCount

    ? `${fmt(criticalCount)} ریسک فوری در عملیات نیازمند تصمیم است`

    : warningCount

      ? `${fmt(warningCount)} مورد مهم برای بهبود عملیات شناسایی شد`

      : 'عملیات در وضعیت پایدار و قابل کنترل است';

  return { actions, criticalCount, warningCount, dataIssueCount, score, machineRows, headline };

}



function salonEmpty() {

  return { metr: '', weight: '', machine: '', kala_id: '', ham_pod: '', ham_chelle: '', shom_chelle: '', chelle_id: '', user: 'admin', tar_percent: 50, pod_percent: 50, skip_print: false };

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

      <Filters filters={filters} rows={items} values={filterValues} setValues={setFilterValues} onPrint={() => printReport(`لیست ${title}`, visible, columns)} onExcel={() => exportExcel(`لیست ${title}`, visible, columns)} />

      {loading ? <div className="empty">در حال بارگذاری...</div> : <Table rows={visible} columns={columns} onEdit={edit} onDelete={del} />}

    </section>

    {typeof extraSections === 'function' ? extraSections({ items, visible, load }) : extraSections}

  </Page>;

}



function Filters({ filters, values, setValues, onPrint, onExcel, rows = [] }) {

  if (!filters?.length) return <div className="actions-row report-actions"><button onClick={onPrint}>چاپ گزارش</button>{onExcel && <button onClick={onExcel}>خروجی اکسل</button>}</div>;

  return <div className="filters">

    {filters.map(([key, label]) => <label key={key}><span>{label}</span><select value={values[key] || ''} onChange={e => setValues(s => ({ ...s, [key]: e.target.value }))}>

      <option value="">همه</option>

      {filterOptions(rows, key).map(x => <option key={x} value={x}>{x}</option>)}

    </select></label>)}

    <button onClick={() => setValues({})}>پاک کردن فیلتر</button>

    <button onClick={onPrint}>چاپ گزارش</button>

    {onExcel && <button onClick={onExcel}>خروجی اکسل</button>}

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



function Input({ label, value, onChange, onBlur, type = 'text', list, disabled = false, hint = '' }) {

  const id = `list-${label.replaceAll(' ', '-')}`;

  return <label><span>{label}</span><input type={type} value={value ?? ''} disabled={disabled} onChange={e => onChange(e.target.value)} onBlur={onBlur ? e => onBlur(e.target.value) : undefined} list={list ? id : undefined} />{list && <datalist id={id}>{list.map(x => <option key={x} value={x} />)}</datalist>}{hint && <small className="field-hint">{hint}</small>}</label>;

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



function printReport(title, rows, columns) {

  const head = columns.map(c => `<th>${safe(c[1])}</th>`).join('');

  const body = rows.map(r => `<tr>${columns.map(c => `<td>${safe(display(r[c[0]]))}</td>`).join('')}</tr>`).join('');

  const html = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="UTF-8"><title>${safe(title)}</title><style>body{font-family:Tahoma;padding:20px;color:#111}h1{text-align:center}table{width:100%;border-collapse:collapse}th,td{border:1px solid #999;padding:8px;text-align:right}th{background:#e5e7eb}@media print{button{display:none}}</style></head><body><button onclick="window.print()">چاپ</button><h1>${safe(title)}</h1><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></body></html>`;

  const win = window.open('', '_blank', 'width=1100,height=800');

  win.document.write(html);

  win.document.close();

  setTimeout(() => win.print(), 300);

}

function exportExcel(title, rows = [], columns = []) {

  const xmlText = value => String(value ?? '')
    .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F]/g, '')
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&apos;');

  const cell = value => {

    const numeric = typeof value === 'number' && Number.isFinite(value);

    return `<Cell><Data ss:Type="${numeric ? 'Number' : 'String'}">${xmlText(value)}</Data></Cell>`;

  };

  const header = `<Row>${columns.map(([, label]) => `<Cell ss:StyleID="Header"><Data ss:Type="String">${xmlText(label)}</Data></Cell>`).join('')}</Row>`;

  const body = rows.map(row => `<Row>${columns.map(([key]) => cell(row?.[key])).join('')}</Row>`).join('');

  const xml = `<?xml version="1.0" encoding="UTF-8"?><?mso-application progid="Excel.Sheet"?><Workbook xmlns="urn:schemas-microsoft-com:office:spreadsheet" xmlns:x="urn:schemas-microsoft-com:office:excel" xmlns:ss="urn:schemas-microsoft-com:office:spreadsheet"><Styles><Style ss:ID="Default" ss:Name="Normal"><Alignment ss:Horizontal="Right"/><Font ss:FontName="Tahoma"/></Style><Style ss:ID="Header"><Font ss:FontName="Tahoma" ss:Bold="1"/><Interior ss:Color="#DDEBF7" ss:Pattern="Solid"/></Style></Styles><Worksheet ss:Name="${xmlText(String(title || 'گزارش').slice(0, 31))}"><Table>${header}${body}</Table><WorksheetOptions xmlns="urn:schemas-microsoft-com:office:excel"><DisplayRightToLeft/></WorksheetOptions></Worksheet></Workbook>`;

  const url = URL.createObjectURL(new Blob([xml], { type: 'application/vnd.ms-excel;charset=utf-8' }));

  const link = document.createElement('a');

  link.href = url;

  link.download = `${String(title || 'گزارش').replace(/[\\/:*?"<>|]/g, '-').trim() || 'گزارش'}.xls`;

  document.body.appendChild(link);

  link.click();

  link.remove();

  setTimeout(() => URL.revokeObjectURL(url), 1000);

}



function printInvoiceTaghes(form) {

  const rows = (form.items || []).map((x, i) => ({ ...x, row: i + 1 }));

  const totalMetr = rows.reduce((s, x) => s + Number(x.metr || 0), 0);

  const totalWeight = rows.reduce((s, x) => s + Number(x.weight || 0), 0);

  const columns = [['row','ردیف'],['id','کد طاقه'],['metr','متراژ'],['weight','وزن'],['ham_chelle','همبافت تار'],['ham_pod','همبافت پود'],['shom_chelle','شماره چله'],['kala','کالا']];

  const head = columns.map(c => `<th>${safe(c[1])}</th>`).join('');

  const body = rows.map(r => `<tr>${columns.map(c => `<td>${safe(display(r[c[0]]))}</td>`).join('')}</tr>`).join('');

  const meta = [

    ['شماره سند', form.sanad_no],

    ['شماره فاکتور', form.invoice_no],

    ['مشتری', form.customer],

    ['نام کالا', form.kala],

    ['تعداد طاقه', rows.length],

    ['جمع متراژ', totalMetr],

    ['جمع وزن', totalWeight]

  ];

  const html = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="UTF-8"><title>لیست طاقه‌های خروجی</title><style>body{font-family:Tahoma;padding:20px;color:#111}h1{text-align:center}.meta{display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin:16px 0}.meta div{border:1px solid #999;padding:8px;border-radius:6px}table{width:100%;border-collapse:collapse}th,td{border:1px solid #999;padding:8px;text-align:center}th{background:#e5e7eb}.total{background:#dcfce7;font-weight:900}@media print{button{display:none}}</style></head><body><button onclick="window.print()">چاپ</button><h1>لیست طاقه‌های خروجی</h1><div class="meta">${meta.map(([k,v]) => `<div><b>${safe(k)}:</b> ${safe(display(v))}</div>`).join('')}</div><table><thead><tr>${head}</tr></thead><tbody>${body}<tr class="total"><td colspan="2">جمع کل</td><td>${safe(display(totalMetr))}</td><td>${safe(display(totalWeight))}</td><td colspan="4">${safe(display(rows.length))} عدد طاقه</td></tr></tbody></table></body></html>`;

  const win = window.open('', '_blank', 'width=1100,height=800');

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



createRoot(document.getElementById('root')).render(<App />);
