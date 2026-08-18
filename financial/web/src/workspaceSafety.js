const clone = value => JSON.parse(JSON.stringify(value ?? {}));

const rowIdentity = row => String(row?.id || row?.trackingNo || '').trim();

const stable = value => JSON.stringify(value ?? null);

export function normalizeChangedTransfers(previousState, proposedState) {
  const next = clone(proposedState);
  const previous = Array.isArray(previousState?.movements) ? previousState.movements : [];
  const previousById = new Map(previous.map(row => [rowIdentity(row), row]).filter(([id]) => id));
  const movements = Array.isArray(next.movements) ? next.movements : [];

  next.movements = movements.map(row => {
    if (row?.transactionType !== 'transfer') return row;
    const id = rowIdentity(row);
    const old = id ? previousById.get(id) : undefined;
    const changed = !old || stable(old) !== stable(row);
    if (!changed || row.direction !== 'in') return row;
    return { ...row, direction: 'out' };
  });

  return next;
}

export function installWorkspaceSafetyFetch() {
  if (typeof window === 'undefined' || typeof window.fetch !== 'function') return;
  if (window.__VIORA_WORKSPACE_SAFETY_INSTALLED__) return;
  window.__VIORA_WORKSPACE_SAFETY_INSTALLED__ = true;

  const nativeFetch = window.fetch.bind(window);
  let lastWorkspaceState = null;

  const isWorkspaceRoot = input => {
    const raw = typeof input === 'string' ? input : input?.url;
    if (!raw) return false;
    try {
      const url = new URL(raw, window.location.origin);
      return /\/workspace\/?$/.test(url.pathname);
    } catch {
      return /\/workspace\/?(?:\?|$)/.test(String(raw));
    }
  };

  window.fetch = async (input, init = {}) => {
    const method = String(init?.method || (typeof input !== 'string' ? input?.method : '') || 'GET').toUpperCase();
    let requestInit = init;

    if (method === 'PUT' && isWorkspaceRoot(input) && typeof init?.body === 'string') {
      try {
        const payload = JSON.parse(init.body);
        if (payload?.state && typeof payload.state === 'object') {
          payload.state = normalizeChangedTransfers(lastWorkspaceState || {}, payload.state);
          requestInit = { ...init, body: JSON.stringify(payload) };
        }
      } catch {
        // Let the API return its normal validation error for malformed payloads.
      }
    }

    const response = await nativeFetch(input, requestInit);

    if (response.ok && isWorkspaceRoot(input) && (method === 'GET' || method === 'PUT')) {
      try {
        const document = await response.clone().json();
        if (document?.state && typeof document.state === 'object') {
          lastWorkspaceState = clone(document.state);
        }
      } catch {
        // Snapshot tracking is best-effort; never interfere with a successful response.
      }
    }

    return response;
  };
}

installWorkspaceSafetyFetch();
