// The app shell: routing between pages, the two areas with their navigation,
// the sidebar with account balances and the sync status.
import { BOOK_HOUSEHOLD, BOOK_PROPERTY, MOBILE_MAX_WIDTH, TIMING } from './constants.js';
import { $, $$, render, svg, toast, toastError } from './dom.js';
import { E0, ago, dm, esc, hm, monthLabel, plural, shiftMonth } from './format.js';
import { api, get, LOGIN_REQUIRED } from './api.js';
import { S, HV, currentMonth, accountName } from './state.js';
import { onClick, onSubmit } from './actions.js';

/** Pages per area: [hash, title]. */
const AREAS = {
  [BOOK_HOUSEHOLD]: {
    title: 'Haushaltsbuch',
    tabs: [
      ['monat', 'Monatsbudget'],
      ['umsaetze', 'Umsätze'],
      ['abos', 'Abos & Fixkosten'],
      ['paar', 'Paar-Aufteilung'],
      ['konten', 'Konten'],
    ],
  },
  [BOOK_PROPERTY]: {
    title: 'Hausverwaltung',
    tabs: [
      ['hv', 'Übersicht'],
      ['hv-mieter', 'Mieter & Verträge'],
      ['hv-objekte', 'Objekte'],
      ['hv-nk', 'Nebenkosten'],
      ['hv-umsaetze', 'Mietkonto'],
      ['hv-fristen', 'Fristen'],
      ['hv-konten', 'Konten'],
    ],
  },
};
const DEFAULT_TAB = 'monat';

/** Pages that show a budget month and the month switcher. */
const MONTH_TABS = ['monat', 'umsaetze', 'paar'];

const ICONS = {
  monat: svg('<circle cx="12" cy="12" r="9"/><path d="M12 3v9l6 4"/>'),
  umsaetze: svg('<path d="M4 6h16M4 12h16M4 18h10"/>'),
  abos: svg(
    '<path d="M17 2l4 4-4 4"/><path d="M3 11V9a3 3 0 0 1 3-3h15"/><path d="M7 22l-4-4 4-4"/><path d="M21 13v2a3 3 0 0 1-3 3H3"/>',
  ),
  paar: svg(
    '<circle cx="9" cy="8" r="3.5"/><circle cx="17" cy="9" r="2.5"/><path d="M3 20c0-3.3 2.7-5.5 6-5.5s6 2.2 6 5.5"/><path d="M15.5 14.6c2.9.2 5.5 2 5.5 5.4"/>',
  ),
  konten: svg('<path d="M3 10l9-6 9 6"/><path d="M5 10v8M9.5 10v8M14.5 10v8M19 10v8"/><path d="M3 21h18"/>'),
  hv: svg(
    '<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>',
  ),
  'hv-mieter': svg(
    '<circle cx="9" cy="8" r="3.5"/><path d="M3 20c0-3.3 2.7-5.5 6-5.5s6 2.2 6 5.5"/><path d="M16 11h5M18.5 8.5v5"/>',
  ),
  'hv-objekte': svg('<path d="M4 21V8l8-5 8 5v13"/><path d="M9 21v-5h6v5"/>'),
  'hv-nk': svg('<path d="M6 3h9l4 4v14H6z"/><path d="M9 12h7M9 16h7M9 8h3"/>'),
  'hv-umsaetze': svg('<path d="M4 6h16M4 12h16M4 18h10"/>'),
  'hv-fristen': svg('<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>'),
  'hv-konten': svg('<path d="M3 10l9-6 9 6"/><path d="M5 10v8M9.5 10v8M14.5 10v8M19 10v8"/><path d="M3 21h18"/>'),
};

/** Short labels for the mobile bottom bar. */
const NAV_SHORT = {
  monat: 'Budget',
  abos: 'Abos',
  paar: 'Paar',
  hv: 'Übersicht',
  'hv-mieter': 'Mieter',
  'hv-umsaetze': 'Mietkonto',
};

const views = {};
/** Registers the function that renders a page. */
export const registerView = (tab, fn) => (views[tab] = fn);

const isKnownTab = tab => Object.values(AREAS).some(a => a.tabs.some(([h]) => h === tab));
const areaOf = tab => (tab.startsWith('hv') ? BOOK_PROPERTY : BOOK_HOUSEHOLD);

/** Renders the page of the current URL hash. */
export async function route() {
  const hash = (location.hash || '#' + DEFAULT_TAB).slice(1);
  if (!S.me) {
    try {
      S.me = await get('/api/me');
      S.loggedIn = S.me.logged_in;
    } catch (e) {
      return render(`<div class="card empty">Server nicht erreichbar: ${esc(e.message)}</div>`);
    }
  }
  if (S.me.auth_required && !S.loggedIn) return viewLogin();

  S.tab = isKnownTab(hash) ? hash : DEFAULT_TAB;
  if (!S.monthInit) {
    S.month = currentMonth();
    S.monthInit = true;
  }
  if (!S.month && S.tab !== 'umsaetze') S.month = currentMonth();
  $('#top').hidden = false;
  $('#side').hidden = false;
  renderArea(areaOf(S.tab));
  renderHeading();
  if (!S.cats.length) {
    try {
      S.cats = await get('/api/categories');
    } catch (e) {
      return toastError(e);
    }
  }
  try {
    await views[S.tab]?.();
  } catch (e) {
    if (e.message !== LOGIN_REQUIRED) render(`<div class="card empty">${esc(e.message)}</div>`);
  }
  updateSyncState();
  renderSide();
}

/** Page title, area and month switcher in the header. */
function renderHeading() {
  const area = AREAS[S.area];
  const isMonthTab = MONTH_TABS.includes(S.tab);
  const tabLabel = area.tabs.find(([h]) => h === S.tab)?.[1] || '';
  $('#monthnav').hidden = !(isMonthTab || S.tab === 'hv');
  $('#mLabel').textContent =
    S.tab === 'hv'
      ? `Mieteingänge ${monthLabel(HV.month)}`
      : isMonthTab
        ? S.month
          ? monthLabel(S.month)
          : 'Alle Monate'
        : tabLabel;
  $('#areaTitle').textContent = isMonthTab && S.tab !== DEFAULT_TAB ? `${area.title} · ${tabLabel}` : area.title;
  $('#pRange').textContent = '';
}

/** Shows the date range of the budget period in the header. */
export function showPeriod(p) {
  if (!p) return;
  const salary = p.mode === 'salary';
  $('#pRange').textContent =
    `${dm(p.start)} – ${dm(p.end)}${p.end.slice(0, 4)}${salary ? ' · von Gehalt zu Gehalt' : ''}`;
  $('#pRange').title = salary
    ? `Budgetmonat von Gehalt zu Gehalt${p.salary_series ? ` (${p.salary_series})` : ''}`
    : 'Kalendermonat';
}

function renderArea(area) {
  S.area = area;
  S.lastTab[area] = S.tab;
  document.body.dataset.area = area;
  document.title = `${AREAS[area].title} · Homestead`;
  for (const a of $$('.areas a')) {
    const on = a.dataset.area === area;
    a.toggleAttribute('aria-current', on);
    if (on) a.setAttribute('aria-current', 'page');
    a.href = '#' + S.lastTab[a.dataset.area];
  }
  const mobile = window.innerWidth <= MOBILE_MAX_WIDTH;
  $('#tabs').innerHTML = AREAS[area].tabs
    .map(([h, label]) => {
      const current = h === S.tab ? ' aria-current="page"' : '';
      return `<a href="#${h}"${current}>${ICONS[h] || ''}<span>${esc(mobile && NAV_SHORT[h] ? NAV_SHORT[h] : label)}</span></a>`;
    })
    .join('');
}

/** Sidebar: balances of the accounts in the current area. */
async function renderSide() {
  try {
    const accs = (await get('/api/accounts')) || [];
    const mine = accs.filter(a => a.active && (a.book || BOOK_HOUSEHOLD) === S.area);
    $('#sideAccTitle').textContent = S.area === BOOK_PROPERTY ? 'Mietkonten' : 'Konten';
    $('#sideAccounts').innerHTML = mine.length
      ? mine
          .map(
            a =>
              `<div><span title="${esc(a.bank)}">${esc(accountName(a))}</span>` +
              `<b class="${a.balance < 0 ? 'neg' : ''}">${a.balance != null ? E0(a.balance) : '–'}</b></div>`,
          )
          .join('')
      : '<div><span>Noch kein Konto</span></div>';
    S.nextSync =
      mine
        .map(a => a.schedule?.next)
        .filter(Boolean)
        .sort()[0] || null;
    updateSyncState();
  } catch {
    /* sidebar is optional */
  }
}

/** Sync status below the accounts: last and next sync, problems in this area. */
async function updateSyncState() {
  try {
    const s = await get('/api/sync');
    const problems = (s.problems || []).filter(p => !p.book || p.book === S.area).length;
    const next = S.nextSync && new Date(S.nextSync) > new Date() ? ` · nächster ${hm(S.nextSync)}` : '';
    const finished = s.last_finish && !s.last_finish.startsWith('0001'); // zero time = never
    const el = $('#syncState');
    el.textContent =
      (s.running ? 'Abruf läuft …' : finished ? `Abgerufen ${ago(s.last_finish)}${next}` : '') +
      (problems ? ` · ${plural(problems, 'Problem', 'Probleme')}` : '');
    el.classList.toggle('running', !!s.running);
    el.classList.toggle('problem', problems > 0);
    return s;
  } catch {
    return null;
  }
}

let pollTimer;
/** Polls the sync status while a sync runs, then reloads the page. */
export async function pollSync() {
  clearTimeout(pollTimer);
  const s = await updateSyncState();
  if (s?.running) {
    pollTimer = setTimeout(pollSync, TIMING.syncPoll);
  } else if (s) {
    if (s.new_transactions) toast(`${s.new_transactions} neue Umsätze abgerufen`, TIMING.toastMedium);
    S.cats = [];
    route();
  }
}

function viewLogin() {
  $('#top').hidden = true;
  $('#side').hidden = true;
  render(`<form class="card login" id="loginForm">
    <h2>Homestead</h2>
    <div class="field"><label for="pw">Passwort</label>
      <input type="password" id="pw" autocomplete="current-password" required autofocus></div>
    <button class="btn primary" type="submit">Anmelden</button>
    <p class="note" id="loginMsg"></p></form>`);
}

export function initShell() {
  window.addEventListener('hashchange', route);
  const moveMonth = d => {
    if (S.tab === 'hv') HV.month = shiftMonth(HV.month, d);
    else S.month = shiftMonth(S.month || currentMonth(), d);
    route();
  };
  $('#prevM').addEventListener('click', () => moveMonth(-1));
  $('#nextM').addEventListener('click', () => moveMonth(1));

  onSubmit('#loginForm', async () => {
    try {
      await api('POST', '/api/login', { password: $('#pw').value });
      S.loggedIn = true;
      S.me = null;
      location.hash = '#' + DEFAULT_TAB;
      route();
    } catch (err) {
      $('#loginMsg').textContent = err.message;
    }
  });
  onClick('logout', async () => {
    await api('POST', '/api/logout');
    S.loggedIn = false;
    location.hash = '#login';
    route();
  });
  onClick('sync', async () => {
    const r = await api('POST', '/api/sync');
    toast(r.started ? 'Abruf gestartet …' : 'Abruf läuft bereits', TIMING.toastMedium);
    pollSync();
  });
}
