// Theme switch: Auto (follows the device) | Hell | Dunkel. index.html applies the
// saved choice before the first paint; this module keeps it up to date.
import { THEME_KEY } from './constants.js';
import { $$, svg } from './dom.js';
import { onClick } from './actions.js';

const THEMES = [
  ['auto', 'Auto', svg('<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>')],
  [
    'light',
    'Hell',
    svg(
      '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2' +
        'M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
    ),
  ],
  ['dark', 'Dunkel', svg('<path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z"/>')],
];

const darkMQ = matchMedia('(prefers-color-scheme: dark)');

function preference() {
  try {
    return localStorage.getItem(THEME_KEY) || 'auto';
  } catch {
    return 'auto';
  }
}

/** The switch; placed into every element with class "theme-slot". */
export function themeSwitchHTML() {
  const p = preference();
  const buttons = THEMES.map(
    ([key, label, icon]) =>
      `<button type="button" data-act="theme" data-theme="${key}" aria-pressed="${p === key}" title="${label}">` +
      `${icon}<span>${label}</span></button>`,
  );
  return `<div class="theme-sw" role="group" aria-label="Erscheinungsbild">${buttons.join('')}</div>`;
}

function applyTheme() {
  const p = preference();
  document.documentElement.dataset.theme = p === 'auto' ? (darkMQ.matches ? 'dark' : 'light') : p;
  for (const el of $$('.theme-slot')) el.innerHTML = themeSwitchHTML();
}

export function initTheme() {
  darkMQ.addEventListener?.('change', () => preference() === 'auto' && applyTheme());
  onClick('theme', el => {
    try {
      localStorage.setItem(THEME_KEY, el.dataset.theme);
    } catch {
      /* storage blocked (private mode): stay on Auto */
    }
    applyTheme();
  });
  applyTheme();
}
