import {
  basePath,
  csrfToken,
  installFetchInterceptors,
  rewriteInput,
  sanitizeHTML,
} from './utils/csrf.js';
import { genUUID, parseCategoryList } from './utils/dom.js';
import { copyToClipboard } from './utils/clipboard.js';

installFetchInterceptors();

export { basePath, csrfToken, rewriteInput, sanitizeHTML, genUUID, parseCategoryList, copyToClipboard };

export async function apiFetch(input, init) {
  return fetch(input, init);
}

export async function apiJSON(input, init) {
  const resp = await apiFetch(input, init);
  if (!resp.ok) {
    const body = await resp.text().catch(() => '');
    throw new Error(body || `Request failed: ${resp.status}`);
  }
  if (resp.status === 204) return null;
  return resp.json();
}

export function apiGet(input) {
  return apiJSON(input);
}

export function apiPost(input, body) {
  return apiJSON(input, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export function apiPut(input, body) {
  return apiJSON(input, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export function apiDelete(input) {
  return apiJSON(input, { method: 'DELETE' });
}

export default {
  apiFetch,
  apiJSON,
  apiGet,
  apiPost,
  apiPut,
  apiDelete,
  basePath,
  csrfToken,
  rewriteInput,
  sanitizeHTML,
  genUUID,
  parseCategoryList,
  copyToClipboard,
};
