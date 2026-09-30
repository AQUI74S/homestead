// Calls to the JSON API. Errors are thrown with the server's message.
import { S } from './state.js';

export const LOGIN_REQUIRED = 'Bitte anmelden';

async function parse(res) {
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `Fehler ${res.status}`);
  return data;
}

export async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  if (res.status === 401 && path !== '/api/login') {
    S.loggedIn = false;
    location.hash = '#login';
    throw new Error(LOGIN_REQUIRED);
  }
  return parse(res);
}

export const get = path => api('GET', path);

/** Uploads a file as multipart form data. */
export async function upload(path, file) {
  const fd = new FormData();
  fd.append('file', file);
  return parse(await fetch(path, { method: 'POST', body: fd, credentials: 'same-origin' }));
}

/** Query string from an object, leaving out empty values. */
export function query(params) {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== '' && v != null && v !== false) p.set(k, v);
  return p.toString();
}
