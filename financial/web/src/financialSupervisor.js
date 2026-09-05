import { toLatinDigits } from './persianDateInput.js';
const number = value => Number.isFinite(Number(value)) ? Number(value) : 0;
const normalizedName = value => toLatinDigits(value || '').normalize('NFKC').replace(/ي/g, 'ی').replace(/ك/g, 'ک').replace(/[\u200c\s]+/g, ' ').trim().toLocaleLowerCase('fa');
function priceDate(value) {
  const raw = toLatinDigits(value || '').trim();
  const jalali = raw.match(/^(1[34]\d{2})[/-](\d{1,2})[/-](\d{1,2})$/);
  if (jalali) return Number(jalali[2]) >= 1 && Number(jalali[2]) <= 12 && Number(jalali[3]) >= 1 && Number(jalali[3]) <= 31 ? `${jalali[1]}/${jalali[2].padStart(2, '0')}/${jalali[3].padStart(2, '0')}` : '';
  if (!/^20\d{2}-\d{2}-\d{2}$/.test(raw)) return '';
  const date = new Date(raw + 'T12:00:00Z');
  if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== raw) return '';
  const parts = new Intl.DateTimeFormat('en-US-u-ca-persian', { year: 'numeric', month: '2-digit', day: '2-digit', timeZone: 'UTC' }).formatToParts(date);
  const part = key => parts.find(x => x.type === key)?.value;
  return `${part('year')}/${part('month')}/${part('day')}`;
}
const text = value => String(value ?? '').trim();
function rows(value) { return Array.isArray(value) ? value : []; }

export function suggestIncomingPrice(finance, draft, limit = 5) {
  const currentId = text(draft?.id);
  const item = normalizedName(draft?.itemName);
  const customer = normalizedName(draft?.customer);
  const asOf = priceDate(draft?.date || new Date().toISOString().slice(0, 10));
  if (!item) return { price: 0, samples: [], confidence: 'none', basis: 'سابقه‌ای پیدا نشد' };
  const samples = rows(finance?.incomingInvoices)
    .filter(row => text(row.id) !== currentId && !row.nonFinancial && normalizedName(row.itemName) === item
      && (row.inventoryType || 'yarn') === (draft?.inventoryType || 'yarn')
      && (row.unit || '') === (draft?.unit || '')
      && (row.currency || 'IRT') === (draft?.currency || 'IRT')
      && (row.source_type === 'operational_chelle_in') === (draft?.source_type === 'operational_chelle_in')
      && asOf && priceDate(row.date) && priceDate(row.date) <= asOf)
    .map(row => ({
      id: text(row.id),
      date: text(row.date),
      dateKey: priceDate(row.date),
      customer: text(row.customer),
      price: number(row.unitPrice) || (number(row.quantity) > 0 ? number(row.subtotal || (number(row.amount) - number(row.taxAmount))) / number(row.quantity) : 0),
    }))
    .filter(row => row.price > 0)
    .sort((left, right) => right.dateKey.localeCompare(left.dateKey) || left.id.localeCompare(right.id));
  const exact = samples.filter(row => normalizedName(row.customer) === customer);
  const selected = (exact.length ? exact : samples).slice(0, limit);
  if (!selected.length) return { price: 0, samples: [], confidence: 'none', basis: 'سابقه‌ای برای این کالا پیدا نشد' };
  const ordered = selected.map(row => row.price).sort((a, b) => a - b);
  const middle = Math.floor(ordered.length / 2);
  const median = ordered.length % 2 ? ordered[middle] : (ordered[middle - 1] + ordered[middle]) / 2;
  return {
    price: Math.round(median),
    samples: selected,
    confidence: exact.length >= 2 ? 'high' : exact.length === 1 ? 'medium' : 'low',
    basis: exact.length ? `${selected.length.toLocaleString('fa-IR')} فاکتور قبلی همین فروشنده و کالا` : `${selected.length.toLocaleString('fa-IR')} فاکتور قبلی همین کالا از فروشندگان دیگر`,
    min: Math.min(...ordered),
    max: Math.max(...ordered),
  };
}
