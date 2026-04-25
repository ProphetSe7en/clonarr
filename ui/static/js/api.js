// Global fetch wrapper. Handles two cross-cutting concerns so call sites
// don't have to:
//
// (1) CSRF — attach X-CSRF-Token header to every mutating fetch. Server
//     sets a clonarr_csrf cookie on GETs; the wrapper reads the cookie
//     and echoes it as a header on POST/PUT/DELETE/PATCH. AJAX only —
//     <form method="POST"> uses a hidden csrf_token field (logout form).
//
// (2) 401 → /login redirect — if any auth-gated endpoint returns 401
//     (session expired, logged out elsewhere, cookie cleared), bounce
//     the user to the login page. Centralized so every fetch call site
//     doesn't need `if (resp.status === 401) window.location...` boiler-
//     plate. A never-resolving promise is returned on redirect so
//     callers don't try to .json() a body that won't arrive before the
//     navigation completes.
//
//     Skip the redirect when:
//       - Already on /login or /setup (avoid loop — these pages probe
//         /api/auth/status as a public endpoint).
//       - Caller opts out via X-Skip-Login-Redirect header. The only
//         legitimate use is the disable-auth modal, where 401 means
//         "confirm_password incorrect" not "session expired".
(function() {
  const origFetch = window.fetch.bind(window);
  window.fetch = async function(input, init) {
    const request = new Request(input, init);
    const method = request.method.toUpperCase();
    const headers = new Headers(request.headers);
    if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
      const m = document.cookie.match(/(?:^|; )clonarr_csrf=([^;]+)/);
      if (m) headers.set('X-CSRF-Token', m[1]);
    }
    const skipLoginRedirect = headers.get('X-Skip-Login-Redirect') === '1';
    // Client-side hint only — strip before sending to server.
    headers.delete('X-Skip-Login-Redirect');
    const resp = await origFetch(new Request(request, { headers }));
    if (resp.status === 401 && !skipLoginRedirect) {
      const path = window.location.pathname;
      if (path !== '/login' && path !== '/setup') {
        window.location.href = '/login';
        return new Promise(() => {});
      }
    }
    return resp;
  };
})();

// Sanitize HTML — only allow safe tags and attributes (for TRaSH descriptions)
function sanitizeHTML(html) {
  if (!html) return '';
  const div = document.createElement('div');
  div.innerHTML = html;
  const allowed = new Set(['A', 'B', 'BR', 'EM', 'I', 'P', 'SPAN', 'STRONG', 'U', 'TABLE', 'THEAD', 'TBODY', 'TR', 'TH', 'TD']);
  const allowedAttrs = { 'A': ['href', 'target', 'rel'], 'TABLE': ['class'], 'TH': ['class'], 'TD': ['class'] };
  function clean(node) {
    for (const child of [...node.childNodes]) {
      if (child.nodeType === 1) { // element
        if (!allowed.has(child.tagName)) {
          child.replaceWith(document.createTextNode(child.textContent));
          continue;
        }
        const okAttrs = allowedAttrs[child.tagName] || [];
        for (const attr of [...child.attributes]) {
          if (!okAttrs.includes(attr.name)) child.removeAttribute(attr.name);
        }
        if (child.tagName === 'A') {
          const href = child.getAttribute('href') || '';
          if (!href.startsWith('http://') && !href.startsWith('https://')) {
            child.removeAttribute('href');
          }
          if (child.hasAttribute('target')) {
            child.setAttribute('rel', 'noopener noreferrer');
          }
        }
        clean(child);
      }
    }
  }
  clean(div);
  return div.innerHTML;
}

// Generate UUID that works over plain HTTP (crypto.randomUUID needs secure context)
function genUUID(noDashes) {
  if (crypto.randomUUID) {
    const id = crypto.randomUUID();
    return noDashes ? id.replace(/-/g, '') : id;
  }
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  if (noDashes) return hex;
  return hex.slice(0,8)+'-'+hex.slice(8,12)+'-'+hex.slice(12,16)+'-'+hex.slice(16,20)+'-'+hex.slice(20);
}

// Parse a comma-separated list of Newznab category IDs from the Settings input
// into a deduped sorted int array. Drops blanks, non-numerics, and non-positive values.
function parseCategoryList(str) {
  if (!str) return [];
  const seen = new Set();
  for (const part of String(str).split(/[,\s]+/)) {
    const n = parseInt(part.trim(), 10);
    if (!isNaN(n) && n > 0) seen.add(n);
  }
  return [...seen].sort((a, b) => a - b);
}

