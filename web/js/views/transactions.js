// Transactions: filter, re-categorize, confirm, create rules, turn into a contract.
import { CATCH_ALL_SLUGS, GROUPS, LIMITS, SOURCE_LABELS, TIMING, UNCATEGORIZED } from '../core/constants.js';
import { E, dmy, esc, monthLabel } from '../core/format.js';
import { $, options, render, toast, when } from '../core/dom.js';
import { api, get, query } from '../core/api.js';
import { S, accountName, catById, currentMonth, nameOf } from '../core/state.js';
import { onChange, onClick } from '../core/actions.js';
import { registerView, route } from '../core/shell.js';

/** Category <option>s grouped by area; withAll adds "Alle Kategorien". */
export function categoryOptions(selected, withAll) {
  let html = withAll ? '<option value="">Alle Kategorien</option>' : '';
  for (const [g, { label }] of Object.entries(GROUPS)) {
    const cats = S.cats.filter(c => c.group === g);
    if (cats.length)
      html += `<optgroup label="${label}">${options(
        cats.map(c => [c.id, c.name]),
        selected,
      )}</optgroup>`;
  }
  return html;
}

let accounts = [];
let searchTimer;

async function viewTransactions() {
  accounts = (await get('/api/accounts')) || [];
  const f = S.txFilter;
  const accountOpts = [['', 'Alle Konten'], ...accounts.map(a => [a.id, `${accountName(a)} · ${a.bank}`])];
  const groupOpts = [['', 'Alle Bereiche'], ...Object.entries(GROUPS).map(([k, g]) => [k, g.label])];
  // "Nicht zugeordnet" right after "Alle Kategorien"
  const catOpts = categoryOptions(f.cat, true).replace(
    '</option>',
    `</option>${options([[UNCATEGORIZED, 'Nicht zugeordnet']], f.cat)}`,
  );
  render(`<section class="card" id="txFilters"><div class="filters">
      <div class="field"><label for="fq">Suche</label>
        <input id="fq" type="search" placeholder="Händler, Verwendungszweck …" value="${esc(f.q)}"></div>
      <div class="field"><label for="facc">Konto</label><select id="facc">${options(accountOpts, f.account)}</select></div>
      <div class="field"><label for="fgroup">Bereich</label><select id="fgroup">${options(groupOpts, f.group)}</select></div>
      <div class="field"><label for="fcat">Kategorie</label><select id="fcat">${catOpts}</select></div>
    </div><div class="row" style="margin-top:10px"><span class="note" style="margin:0" id="txRange">${
      S.month ? `Zeitraum: Budgetmonat ${monthLabel(S.month)} (oben umschalten)` : 'Zeitraum: alle Monate'
    }</span>
      <button class="btn ghost small" data-act="tx-month">${S.month ? 'Alle Monate zeigen' : 'Nur aktuellen Monat'}</button></div></section>
    <section class="ledger"><header><h2>Umsätze</h2><span class="sum" id="txSum"></span></header>
      <div class="tbl-scroll" id="txTable"><div class="empty">Lädt …</div></div></section>`);
  $('#fq').addEventListener('input', () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      readFilters();
      loadTransactions();
    }, TIMING.searchDebounce);
  });
  await loadTransactions();
}

function readFilters() {
  S.txFilter = {
    q: $('#fq').value,
    account: $('#facc').value,
    cat: $('#fcat').value,
    group: $('#fgroup').value,
    recurring: '',
  };
}

async function loadTransactions() {
  if (S.tab !== 'umsaetze') return;
  const f = S.txFilter;
  const txs = await get(
    '/api/transactions?' +
      query({
        month: S.month,
        q: f.q,
        account_id: f.account,
        uncategorized: f.cat === UNCATEGORIZED && '1',
        category_id: f.cat !== UNCATEGORIZED && f.cat,
        group: f.group,
        recurring_id: f.recurring,
        limit: LIMITS.transactions,
      }),
  );
  S.txCache = txs;
  if (S.tab !== 'umsaetze' || !$('#txSum')) return; // user has navigated away in the meantime
  const counted = txs.filter(t => t.group !== 'transfer');
  const inn = counted.filter(t => t.amount > 0).reduce((a, t) => a + t.amount, 0);
  const out = counted.filter(t => t.amount < 0).reduce((a, t) => a + t.amount, 0);
  $('#txSum').innerHTML = `${txs.length} Umsätze · <span class="pos">${E(inn)}</span> / <b>${E(out)}</b>`;
  $('#txTable').innerHTML = txs.length
    ? `<table><thead><tr><th>Datum</th><th>Empfänger / Zweck</th><th>Konto</th><th>Kategorie</th><th class="r">Betrag</th></tr></thead>
      <tbody>${txs.map(rowHTML).join('')}</tbody></table>`
    : '<div class="empty">Keine Umsätze für diese Auswahl.</div>';
}

function rowHTML(t) {
  const recurring = t.recurring_id
    ? ' <span class="src" title="Wiederkehrende Zahlung">↻</span>'
    : ` <button class="btn ghost small" data-act="as-contract" data-tx="${t.id}" title="Als wiederkehrenden Vertrag anlegen">↻ Als Vertrag</button>`;
  const unconfirmed = t.source === 'auto' && CATCH_ALL_SLUGS.includes(t.category_slug);
  const confirm = when(
    unconfirmed,
    ` <button class="btn small" data-act="confirm-cat" data-tx="${t.id}" data-cat="${t.category_id}" ` +
      `title="Kategorie stimmt – nicht mehr als „nicht zugeordnet“ zählen">✓ Passt so</button>`,
  );
  const badge = when(
    SOURCE_LABELS[t.source],
    `<span class="src ${t.source}" title="${esc(t.reason)}">${SOURCE_LABELS[t.source]}</span>`,
  );
  const manual = when(
    t.source === 'manual',
    ` <button class="btn ghost small" data-act="reset-tx" data-tx="${t.id}" title="Wieder automatisch zuordnen">↺</button>` +
      when(
        t.merchant,
        `<br><button class="btn small" style="margin-top:4px" data-act="rule" data-tx="${t.id}" ` +
          `data-cat="${t.category_id}">Für alle von ${esc(t.merchant)} übernehmen</button>`,
      ),
  );
  return `<tr data-merchant="${esc(t.merchant)}">
      <td class="tnum">${dmy(t.date)}</td>
      <td><div class="tx-main"><b>${esc(t.merchant || t.counterparty || '–')}${recurring}</b>
        <span class="muted small clip" title="${esc(t.remittance)}">${esc(t.remittance)}</span></div></td>
      <td class="small">${esc(t.account)}<br><span class="muted">${esc(nameOf(t.owner))}</span></td>
      <td><select class="inline" data-txcat="${t.id}" aria-label="Kategorie" title="${esc(t.reason)}">${categoryOptions(t.category_id, false)}</select>${
        confirm
      }${badge}${manual}</td>
      <td class="r tnum ${t.amount > 0 ? 'amt-in' : 'amt-out'}">${E(t.amount)}</td></tr>`;
}

/** Toast with a button that turns the choice into a rule for the whole merchant. */
function ruleOffer(text, id, cat, merchant, verb) {
  const button =
    `<button class="btn small" data-act="rule" data-tx="${id}" data-cat="${cat}">` +
    `Alle von ${esc(merchant)} so ${verb}</button>`;
  toast(`<span>${text}</span>${when(merchant, button)}`, TIMING.toastLong);
}

registerView('umsaetze', viewTransactions);

onChange('#txFilters *', () => {
  readFilters();
  loadTransactions();
});
onChange('[data-txcat]', async el => {
  const id = +el.dataset.txcat;
  const cat = +el.value;
  await api('PATCH', `/api/transactions/${id}`, { category_id: cat });
  ruleOffer(`Auf „${esc(catById(cat)?.name)}“ gesetzt.`, id, cat, el.closest('tr').dataset.merchant, 'zuordnen');
  loadTransactions();
});
onClick('confirm-cat', async el => {
  const id = +el.dataset.tx;
  const cat = +el.dataset.cat;
  await api('PATCH', `/api/transactions/${id}`, { category_id: cat });
  ruleOffer(
    `Bestätigt: bleibt bei „${esc(catById(cat)?.name)}“.`,
    id,
    cat,
    el.closest('tr')?.dataset.merchant,
    'lassen',
  );
  loadTransactions();
});
onClick('rule', async el => {
  await api('PATCH', `/api/transactions/${el.dataset.tx}`, { category_id: +el.dataset.cat, rule: true });
  toast('Regel gespeichert, alle passenden Umsätze neu zugeordnet', TIMING.toastMedium);
  loadTransactions();
});
onClick('reset-tx', async el => {
  await api('PATCH', `/api/transactions/${el.dataset.tx}`, { reset: true });
  toast('Wieder automatisch zugeordnet', TIMING.toastShort);
  loadTransactions();
});
onClick('tx-month', () => {
  S.month = S.month ? '' : currentMonth();
  route();
});
onClick('as-contract', el => {
  const t = S.txCache.find(x => x.id === +el.dataset.tx);
  if (!t) return;
  S.prefill = {
    label: t.merchant || t.counterparty,
    amount: Math.abs(t.amount),
    date: t.date,
    category_id: t.category_id,
    account_id: t.account_id,
    kind: t.amount > 0 ? 'einkommen' : 'fixkosten',
  };
  S.showContract = true;
  location.hash = '#abos';
});
