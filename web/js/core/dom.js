// DOM helpers, icons and the toast message.
import { TIMING } from './constants.js';
import { esc } from './format.js';

export const $ = selector => document.querySelector(selector);
export const $$ = selector => [...document.querySelectorAll(selector)];

/** The page content area. */
export const main = $('#main');

/** Renders HTML into the page content area. */
export const render = html => {
  main.innerHTML = html;
};

/** Stroked 24×24 icon with the given SVG paths. */
export const svg = paths =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" ` +
  `stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

/** <option> elements from [value, label] pairs. */
export const options = (pairs, selected) =>
  pairs
    .map(([v, l]) => `<option value="${esc(v)}"${String(v) === String(selected) ? ' selected' : ''}>${esc(l)}</option>`)
    .join('');

/**
 * Returns a if cond is truthy, else b (default ''). Pass a function to build the
 * text only when it is needed, e.g. when(msg, () => msg[1]).
 */
export const when = (cond, a, b = '') => {
  const v = cond ? a : b;
  return typeof v === 'function' ? v() : v;
};

let toastTimer;
/** Shows a message at the bottom; ms = 0 keeps it until the next one. */
export function toast(html, ms = TIMING.toast) {
  const t = $('#toast');
  t.innerHTML = html;
  t.hidden = false;
  clearTimeout(toastTimer);
  if (ms) toastTimer = setTimeout(() => (t.hidden = true), ms);
}
export const toastError = e => toast(`<span>${esc(e.message || e)}</span>`, TIMING.toastError);

/**
 * Two-click confirmation for destructive buttons: the first click changes the
 * label and returns false, the second returns true.
 */
export function confirmed(button, question) {
  if (button.dataset.confirm === '1') return true;
  button.dataset.confirm = '1';
  button.textContent = question;
  return false;
}

/** Scrolls an element to the top of the view if it exists. */
export const scrollToTop = el => el?.scrollIntoView({ block: 'start' });
