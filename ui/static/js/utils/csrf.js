// Shared browser security helpers for the Clonarr UI.

const BASE = (document.documentElement.dataset.base || '').replace(/\/$/, '');

export function basePath() {
  return BASE;
}

export function rewriteInput(input) {
  if (BASE === '') return input;
  if (typeof input === 'string' && input.startsWith('/') && !input.startsWith(BASE + '/')) {
    return BASE + input;
  }
  return input;
}

export function csrfToken() {
  const m = document.cookie.match(/(?:^|; )clonarr_csrf=([^;]+)/);
  return m ? m[1] : '';
}

export function installFetchInterceptors() {
  if (window.__clonarrFetchInterceptorsInstalled) return;
  window.__clonarrFetchInterceptorsInstalled = true;

  const origFetch = window.fetch.bind(window);
  window.fetch = async function(input, init) {
    const request = new Request(rewriteInput(input), init);
    const method = request.method.toUpperCase();
    const headers = new Headers(request.headers);
    if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
      const token = csrfToken();
      if (token) headers.set('X-CSRF-Token', token);
    }
    const skipLoginRedirect = headers.get('X-Skip-Login-Redirect') === '1';
    // Client-side hint only — strip before sending to server.
    headers.delete('X-Skip-Login-Redirect');
    const resp = await origFetch(new Request(request, { headers }));
    if (resp.status === 401 && !skipLoginRedirect) {
      const path = window.location.pathname;
      if (path !== BASE + '/login' && path !== BASE + '/setup') {
        window.location.href = BASE + '/login';
        return new Promise(() => {});
      }
    }
    return resp;
  };
}

// Sanitize HTML — only allow safe tags and attributes (for TRaSH descriptions).
export function sanitizeHTML(html) {
  if (!html) return '';
  const div = document.createElement('div');
  div.innerHTML = html;
  const allowed = new Set(['A', 'B', 'BR', 'EM', 'I', 'P', 'SPAN', 'STRONG', 'U', 'TABLE', 'THEAD', 'TBODY', 'TR', 'TH', 'TD']);
  const allowedAttrs = { A: ['href', 'target', 'rel'], TABLE: ['class'], TH: ['class'], TD: ['class'] };
  function clean(node) {
    for (const child of [...node.childNodes]) {
      if (child.nodeType === 1) {
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
