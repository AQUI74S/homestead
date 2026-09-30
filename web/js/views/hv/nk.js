// Utility settlement (Nebenkostenabrechnung) per property and year, with printable statements.
import { E, cents, dmy, esc } from '../../core/format.js';
import { $, $$, options, render, when } from '../../core/dom.js';
import { api, get } from '../../core/api.js';
import { HV } from '../../core/state.js';
import { onChange, onClick, onSubmit } from '../../core/actions.js';
import { registerView } from '../../core/shell.js';
import { ALLOCATION_KEYS, costTypeName, loadMeta } from './common.js';

/** Years offered in the year select (this year and the previous ones). */
const YEARS_BACK = 4;

async function viewNK() {
  await loadMeta(true);
  renderNK();
}

async function renderNK() {
  const props = HV.meta.properties;
  if (!props.length) return render('<div class="card empty">Lege zuerst ein Objekt an.</div>');
  if (!props.some(p => p.id === HV.nkProp)) HV.nkProp = props[0].id;
  const d = await get(`/api/hv/nk?property_id=${HV.nkProp}&year=${HV.nkYear}`);
  const st = d.settlement;
  const thisYear = new Date().getFullYear();
  const years = Array.from({ length: YEARS_BACK }, (_, i) => [thisYear - i, thisYear - i]);
  const keys = Object.entries(ALLOCATION_KEYS);
  const allocable = HV.meta.cost_types.filter(c => c.umlagefaehig).map(c => [c.slug, c.name]);

  const costs = st.costs.map(
    c => `<tr><td>${esc(c.name)}</td>
      <td><select class="inline" data-nkkey="${c.cost_type}">${options(keys, c.key)}</select></td>
      <td class="r tnum">${E(c.from_bank)}</td><td class="r tnum">${c.manual ? E(c.manual) : ''}</td>
      <td class="r tnum"><b>${E(c.total)}</b></td></tr>`,
  );
  const manual = d.manual_costs.map(
    m => `<li><span class="date-chip">+</span>
      <span>${esc(costTypeName(m.cost_type))} ${m.note ? '· ' + esc(m.note) : ''}</span>
      <span class="tnum">${E(m.amount)} <button class="btn ghost small danger" data-act="hv-del-mc" data-id="${m.id}">×</button></span></li>`,
  );

  render(`<section class="card no-print"><div class="filters nk">
      <div class="field"><label for="nkProp">Objekt</label><select id="nkProp">${options(
        props.map(p => [p.id, p.name]),
        HV.nkProp,
      )}</select></div>
      <div class="field"><label for="nkYear">Abrechnungsjahr</label><select id="nkYear">${options(years, HV.nkYear)}</select></div>
      <div class="field"><label>Frist</label><div class="tnum" style="padding:8px 0">Zustellung an die Mieter bis <b>${dmy(st.deadline)}</b></div></div>
    </div></section>
    <section class="ledger no-print"><header><h2>Umlagefähige Kosten ${st.year}</h2>
      <span class="sum">gesamt <b>${E(st.cost_total)}</b> · Eigentümeranteil (Leerstand) ${E(st.owner_share)}</span></header>
      <div class="tbl-scroll"><table><thead><tr><th>Kostenart</th><th>Verteilung nach</th><th class="r">Mietkonto</th>
        <th class="r">Ergänzt</th><th class="r">Gesamt</th></tr></thead><tbody>
        ${costs.join('') || '<tr><td colspan="5" class="empty">Keine umlagefähigen Kosten für dieses Jahr gefunden.</td></tr>'}
      </tbody></table></div>
      <form id="mcForm" class="row small" style="padding:10px 14px">
        <b>Kosten ergänzen</b> <span class="muted">(z. B. aus der WEG-Abrechnung oder von einem anderen Konto)</span>
        <select id="mcType" class="inline">${options(allocable)}</select>
        <input id="mcAmount" inputmode="decimal" placeholder="Betrag €" required style="width:100px">
        <input id="mcNote" placeholder="Notiz" style="width:160px">
        <button class="btn small" type="submit">Hinzufügen</button></form>
      ${when(manual.length, () => `<ul class="list" style="padding:0 14px 10px">${manual.join('')}</ul>`)}
      <p class="note" style="padding:0 14px 12px">Heizkosten sind hier vereinfacht nach Fläche verteilt. Gesetzlich müssen sie
        meist zu 50–70 % nach Verbrauch abgerechnet werden (Heizkostenverordnung). Nimm dafür die Verbrauchsabrechnung (ista,
        Techem, …) und trage den Betrag je Mieter manuell ein.</p></section>
    ${
      st.statements.map(s => statementHTML(s, st, d.property)).join('') ||
      '<div class="card empty">In diesem Jahr gab es für das Objekt keine Mietverträge.</div>'
    }`);
}

/** The settlement of one tenant, printable on its own. */
function statementHTML(s, st, property) {
  const totalOf = type => st.costs.find(c => c.cost_type === type)?.total;
  const shares = s.shares.map(
    x => `<tr><td>${esc(x.name)}</td><td class="r tnum">${E(totalOf(x.cost_type))}</td>
      <td class="small muted">${esc(ALLOCATION_KEYS[x.key])}: ${esc(x.basis)}</td><td class="r tnum">${E(x.amount)}</td></tr>`,
  );
  const backpay = s.result > 0;
  return `<section class="card statement" id="stmt-${s.lease_id}">
      <div class="row"><div><h3 style="margin:0">Nebenkostenabrechnung ${st.year}</h3>
        <div class="muted small">${esc(property.name)}${property.address ? ', ' + esc(property.address) : ''}</div></div>
        <span class="spacer"></span>
        <button class="btn small no-print" data-act="hv-print" data-id="${s.lease_id}">Drucken / PDF</button></div>
      <p><b>${esc(s.tenant)}</b> · ${esc(s.unit)} · Zeitraum ${dmy(s.from)} – ${dmy(s.to)} (${s.days} Tage)</p>
      <div class="tbl-scroll"><table><thead><tr><th>Kostenart</th><th class="r">Gesamtkosten</th><th>Verteilung</th>
        <th class="r">Ihr Anteil</th></tr></thead><tbody>${shares.join('')}</tbody><tfoot>
        <tr><td colspan="3">Ihre Kosten</td><td class="r tnum">${E(s.cost_sum)}</td></tr>
        <tr><td colspan="3">abzüglich geleistete Vorauszahlungen</td><td class="r tnum">${E(-s.prepaid)}</td></tr>
        <tr><td colspan="3"><b>${backpay ? 'Nachzahlung' : 'Guthaben'}</b></td>
          <td class="r tnum"><b class="${backpay ? 'neg' : 'pos'}">${E(Math.abs(s.result))}</b></td></tr>
      </tfoot></table></div>
      <p class="note">Vorschlag für die neue monatliche Vorauszahlung: ${E(s.suggested_prepay)}. ${
        backpay
          ? 'Die Nachzahlung ist innerhalb von 30 Tagen nach Zugang fällig.'
          : 'Das Guthaben wird erstattet bzw. verrechnet.'
      }</p>
    </section>`;
}

registerView('hv-nk', viewNK);

onChange('#nkProp', el => {
  HV.nkProp = +el.value;
  renderNK();
});
onChange('#nkYear', el => {
  HV.nkYear = +el.value;
  renderNK();
});
onChange('[data-nkkey]', async el => {
  await api('PUT', '/api/hv/nk-keys', { property_id: HV.nkProp, cost_type: el.dataset.nkkey, key: el.value });
  renderNK();
});
onSubmit('#mcForm', async () => {
  await api('POST', '/api/hv/manual-costs', {
    property_id: HV.nkProp,
    year: HV.nkYear,
    cost_type: $('#mcType').value,
    amount: cents($('#mcAmount').value),
    note: $('#mcNote').value,
  });
  renderNK();
});
onClick('hv-del-mc', async el => {
  await api('DELETE', '/api/hv/manual-costs/' + el.dataset.id);
  renderNK();
});
onClick('hv-print', el => {
  for (const s of $$('.statement')) s.classList.toggle('print-target', s.id === 'stmt-' + el.dataset.id);
  window.print();
});
