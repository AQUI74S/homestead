// Accounts: bank connections, account settings, budget month, rules, appearance.
import {
  BANK_RESULTS,
  BOOK_HOUSEHOLD,
  BOOK_PROPERTY,
  CONNECTION_STATUS,
  DAY_MS,
  LIMITS,
  OWNER_A,
  OWNER_B,
  OWNER_JOINT,
  PERIOD_CALENDAR,
  PERIOD_SALARY,
  RULE_FIELDS,
  TIMING,
} from '../core/constants.js';
import { E, ago, dmy, esc, hm, ibanBlocks } from '../core/format.js';
import { $, $$, confirmed, options, render, toast, when } from '../core/dom.js';
import { api, get, upload } from '../core/api.js';
import { S, accountName, nameOf } from '../core/state.js';
import { onChange, onClick } from '../core/actions.js';
import { pollSync, registerView } from '../core/shell.js';
import { themeSwitchHTML } from '../core/theme.js';

const bookTitle = book => (book === BOOK_HOUSEHOLD ? 'Haushaltsbuch' : 'Hausverwaltung');
const bankMatches = b => !S.bankFilter || b.name.toLowerCase().includes(S.bankFilter.toLowerCase());

async function viewAccounts(book = BOOK_HOUSEHOLD) {
  const household = book === BOOK_HOUSEHOLD;
  const [allAccounts, allConns, rules, recs] = await Promise.all([
    get('/api/accounts'),
    get('/api/connections'),
    get('/api/rules'),
    get('/api/recurring'),
  ]).then(r => r.map(x => x || []));
  const accs = allAccounts.filter(a => (a.book || BOOK_HOUSEHOLD) === book);
  const conns = allConns.filter(
    c => (c.book || BOOK_HOUSEHOLD) === book || allAccounts.some(a => a.connection_id === c.id && a.book === book),
  );
  const others = allAccounts.length - accs.length;
  S.me = await get('/api/me');

  // Result of a bank authorization (?bank=ok|abgebrochen|fehler), shown once
  const bankResult = new URLSearchParams(location.search).get('bank');
  if (bankResult) history.replaceState(null, '', location.pathname + location.hash);
  const msg = BANK_RESULTS[bankResult];

  render(`${when(msg, () => `<div class="banner ${msg[0]}"><p>${msg[1]}</p></div>`)}
    ${when(
      S.me.demo,
      '<div class="banner warn"><p><b>Demo-Modus:</b> Die Banken hier sind simuliert. Für echte Konten ' +
        '<code>HS_DEMO</code> ausschalten und Enable Banking einrichten (siehe README).</p></div>',
    )}
    ${connectionsHTML(conns, book)}
    ${when(household, () => periodSettingsHTML(recs.filter(r => r.direction === 'in')))}
    ${when(
      !accs.length,
      `<div class="card empty">Noch kein Konto im Bereich ${bookTitle(book)}. Verbinde eine Bank oder
      verschiebe ein Konto über „Gehört zu“ hierher.</div>`,
    )}
    <section class="acc-grid">${accs.map(a => accountCard(a, household)).join('')}</section>
    ${when(
      others,
      `<p class="note">${others === 1 ? 'Ein weiteres Konto gehört' : others + ' weitere Konten gehören'} zum
      Bereich ${household ? '<a href="#hv-konten">Hausverwaltung</a>' : '<a href="#konten">Haushaltsbuch</a>'}.</p>`,
    )}
    ${when(household, () => rulesHTML(rules))}
    <section class="ledger"><header><h2>Erscheinungsbild</h2><span class="sum">Auto folgt der Einstellung deines Geräts.
      Die Wahl gilt für diesen Browser.</span></header>
      <div class="theme-slot" style="max-width:340px;padding:4px 18px 18px">${themeSwitchHTML()}</div></section>
    ${when(S.me.auth_required, '<div class="row"><span class="spacer"></span><button class="btn ghost" data-act="logout">Abmelden</button></div>')}`);

  const search = $('#bankq');
  if (search) {
    search.addEventListener('input', () => {
      S.bankFilter = search.value;
      $('#bankList').innerHTML = bankButtons((S.banks || []).filter(bankMatches));
    });
    search.focus();
  }
  if (bankResult === 'ok') pollSync();
}

function connectionsHTML(conns, book) {
  const shown = conns.filter(c => c.status !== 'pending');
  const bankButton = (c, label) =>
    `<button class="btn small" data-act="connect" data-bank="${esc(c.bank)}" data-country="${esc(c.country)}">${label}</button>`;
  const rows = shown.map(
    c => `<tr><td><b>${esc(c.bank)}</b></td>
      <td><span class="dot ${c.status}"></span> ${CONNECTION_STATUS[c.status] || c.status}${when(
        c.last_error,
        `<br><span class="muted small">${esc(c.last_error)}</span>`,
      )}</td>
      <td class="tnum">${c.valid_until ? dmy(c.valid_until.slice(0, 10)) : '–'}</td>
      <td class="r">${
        c.status !== 'active'
          ? bankButton(c, 'Neu verbinden')
          : `${bankButton(c, 'Verlängern')} <button class="btn ghost small danger" data-act="disconnect" data-id="${c.id}">Trennen</button>`
      }</td></tr>`,
  );
  const picker = `<div class="field" style="margin-top:12px"><label for="bankq">Bank suchen</label>
      <input id="bankq" type="search" placeholder="z. B. Sparkasse, DKB, ING, Volksbank …" value="${esc(S.bankFilter)}"></div>
    <div class="bank-list" id="bankList">${bankButtons((S.banks || []).filter(bankMatches))}</div>
    <p class="note">Du wirst zu deiner Bank weitergeleitet und bestätigst dort mit Login und TAN. Die Freigabe ist nur lesend
      und gilt je nach Bank 90 bis 180 Tage. Neue Konten landen im Bereich <b>${bookTitle(book)}</b>.</p>`;
  return `<section class="card"><div class="row"><div><h3 style="margin:0">Bankverbindungen ${bookTitle(book)}</h3></div>
      <span class="spacer"></span>
      <button class="btn" data-act="sync" title="Ein Abruf von Hand zählt nicht zum Tageslimit der Bank, weil du dabei bist.">Jetzt abrufen</button><button
        class="btn primary" data-act="show-banks">${S.showBanks ? 'Schließen' : 'Bank verbinden'}</button></div>
    ${when(S.showBanks, picker)}
    <div class="tbl-scroll" style="margin-top:12px">${
      rows.length
        ? `<table><thead><tr><th>Bank</th><th>Status</th><th>Freigabe bis</th><th></th></tr></thead><tbody>${rows.join('')}</tbody></table>`
        : '<p class="muted">Noch keine Bank verbunden.</p>'
    }</div></section>`;
}

function accountCard(a, household) {
  const owners = [
    [OWNER_A, nameOf(OWNER_A)],
    [OWNER_B, nameOf(OWNER_B)],
    [OWNER_JOINT, 'Gemeinsam'],
  ];
  const books = [
    [BOOK_HOUSEHOLD, 'Haushaltsbuch'],
    [BOOK_PROPERTY, 'Hausverwaltung'],
  ];
  return `<div class="card acc" data-accid="${a.id}">
      <div class="top"><div><b>${esc(accountName(a))}</b><div class="muted small">${esc(a.bank)}</div>
        <div class="iban">${esc(ibanBlocks(a.iban))}</div></div>
        <div class="bal"><b class="${a.balance < 0 ? 'neg' : ''}">${a.balance != null ? E(a.balance) : '–'}</b>
          <div class="muted small">abgerufen ${ago(a.last_synced_at)}</div></div></div>
      ${when(a.sync_error, `<div class="banner bad"><p>${esc(a.sync_error)}</p></div>`)}
      ${scheduleHTML(a)}
      <div class="row">
        <div class="field" style="flex:1 1 140px"${household ? '' : ' hidden'}><label>Gehört</label>
          <select data-acc="owner">${options(owners, a.owner)}</select></div>
        <div class="field" style="flex:2 1 180px"><label>Anzeigename</label>
          <input data-acc="display_name" value="${esc(a.display_name)}" placeholder="${esc(a.name)}"></div>
        <div class="field" style="flex:1 1 150px"><label>Bereich</label>
          <select data-acc="book">${options(books, a.book === BOOK_PROPERTY ? BOOK_PROPERTY : BOOK_HOUSEHOLD)}</select></div>
      </div>
      <label class="small"><input type="checkbox" data-acc="active"${a.active ? ' checked' : ''}> In Auswertungen einbeziehen und abrufen</label>
      <div class="row small"><label class="btn small" style="cursor:pointer">Ältere Umsätze importieren (CSV)<input type="file"
        accept=".csv,text/csv" data-import="${a.id}" hidden></label><span class="muted">Kontoauszug-Export aus dem Online-Banking</span></div>
    </div>`;
}

/** Automatic sync plan of an account (bank request limits, see syncer.Schedule). */
function scheduleHTML(a) {
  const sc = a.schedule;
  if (!sc || !a.active || a.connection_status !== 'active') return '';
  const at = iso => {
    const d = new Date(iso);
    const today = new Date();
    if (d.toDateString() === today.toDateString()) return `heute ${hm(d)}`;
    if (d.toDateString() === new Date(today.getTime() + DAY_MS).toDateString()) return `morgen ${hm(d)}`;
    return `${d.toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit' })} ${hm(d)}`;
  };
  const hours = Math.round(parseFloat(sc.interval) || 0);
  const limit = `${sc.used} von ${sc.limit} automatischen Abrufen in 24 h${sc.learned ? ' (Limit dieser Bank, automatisch erkannt)' : ''}`;
  if (sc.limited_until && new Date(sc.limited_until) > new Date())
    return `<p class="small sched warn">Bank-Limit erreicht – automatischer Abruf wieder ab ${at(sc.limited_until)}. ${limit}.</p>`;
  const next = sc.next ? `, nächster Abruf ${new Date(sc.next) <= new Date() ? 'in Kürze' : at(sc.next)}` : '';
  return `<p class="small muted sched">Automatisch etwa alle ${hours} h${next} · ${limit}.</p>`;
}

function periodSettingsHTML(series) {
  const mode = S.me.period_mode || PERIOD_SALARY;
  const selected = new Set(
    String(S.me.salary_series || '')
      .split(',')
      .filter(Boolean),
  );
  const auto = selected.size === 0;
  const list = series.filter(r => !r.ended || selected.has(String(r.id))).sort((a, b) => b.amount - a.amount);
  const modes = [
    [PERIOD_SALARY, 'von Gehalt zu Gehalt'],
    [PERIOD_CALENDAR, 'vom 1. bis Monatsende'],
  ];
  const explanation = auto
    ? 'Automatisch: der erste große Zahlungseingang (mind. 30&nbsp;% eures üblichen Monatseinkommens) zwischen dem 22. und ' +
      `dem 5.${S.me.salary_series_name ? ` Zuletzt erkannt: ${esc(S.me.salary_series_name)}.` : ''} Wähle Gehälter aus, um ` +
      'das genau festzulegen.'
    : 'Der Monat beginnt mit dem ersten der ausgewählten Gehälter. Keine Auswahl = automatisch.';
  const choices = list.length
    ? list
        .map(
          r =>
            `<label class="small"><input type="checkbox" data-setting="period" data-series="${r.id}"${
              selected.has(String(r.id)) ? ' checked' : ''
            }> ${esc(r.label)} · ${E(r.amount)} · ${esc(r.cycle)}${
              r.kind === 'einkommen' ? '' : ` <span class="muted">(${esc(r.kind)})</span>`
            }</label>`,
        )
        .join('')
    : '<p class="muted small">Noch keine regelmäßigen Eingänge erkannt.</p>';
  return `<section class="card"><h3>Budgetmonat</h3>
    <div class="filters" style="grid-template-columns:1fr 2fr">
      <div class="field"><label for="pMode">Ein Monat läuft</label><select id="pMode" data-setting="period">${options(modes, mode)}</select></div>
      <div class="field"><label for="ownNames">Weitere eigene Namen</label><input id="ownNames" data-setting="names"
        value="${esc(S.me.own_names || '')}" placeholder="z. B. Max Mustermann, Erika Mustermann"></div>
    </div>
    ${when(
      mode === PERIOD_SALARY,
      `<fieldset class="salary-pick"><legend>Welche Gehälter beginnen einen neuen Monat?</legend>
      <p class="note">${explanation}</p>${choices}</fieldset>`,
    )}
    <p class="note">Ein Budgetmonat heißt nach dem Monat, für den das Geld gedacht ist: Gehalt am 28.09. → Budgetmonat Oktober.
      Solange ein fälliges Gehalt noch nicht gebucht ist, läuft der alte Monat weiter (höchstens 7 Tage). Überweisungen auf
      eigene Namen zu nicht verbundenen Konten zählen als „Sparen“.</p></section>`;
}

function rulesHTML(rules) {
  const catName = r => S.cats.find(c => c.id === r.category_id)?.name || r.category_slug;
  const rows = rules.map(
    r => `<tr><td>${RULE_FIELDS[r.field]}</td><td><b>${esc(r.pattern)}</b></td><td>${esc(catName(r))}</td>
      <td class="r"><button class="btn ghost small danger" data-act="del-rule" data-id="${r.id}">Löschen</button></td></tr>`,
  );
  return `<section class="ledger"><header><h2>Eigene Regeln</h2><span class="sum">entstehen, wenn du einen Umsatz umsortierst
      und „Alle … so zuordnen“ wählst</span></header>
    ${
      rules.length
        ? `<div class="tbl-scroll"><table><thead><tr><th>Wenn</th><th>enthält</th><th>dann</th><th></th></tr></thead>
      <tbody>${rows.join('')}</tbody></table></div>`
        : '<div class="empty">Noch keine eigenen Regeln.</div>'
    }</section>`;
}

function bankButtons(list) {
  if (!list.length) return '<div class="empty">Keine Bank gefunden.</div>';
  return list
    .slice(0, LIMITS.banks)
    .map(
      b =>
        `<button data-act="connect" data-bank="${esc(b.name)}" data-country="${esc(b.country)}">${when(
          b.logo,
          `<img src="${esc(b.logo)}" alt="" loading="lazy">`,
        )}<span>${esc(b.name)}${when(b.beta, ' <span class="src">Beta</span>')}</span><span class="spacer"></span>` +
        `<span class="muted small">${b.consent_days ? b.consent_days + ' Tage' : ''}</span></button>`,
    )
    .join('');
}

registerView('konten', () => viewAccounts(BOOK_HOUSEHOLD));
registerView('hv-konten', () => viewAccounts(BOOK_PROPERTY));
const reload = () => viewAccounts(S.area);

/** Any field of an account card saves the whole card. */
onChange('[data-acc]', async el => {
  const card = el.closest('[data-accid]');
  const field = name => card.querySelector(`[data-acc="${name}"]`);
  const book = field('book').value;
  await api('PATCH', `/api/accounts/${card.dataset.accid}`, {
    owner: field('owner').value,
    display_name: field('display_name').value,
    active: field('active').checked,
    book,
  });
  const moved = book !== S.area;
  toast(moved ? 'Konto verschoben' : 'Konto gespeichert', TIMING.toastShort);
  if (moved) reload();
});
onChange('[data-setting="period"]', async () => {
  const ids = $$('[data-series]:checked')
    .map(c => c.dataset.series)
    .join(',');
  await api('PUT', '/api/settings', { period_mode: $('#pMode').value, salary_series: ids });
  S.me = await get('/api/me');
  S.month = S.me.current_month;
  toast('Budgetmonat gespeichert', TIMING.toastShort);
  viewAccounts(BOOK_HOUSEHOLD);
});
onChange('[data-setting="names"]', async el => {
  await api('PUT', '/api/settings', { own_names: el.value });
  toast('Gespeichert, Umsätze neu zugeordnet', TIMING.toastShort);
});
onChange('[data-import]', async el => {
  const file = el.files[0];
  if (!file) return;
  toast('Importiere …', 0);
  try {
    const d = await upload(`/api/accounts/${el.dataset.import}/import`, file);
    toast(
      `${d.added} Umsätze importiert (${dmy(d.from)} bis ${dmy(d.to)}), ${d.skipped} schon vorhanden und übersprungen.`,
      TIMING.toastLong,
    );
    reload();
  } finally {
    el.value = '';
  }
});
onClick('show-banks', async () => {
  S.showBanks = !S.showBanks;
  if (S.showBanks && !S.banks) S.banks = await get('/api/banks');
  reload();
});
onClick('connect', async el => {
  el.disabled = true;
  const r = await api('POST', '/api/connections', { bank: el.dataset.bank, country: el.dataset.country, book: S.area });
  location.href = r.url;
});
onClick('disconnect', async el => {
  if (!confirmed(el, 'Wirklich trennen?')) return;
  await api('DELETE', `/api/connections/${el.dataset.id}`);
  toast('Verbindung getrennt', TIMING.toastMedium);
  reload();
});
onClick('del-rule', async el => {
  await api('DELETE', `/api/rules/${el.dataset.id}`);
  toast('Regel gelöscht', TIMING.toastShort);
  viewAccounts(BOOK_HOUSEHOLD);
});
