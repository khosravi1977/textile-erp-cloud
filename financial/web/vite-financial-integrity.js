const importAnchor = "import { isMonetaryColumn, monetaryColumnTotals, parseLocalizedNumber } from './reportTotals.js';";
const integrityImport = "import { buildFinancialHealthAccurate, normalizeAccessListStrict } from './audit/financialIntegrity.js';";

function replaceSection(source, startMarker, endMarker, replacement, label) {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  if (start < 0 || end < 0 || end <= start) {
    throw new Error(`financial integrity transform anchor missing: ${label}`);
  }
  return source.slice(0, start) + replacement + source.slice(end);
}

export function transformFinancialAppSource(input) {
  let source = String(input);

  if (!source.includes(integrityImport)) {
    if (!source.includes(importAnchor)) throw new Error('financial integrity transform anchor missing: import');
    source = source.replace(importAnchor, `${importAnchor}\n${integrityImport}`);
  }

  if (source.includes('function normalizeAccessList(list) {')) {
    source = replaceSection(
      source,
      'function normalizeAccessList(list) {',
      '\n\nfunction buildSessionProfile',
      'const normalizeAccessList = normalizeAccessListStrict;',
      'permission normalization',
    );
  }

  source = source.replace(
    "portalRole: sessionData.portalRole || claims.portal_role || claims.role || 'owner',",
    "portalRole: sessionData.portalRole || claims.portal_role || claims.role || 'viewer',",
  );
  source = source.replace(
    'permissions: normalizeAccessList(sessionData.permissions || claims.permissions),',
    "permissions: normalizeAccessList(sessionData.permissions || claims.permissions, sessionData.portalRole || claims.portal_role || claims.role || 'viewer'),",
  );

  if (source.includes('function buildFinancialHealth(finance) {')) {
    source = replaceSection(
      source,
      'function buildFinancialHealth(finance) {',
      '\n\nfunction kpiTone',
      'const buildFinancialHealth = buildFinancialHealthAccurate;',
      'financial health calculation',
    );
  }

  const forbidden = [
    "const allowed = Array.isArray(list) && list.length ? list.filter(item => fullPageAccess.includes(item)) : fullPageAccess;",
    "portalRole: sessionData.portalRole || claims.portal_role || claims.role || 'owner',",
    'function buildFinancialHealth(finance) {',
  ];
  for (const pattern of forbidden) {
    if (source.includes(pattern)) throw new Error(`financial integrity transform left insecure/legacy pattern: ${pattern}`);
  }
  if (!source.includes('const normalizeAccessList = normalizeAccessListStrict;') || !source.includes('const buildFinancialHealth = buildFinancialHealthAccurate;')) {
    throw new Error('financial integrity transform did not activate audited helpers');
  }
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
