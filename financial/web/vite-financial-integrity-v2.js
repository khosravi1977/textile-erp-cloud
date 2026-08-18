const importAnchor = "import { isMonetaryColumn, monetaryColumnTotals, parseLocalizedNumber } from './reportTotals.js';";
const integrityImport = "import { buildFinancialHealthAccurate, normalizeAccessListStrict } from './audit/financialIntegrity.js';";

function replaceSection(source, startMarker, endMarker, replacement, label) {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  if (start < 0 || end < 0 || end <= start) throw new Error(`financial integrity transform anchor missing: ${label}`);
  return source.slice(0, start) + replacement + source.slice(end);
}

function replaceRequired(source, oldValue, newValue, label) {
  if (source.includes(newValue)) return source;
  if (!source.includes(oldValue)) throw new Error(`financial integrity transform anchor missing: ${label}`);
  return source.replace(oldValue, newValue);
}

export function transformFinancialAppSource(input) {
  let source = String(input);
  if (!source.includes(integrityImport)) {
    if (!source.includes(importAnchor)) throw new Error('financial integrity transform anchor missing: import');
    source = source.replace(importAnchor, `${importAnchor}\n${integrityImport}`);
  }

  if (source.includes('function normalizeAccessList(list) {')) {
    source = replaceSection(source, 'function normalizeAccessList(list) {', '\n\nfunction buildSessionProfile', 'const normalizeAccessList = normalizeAccessListStrict;', 'permission normalization');
  }
  source = source.replace("portalRole: sessionData.portalRole || claims.portal_role || claims.role || 'owner',", "portalRole: sessionData.portalRole || claims.portal_role || claims.role || 'viewer',");
  source = source.replace('permissions: normalizeAccessList(sessionData.permissions || claims.permissions),', "permissions: normalizeAccessList(sessionData.permissions || claims.permissions, sessionData.portalRole || claims.portal_role || claims.role || 'viewer'),");

  if (source.includes('function buildFinancialHealth(finance) {')) {
    source = replaceSection(source, 'function buildFinancialHealth(finance) {', '\n\nfunction kpiTone', 'const buildFinancialHealth = buildFinancialHealthAccurate;', 'financial health calculation');
  }

  source = replaceRequired(source,
    "const purchases = finance.incomingInvoices.filter(x => inRange(x.date));",
    "const purchases = finance.incomingInvoices.filter(x => !x.nonFinancial && inRange(x.date));",
    'tax report excludes non-financial incoming rows');
  source = replaceRequired(source,
    "const futureChecks = finance.receivableDocs.filter(x => x.customer === customer && x.status !== 'cleared' && x.status !== 'assigned').reduce((s, x) => s + Number(x.amount || 0), 0);",
    "const futureChecks = finance.receivableDocs.filter(x => x.customer === customer && x.status === 'open').reduce((s, x) => s + Number(x.amount || 0), 0);",
    'credit scoring valid open checks');
  source = replaceRequired(source,
    "const netExposure = financial.debt - futureChecks - ownedYarn - ownedFabric;",
    "const netExposure = Math.max(0, financial.debt + futureChecks - ownedYarn - ownedFabric);",
    'credit exposure calculation');

  // The available workspace does not persist depreciation/amortization nor an
  // approved budget. Do not present net/operating profit as EBITDA and do not
  // imply that an empty variance table is a calculated budget comparison.
  source = source.replace('label="EBITDA"', 'label="سود عملیاتی ثبت‌شده"');
  source = source.replace('>تحليل انحرافات بودجه</h3>', '>انحراف بودجه (نیازمند بودجه مصوب)</h3>');

  const forbidden = [
    "const allowed = Array.isArray(list) && list.length ? list.filter(item => fullPageAccess.includes(item)) : fullPageAccess;",
    "portalRole: sessionData.portalRole || claims.portal_role || claims.role || 'owner',",
    'function buildFinancialHealth(finance) {',
    "const purchases = finance.incomingInvoices.filter(x => inRange(x.date));",
    "const netExposure = financial.debt - futureChecks - ownedYarn - ownedFabric;",
    'label="EBITDA"',
  ];
  for (const pattern of forbidden) if (source.includes(pattern)) throw new Error(`financial integrity transform left legacy pattern: ${pattern}`);
  if (!source.includes('const normalizeAccessList = normalizeAccessListStrict;') || !source.includes('const buildFinancialHealth = buildFinancialHealthAccurate;')) throw new Error('financial integrity transform did not activate audited helpers');
  return source;
}

export function financialIntegrityPlugin() {
  return {
    name: 'viora-financial-integrity',
    enforce: 'pre',
    transform(code, id) {
      if (!String(id).replace(/\\/g, '/').endsWith('/src/App.jsx')) return null;
      return { code: transformFinancialAppSource(code), map: null };
    },
  };
}
