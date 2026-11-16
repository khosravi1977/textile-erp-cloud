import { isMonetaryColumn } from './reportTotals.js';

export function rialColumnLabel(label) {
  const text = String(label || '');
  if (/تاریخ|شماره|درصد|وضعیت|وزن|متراژ|تعداد|مقدار/.test(text)) return text;
  if (!isMonetaryColumn(text) && !/قیمت|نرخ واحد|^مانده(?:\s|$)/.test(text)) return text;
  return text.includes('ریال') ? text : `${text} (ریال)`;
}
