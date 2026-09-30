// Subscriptions & fixed costs: detected recurring payments and contracts added by hand.
import { CYCLES, CYCLE_MONTHS, DEFAULT_CYCLE, KINDS, TIMING } from '../core/constants.js';
import { E, E0, amountInput, dmy, esc, parseDE } from '../core/format.js';
import { $, confirmed, options, render, scrollToTop, toast, when } from '../core/dom.js';
import { api, get } from '../core/api.js';
import { S, accountName, catById, emptyTxFilter } from '../core/state.js';
import { onChange, onClick, onSubmit } from '../core/actions.js';
import { registerView } from '../core/shell.js';
import { categoryOptions } from './transactions.js';

/** Bounds the catch-up loop of nextDue (520 weeks = 10 years). */
const MAX_CATCH_UP = 520;

/** The next due date on or after today, starting from a past payment. */
function nextDue(dateStr, cycle) {
  if (!dateStr) return '';
  const d = new Date(dateStr + 'T00:00:00Z');
  const today = new Date();
  today.setUTCHours(0, 0, 0, 0);
  const months = CYCLE_MONTHS[cycle];
  for (let i = 0; d < today && i < MAX_CATCH_UP; i++) {
    if (months) d.setUTCMonth(d.getUTCMonth() + months);
    else d.setUTCDate(d.getUTCDate() + cycle);
  }
  return d.toISOString().slice(0, 10);
}

function updateNextHint() {
  const date = $('#cNext');
  const hint = $('#cNextHint');
  if (!date || !hint) return;
  const next = nextDue(date.value, +$('#cCycle').value);
  hint.textContent = next && next !== date.value ? `→ nächste Fälligkeit ${dmy(next)}` : '';
}

const kindOptions = selected => options(Object.entries(KINDS), selected);

function rowHTML(r) {
  const cat = r.category_id && catById(r.category_id) ? esc(catById(r.category_id).name) + ' · ' : '';
  const origin = r.manual ? 'von Hand angelegt' : `seit ${dmy(r.first_date)} · ${r.occurrences}× erkannt`;
  const stateButton = (status, label, title) =>
    `<button class="icon-btn ${r.status === status ? 'on' : ''}" data-act="rec-status" data-status="${status}" ` +
    `data-cur="${r.status}" data-id="${r.id}" title="${title}">${label}</button>`;
  const extra = r.manual
    ? `<button class="icon-btn" data-act="del-contract" data-id="${r.id}" title="Vertrag löschen">🗑</button>`
    : `<button class="icon-btn" data-act="rec-tx" data-id="${r.id}" title="Zahlungen ansehen">≡</button>`;
  return `<tr class="${r.status === 'ignored' ? 'ignored' : ''} ${r.ended ? 'ended' : ''}">
      <td><b>${esc(r.label)}</b><br><span class="muted small">${cat}${origin}</span></td>
      <td><span class="cycle c${r.cycle_days}">${esc(r.cycle)}</span></td>
      <td class="r tnum">${E(r.amount)}</td>
      <td class="r tnum"><b>${E(r.monthly)}</b></td>
      <td class="tnum">${r.ended ? `<span class="muted">zuletzt ${dmy(r.last_date)}</span>` : dmy(r.next_date)}</td>
      <td><select class="inline" data-kind="${r.id}" aria-label="Art">${kindOptions(r.kind)}</select></td>
      <td><div class="state-btns">
        ${stateButton('confirmed', '✓', 'Bestätigen')}
        ${stateButton('ignored', '✕', 'Ignorieren (zählt nicht in Summen)')}
        ${extra}</div></td></tr>`;
}

const tableHead = lastLabel =>
  `<thead><tr><th>Zahlung</th><th>Rhythmus</th><th class="r">Betrag</th><th class="r">Monatlich</th><th>${lastLabel}</th>` +
  '<th>Art</th><th></th></tr></thead>';

function tableHTML(items, title, color, note) {
  if (!items.length) return '';
  const monthly = items.filter(r => r.status !== 'ignored').reduce((a, r) => a + r.monthly, 0);
  return `<section class="ledger" style="--gc:var(${color})"><header><h2>${title}</h2>
      <span class="sum"><b>${E(monthly)}</b> pro Monat${when(note, ` · ${note}`)}</span></header>
    <div class="tbl-scroll"><table>${tableHead('Nächste')}<tbody>${items.map(rowHTML).join('')}</tbody></table></div></section>`;
}

function contractFormHTML() {
  const pf = S.prefill || {};
  const accountOpts = [['', '–'], ...S.accs.map(a => [a.id, `${accountName(a)} · ${a.bank}`])];
  return `<form id="contractForm" class="filters" style="margin-top:12px">
    <div class="field"><label for="cLabel">Name</label>
      <input id="cLabel" required placeholder="z. B. Kfz-Versicherung" value="${esc(pf.label || '')}"></div>
    <div class="field"><label for="cAmount">Betrag €</label>
      <input id="cAmount" inputmode="decimal" required placeholder="612,00" value="${amountInput(pf.amount)}"></div>
    <div class="field"><label for="cCycle">Rhythmus</label><select id="cCycle">${options(CYCLES, DEFAULT_CYCLE)}</select></div>
    <div class="field"><label for="cNext">Letzte oder nächste Zahlung</label>
      <input id="cNext" type="date" required value="${esc(pf.date || '')}"><span class="muted small" id="cNextHint"></span></div>
    <div class="field"><label for="cKind">Art</label><select id="cKind">${kindOptions(pf.kind || 'fixkosten')}</select></div>
    <div class="field"><label for="cCat">Kategorie</label>
      <select id="cCat"><option value="">–</option>${categoryOptions(pf.category_id || '', false)}</select></div>
    <div class="field"><label for="cAcc">Abbuchung von</label><select id="cAcc">${options(accountOpts, pf.account_id)}</select></div>
    <div class="field" style="align-self:end"><button class="btn primary" type="submit">Speichern</button></div>
  </form>`;
}

async function viewRecurring() {
  const [rec, accs] = await Promise.all([get('/api/recurring'), get('/api/accounts')]);
  S.accs = accs || [];
  const live = rec.filter(r => !r.ended && r.status !== 'ignored');
  const monthlyOf = k => live.filter(r => r.direction === 'out' && r.kind === k).reduce((a, r) => a + r.monthly, 0);
  const incomeMonthly = live.filter(r => r.direction === 'in').reduce((a, r) => a + r.monthly, 0);
  const current = rec.filter(r => !r.ended);
  const ofKind = k => current.filter(r => r.direction === 'out' && r.kind === k);
  const ended = rec.filter(r => r.ended);
  const abo = monthlyOf('abo');
  const stat = (title, value, note) =>
    `<div class="card"><h3>${title}</h3><div class="bignum">${value}</div><p class="note">${note}</p></div>`;

  render(`<section class="stat-grid">
      ${stat('Abos pro Monat', E(abo), `${E0(abo * 12)} im Jahr · ${ofKind('abo').filter(r => r.status !== 'ignored').length} aktive Abos`)}
      ${stat('Fixkosten pro Monat', E(monthlyOf('fixkosten')), 'Energie, Versicherungen, Telefon & Co.')}
      ${stat(
        'Kredite &amp; Sparen',
        E(monthlyOf('kredit') + monthlyOf('sparen')),
        `Kredite ${E(monthlyOf('kredit'))} · Sparen ${E(monthlyOf('sparen'))}`,
      )}
      ${stat('Regelmäßige Einnahmen', E(incomeMonthly), 'Gehalt, Kindergeld und andere feste Eingänge')}
    </section>
    <section class="card" id="addContract">
      <div class="row"><h3 style="margin:0">Vertrag von Hand anlegen</h3><span class="spacer"></span>
        <button class="btn small" data-act="toggle-contract">${S.showContract ? 'Schließen' : 'Hinzufügen'}</button></div>
      <p class="note" style="margin-top:6px">Für Zahlungen, die im Abruf noch fehlen, z. B. jährliche Versicherungen oder
        halbjährliche Beiträge. Als Datum reicht die letzte Abbuchung, den nächsten Termin rechnet die App aus. Am schnellsten
        geht es über „↻ Als Vertrag“ direkt in der Umsatzliste.</p>
      ${when(S.showContract, contractFormHTML())}
    </section>
    <p class="note" style="margin:0">Erkannt anhand von Rhythmus und Betrag über die letzten zwei Jahre. Mit ✓ bestätigst du
      eine Zahlung, mit ✕ blendest du sie aus. Die Art lässt sich per Auswahl ändern und bleibt dann fest.</p>
    ${tableHTML(ofKind('abo'), 'Abos', '--c-expenses', 'im Jahr ' + E0(abo * 12))}
    ${tableHTML(ofKind('fixkosten'), 'Fixkosten', '--c-bills')}
    ${tableHTML(ofKind('kredit'), 'Kredite', '--c-debts')}
    ${tableHTML(ofKind('sparen'), 'Sparen', '--c-savings')}
    ${tableHTML(
      current.filter(r => r.direction === 'in'),
      'Einnahmen',
      '--c-income',
    )}
    ${tableHTML(ofKind('sonstiges'), 'Sonstige Regelmäßigkeiten', '--c-transfer', 'prüfen, ob etwas davon ein Abo ist')}
    ${when(
      ended.length,
      `<section class="ledger"><details class="ended">
      <summary>Beendet (${ended.length}): keine Zahlung mehr im erwarteten Rhythmus</summary>
      <div class="tbl-scroll"><table>${tableHead('Letzte')}<tbody>${ended.map(rowHTML).join('')}</tbody></table></div>
      </details></section>`,
    )}
    ${when(
      !rec.length,
      '<div class="card empty">Noch keine wiederkehrenden Zahlungen erkannt. Dafür braucht es ein paar ' +
        'Monate Kontoumsätze.</div>',
    )}`);
  updateNextHint();
  if (S.showContract && S.prefill) scrollToTop($('#addContract'));
}

registerView('abos', viewRecurring);

onChange('#cNext, #cCycle', updateNextHint);
onChange('[data-kind]', async el => {
  await api('PATCH', `/api/recurring/${el.dataset.kind}`, { kind: el.value });
  viewRecurring();
});
onSubmit('#contractForm', async () => {
  const amount = parseDE($('#cAmount').value);
  if (!amount) return toast('Betrag angeben, z. B. 612,00');
  await api('POST', '/api/recurring', {
    label: $('#cLabel').value,
    amount: Math.abs(amount),
    cycle_days: +$('#cCycle').value,
    next_date: $('#cNext').value,
    kind: $('#cKind').value,
    category_id: +$('#cCat').value || 0,
    account_id: +$('#cAcc').value || 0,
  });
  S.showContract = false;
  S.prefill = null;
  toast('Vertrag angelegt, erscheint jetzt in der Prognose', TIMING.toastMedium);
  viewRecurring();
});
onClick('rec-status', async el => {
  // clicking the active state again resets it
  const next = el.dataset.cur === el.dataset.status ? 'detected' : el.dataset.status;
  await api('PATCH', `/api/recurring/${el.dataset.id}`, { status: next });
  viewRecurring();
});
onClick('rec-tx', el => {
  S.txFilter = { ...emptyTxFilter(), recurring: el.dataset.id };
  S.month = '';
  location.hash = '#umsaetze';
});
onClick('toggle-contract', () => {
  S.showContract = !S.showContract;
  if (!S.showContract) S.prefill = null;
  viewRecurring();
});
onClick('del-contract', async el => {
  if (!confirmed(el, '?')) {
    el.title = 'Nochmal klicken zum Löschen';
    return;
  }
  await api('DELETE', `/api/recurring/${el.dataset.id}`);
  toast('Vertrag gelöscht', TIMING.toastShort);
  viewRecurring();
});
