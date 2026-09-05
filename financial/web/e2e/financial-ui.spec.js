import { test, expect } from '@playwright/test';
import { financialPageIds } from '../src/audit/financialIntegrity.js';

// UI fixtures do not contact production. Actual PostgreSQL commit/replay/tenant
// isolation is covered separately by tests/integration/supervisor_persistence.
async function fixture(page) {
  let revision = 1;
  let state = { accounts: [{ id: 'test-bank', name: 'بانک آزمایشی', opening: 1000, type: 'بانک' }], incomingInvoices: [], expenses: [], movements: [], invoices: [], receivableDocs: [], payableDocs: [], ownedInventory: [] };
  const requests = [];
  await page.addInitScript(({ permissions }) => {
    if (window !== window.top) return;
    window.ERP_FINANCIAL_API = '/api';
    localStorage.setItem('financial-auth-token', 'ui-test-token');
    localStorage.setItem('financial-auth-profile', JSON.stringify({ name: 'حسابرس آزمایشی', permissions, portalLinked: true, portalRole: 'accountant' }));
  }, { permissions: financialPageIds });
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url());
    requests.push({ path: url.pathname, method: request.method() });
    let data = { rows: [], transactions: [], recipients: [], accounts: [], periods: [], vouchers: [], trialBalance: [], generalLedger: [] };
    if (url.pathname === '/api/workspace') {
      if (request.method() === 'PUT') { state = request.postDataJSON().state; revision++; }
      data = { state, revision };
    }
    if (url.pathname === '/api/supervisor/report') data = { revision, checkedAt: new Date().toISOString(), complete: true, findings: [], checked: 1, coverage: ['آزمون نمایشی'], backgroundCheckedAt: new Date().toISOString() };
    if (url.pathname === '/api/supervisor/preview') data = { revision, approval: 'fixture-approval', lines: [{ accountName: 'موجودی نخ', party: '', debit: 200, credit: 0 }, { accountName: 'بدهی فروشنده', party: 'فروشنده آزمایشی', debit: 0, credit: 200 }], findings: [] };
    if (url.pathname === '/api/supervisor/commit') { state = request.postDataJSON().state; revision++; data = { state, revision }; }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(data) });
  });
  return { requests, state: () => state };
}

test('every permitted finance tab renders without a JavaScript crash', async ({ page }) => {
  const failures = [];
  page.on('pageerror', error => failures.push(error.message));
  await fixture(page);
  for (const id of financialPageIds) {
    await page.goto('/?page=' + id);
    await expect(page.locator('aside')).toBeVisible();
    await expect(page.locator('main')).toBeVisible();
    await expect(page.locator('main')).not.toBeEmpty();
    expect(failures, 'page ' + id).toEqual([]);
  }
});

test('incoming form uses Persian date, previews without write, then saves exact reviewed invoice', async ({ page }) => {
  const mock = await fixture(page);
  await page.goto('/?page=incomingInvoices');
  await page.getByPlaceholder('انتخاب یا درج شخص').fill('فروشنده آزمایشی');
  await page.getByPlaceholder('انتخاب یا درج نوع نخ').fill('نخ آزمایشی');
  await page.getByPlaceholder('مقدار', { exact: true }).fill('2');
  await page.getByPlaceholder('نرخ واحد', { exact: true }).fill('100');
  const date = page.locator('form input').first();
  await expect(date).not.toHaveAttribute('type', 'date');
  await date.fill('1405/06/10');
  await expect(date).toHaveValue('1405/06/10');
  await page.getByRole('button', { name: 'بررسی ناظر مالی و نمایش اثر ثبت' }).click();
  await expect(page.getByText('اثر خالص ثبت پیشنهادی — هنوز ذخیره نشده است')).toBeVisible();
  expect(mock.state().incomingInvoices).toHaveLength(0);
  expect(mock.requests.filter(x => x.method === 'PUT')).toHaveLength(0);
  await page.screenshot({ path: 'test-results/incoming-review.png', fullPage: true });
  await page.getByRole('button', { name: 'تأیید نهایی و ثبت روی سرور' }).click();
  await expect(page.getByRole('status')).toContainText('ثبت نهایی و اعمال سند حسابداری روی سرور انجام شد');
  expect(mock.state().incomingInvoices).toHaveLength(1);
  expect(mock.state().ownedInventory).toHaveLength(1);
  expect(mock.state().incomingInvoices[0].payments[0].type).toBe('credit');
  await page.reload();
  await expect(page.getByRole('cell', { name: 'فروشنده آزمایشی', exact: true })).toBeVisible();
});

test('unavailable supervisor never reports financial health', async ({ page }) => {
  await fixture(page);
  await page.route('**/api/supervisor/report', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'ارتباط آزمایشی قطع است' }) }));
  await page.goto('/?page=financialSupervisor');
  await expect(page.getByRole('status')).toContainText('ارتباط آزمایشی قطع است');
  await expect(page.getByText('در کنترل‌های اجراشده مغایرت قطعی پیدا نشد.')).toHaveCount(0);
});
