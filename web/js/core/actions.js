// Event delegation: views register handlers by name or selector instead of
// adding listeners to every element they render. Errors are shown as a toast.
//
//   <button data-act="sync">          onClick('sync', el => ...)
//   <select data-budget="3">          onChange('[data-budget]', el => ...)
//   <form id="leaseForm">             onSubmit('#leaseForm', form => ...)
import { toastError } from './dom.js';

const clicks = new Map();
const changes = [];
const submits = [];

/** Registers a handler for clicks on elements with data-act="name". */
export function onClick(name, fn) {
  if (clicks.has(name)) throw new Error(`action ${name} registered twice`);
  clicks.set(name, fn);
}
/** Registers a handler for change events on elements matching the selector. */
export const onChange = (selector, fn) => changes.push([selector, fn]);
/** Registers a handler for submitted forms matching the selector (default is prevented). */
export const onSubmit = (selector, fn) => submits.push([selector, fn]);

async function run(fn, ...args) {
  try {
    await fn(...args);
  } catch (err) {
    toastError(err);
  }
}

export function initActions() {
  document.addEventListener('click', e => {
    const el = e.target.closest('[data-act]');
    const fn = el && clicks.get(el.dataset.act);
    if (fn) run(fn, el, e);
  });
  document.addEventListener('change', e => {
    for (const [selector, fn] of changes) if (e.target.matches(selector)) run(fn, e.target, e);
  });
  document.addEventListener('submit', e => {
    const match = submits.find(([selector]) => e.target.matches(selector));
    if (!match) return;
    e.preventDefault();
    run(match[1], e.target, e);
  });
}
