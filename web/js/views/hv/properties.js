// Properties: create properties and units, annual income and costs per property.
import { TIMING } from '../../core/constants.js';
import { E, cents, esc, m2, pct2 } from '../../core/format.js';
import { $, confirmed, render, toast, when } from '../../core/dom.js';
import { api } from '../../core/api.js';
import { HV } from '../../core/state.js';
import { onClick, onSubmit } from '../../core/actions.js';
import { registerView } from '../../core/shell.js';
import { costTypeName, loadMeta, loadReport, yearSwitch } from './common.js';

function propertySection(p, r) {
  const costs = Object.entries(r.costs_by_type || {}).sort((a, b) => b[1] - a[1]);
  const units = p.units.map(
    u => `<tr><td>${esc(u.name)}</td><td class="r tnum">${m2(u.area)}</td>
      <td class="r"><button class="btn ghost small danger" data-act="hv-del-unit" data-id="${u.id}">×</button></td></tr>`,
  );
  const small = (label, value) =>
    `<tr><td class="muted small">${label}</td><td class="r tnum small">${value}</td></tr>`;
  return `<section class="ledger"><header><h2>${esc(p.name)}</h2><span class="sum">${esc(p.address)}</span>
      <button class="btn ghost small danger" data-act="hv-del-prop" data-id="${p.id}">Objekt löschen</button></header>
    <div class="lease-grid" style="padding:0 14px 14px">
      <div><h3 class="label">Einheiten</h3><table><thead><tr><th>Einheit</th><th class="r">Fläche</th><th></th></tr></thead><tbody>
        ${units.join('')}
        <tr><td colspan="3"><form class="row small unitForm" data-prop="${p.id}">
          <input name="name" placeholder="z. B. EG links" required style="flex:2">
          <input name="area" inputmode="decimal" placeholder="m²" required style="width:80px">
          <button class="btn small" type="submit">Einheit hinzufügen</button></form></td></tr>
        </tbody></table>
        <p class="note">${r.occupied || 0} von ${p.units.length} Einheiten vermietet. Kaufpreis ${p.purchase_price ? E(p.purchase_price) : '–'}.</p></div>
      <div><h3 class="label">${HV.year}</h3><table class="fc"><tbody>
        <tr><td>Einnahmen (Mieten inkl. NK)</td><td class="r tnum pos">${E(r.income)}</td></tr>
        ${costs.map(([k, v]) => `<tr><td class="muted">− ${esc(costTypeName(k))}</td><td class="r tnum">${E(-v)}</td></tr>`).join('')}
        <tr class="total"><td>Überschuss (Cashflow)</td><td class="r tnum ${r.surplus < 0 ? 'neg' : ''}">${E(r.surplus)}</td></tr>
        ${small('davon umlagefähige Kosten', E(r.costs_umlage))}
        ${small('Soll-Kaltmiete p. a. (aktuell)', E(r.cold_rent_year))}
        ${when(r.gross_yield, () => small('Bruttomietrendite', `${pct2(r.gross_yield)} %`))}
      </tbody></table><p class="note">Darlehensraten enthalten Tilgung. Steuerlich absetzbar sind nur die Zinsen.</p></div>
    </div></section>`;
}

async function viewProperties() {
  await loadMeta(true);
  renderProperties();
}

async function renderProperties() {
  const rep = await loadReport(HV.year);
  const reportOf = Object.fromEntries((rep.properties || []).map(p => [p.property_id, p]));
  const sections = HV.meta.properties.map(p => propertySection(p, reportOf[p.id] || { costs_by_type: {} }));
  render(`<section class="card"><h3>Neues Objekt</h3><form id="propForm" class="form-grid">
      <div class="field"><label>Name</label><input id="pName" required placeholder="z. B. Talstraße 3"></div>
      <div class="field"><label>Adresse</label><input id="pAddr" placeholder="Straße Nr., PLZ Ort"></div>
      <div class="field"><label>Kaufpreis € (für Rendite)</label><input id="pPrice" inputmode="decimal"></div>
      <div class="field" style="align-self:end"><button class="btn primary" type="submit">Anlegen</button></div></form></section>
    <div class="row"><h3 class="label" style="margin:0">Jahresübersicht</h3><span class="spacer"></span>${yearSwitch(HV.year, 'hv-year')}</div>
    ${sections.join('') || '<div class="card empty">Noch keine Objekte angelegt.</div>'}`);
}

/** Reloads properties after a change. */
async function refresh() {
  await loadMeta(true);
  renderProperties();
}

registerView('hv-objekte', viewProperties);

onClick('hv-year', el => {
  HV.year += +el.dataset.step;
  renderProperties();
});
onClick('hv-del-prop', async el => {
  if (!confirmed(el, 'Wirklich löschen?')) return;
  await api('DELETE', '/api/hv/properties/' + el.dataset.id);
  toast('Objekt gelöscht', TIMING.toastShort);
  refresh();
});
onClick('hv-del-unit', async el => {
  if (!confirmed(el, 'Wirklich löschen?')) return;
  await api('DELETE', '/api/hv/units/' + el.dataset.id);
  toast('Einheit gelöscht', TIMING.toastShort);
  refresh();
});
onSubmit('#propForm', async () => {
  const price = cents($('#pPrice').value);
  await api('POST', '/api/hv/properties', {
    name: $('#pName').value.trim(),
    address: $('#pAddr').value.trim(),
    purchase_price: price || null,
  });
  toast('Objekt angelegt. Jetzt die Einheiten ergänzen.', TIMING.toastMedium);
  refresh();
});
onSubmit('.unitForm', async form => {
  await api('POST', '/api/hv/units', {
    property_id: +form.dataset.prop,
    name: form.elements.name.value.trim(),
    area: cents(form.elements.area.value),
  });
  refresh();
});
