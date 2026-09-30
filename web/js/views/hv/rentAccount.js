// Rent account (Mietkonto): assign transactions to property, lease and cost type.
import { TIMING } from '../../core/constants.js';
import { E, dmy, esc } from '../../core/format.js';
import { options, render, toast, when } from '../../core/dom.js';
import { api, get, query } from '../../core/api.js';
import { HV } from '../../core/state.js';
import { onChange, onClick } from '../../core/actions.js';
import { registerView } from '../../core/shell.js';
import { loadMeta, yearSwitch } from './common.js';

const COST = 'kosten'; // cost type kind of expenses

function costTypeOptions(selected) {
  const types = HV.meta.cost_types;
  const income = types.filter(c => c.kind !== COST).map(c => [c.slug, c.name]);
  const costs = types
    .filter(c => c.kind === COST)
    .map(c => [c.slug, c.name + (c.umlagefaehig ? ' (umlagefähig)' : '')]);
  return `<optgroup label="Einnahmen / neutral">${options(income, selected)}</optgroup>
    <optgroup label="Kosten">${options(costs, selected)}</optgroup>`;
}

function rowHTML(t) {
  const props = [['', '–'], ...HV.meta.properties.map(p => [p.id, p.name])];
  const leases = [['', '–'], ...HV.meta.leases.map(l => [l.id, `${l.tenant.name} · ${l.unit_name}`])];
  return `<tr data-hvtx="${t.id}">
      <td class="tnum">${dmy(t.date)}</td>
      <td><div class="tx-main"><b>${esc(t.merchant || t.counterparty || '–')}</b>
        <span class="muted small clip" title="${esc(t.remittance)}">${esc(t.remittance)}</span></div></td>
      <td><select class="inline" data-f="property">${options(props, t.property_id ?? '')}</select></td>
      <td>${when(t.amount > 0, () => `<select class="inline" data-f="lease">${options(leases, t.lease_id ?? '')}</select>`)}</td>
      <td><select class="inline" data-f="cost">${costTypeOptions(t.cost_type)}</select>${when(
        t.hv_source === 'manual',
        ' <span class="src manual">Hand</span>',
      )}<br>
        <label class="small muted"><input type="checkbox" data-f="remember"> merken</label></td>
      <td class="r tnum ${t.amount > 0 ? 'amt-in' : 'amt-out'}">${E(t.amount)}</td></tr>`;
}

async function viewRentAccount() {
  await loadMeta(true);
  renderRentAccount();
}

async function renderRentAccount() {
  const txs = (await get('/api/hv/transactions?' + query({ year: HV.txYear, open: HV.txOpen && '1' }))) || [];
  render(`<div class="row">${yearSwitch(HV.txYear, 'hv-tx-year')}<span class="spacer"></span>
      <label class="small"><input type="checkbox" id="txOpen"${HV.txOpen ? ' checked' : ''}> Nur nicht zugeordnete</label></div>
    <section class="ledger"><header><h2>Mietkonto ${HV.txYear}</h2><span class="sum">${txs.length} Umsätze · Änderungen gelten
      für die Zeile, mit „merken“ für alle vom selben Empfänger</span></header>
      <div class="tbl-scroll">${
        txs.length
          ? `<table><thead><tr><th>Datum</th><th>Empfänger / Zweck</th><th>Objekt</th>
        <th>Mietvertrag</th><th>Art</th><th class="r">Betrag</th></tr></thead><tbody>${txs.map(rowHTML).join('')}</tbody></table>`
          : '<div class="empty">Keine Umsätze. Ist das Mietkonto unter „Konten“ der Hausverwaltung zugeordnet?</div>'
      }</div></section>`);
}

registerView('hv-umsaetze', viewRentAccount);

onClick('hv-tx-year', el => {
  HV.txYear += +el.dataset.step;
  renderRentAccount();
});
onChange('#txOpen', el => {
  HV.txOpen = el.checked;
  renderRentAccount();
});
/** Changing property, lease or cost type saves the row ("merken" only marks it). */
onChange('[data-hvtx] [data-f]:not([data-f="remember"])', async el => {
  const row = el.closest('[data-hvtx]');
  const f = name => row.querySelector(`[data-f="${name}"]`);
  const remember = f('remember').checked;
  await api('PATCH', '/api/hv/transactions/' + row.dataset.hvtx, {
    property_id: +f('property').value || 0,
    lease_id: +(f('lease')?.value || 0),
    cost_type: f('cost').value,
    remember,
  });
  toast(remember ? 'Gespeichert und für diesen Empfänger gemerkt' : 'Gespeichert', TIMING.toastShort);
  if (remember) renderRentAccount();
});
