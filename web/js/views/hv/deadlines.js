// Deadlines: derived from leases and arrears, plus reminders added by hand.
import { TIMING } from '../../core/constants.js';
import { dmy, esc } from '../../core/format.js';
import { $, options, render, toast, when } from '../../core/dom.js';
import { api } from '../../core/api.js';
import { HV } from '../../core/state.js';
import { onClick, onSubmit } from '../../core/actions.js';
import { registerView } from '../../core/shell.js';
import { DEADLINE_KINDS, loadMeta, loadReport } from './common.js';

async function viewDeadlines() {
  await loadMeta(true);
  renderDeadlines();
}

async function renderDeadlines() {
  const rep = await loadReport(new Date().getFullYear());
  const props = [['', '–'], ...HV.meta.properties.map(p => [p.id, p.name])];
  const rows = (rep.deadlines || []).map(
    d => `<tr class="${d.urgent ? 'urgent-row' : ''}"><td class="tnum">${dmy(d.date)}</td>
      <td><span class="pill">${DEADLINE_KINDS[d.kind] || d.kind}</span></td>
      <td><b>${esc(d.title)}</b><br><span class="muted small">${esc(d.detail || '')}</span></td>
      <td class="r">${when(d.reminder_id, () => `<button class="btn small" data-act="hv-rem-done" data-id="${d.reminder_id}">Erledigt</button>`)}</td></tr>`,
  );
  render(`<section class="card"><h3>Wiedervorlage anlegen</h3><form id="remForm" class="form-grid">
      <div class="field"><label>Was</label><input id="rTitle" required placeholder="z. B. Rauchmelder prüfen, Zählerstände ablesen"></div>
      <div class="field"><label>Wann</label><input id="rDate" type="date" required></div>
      <div class="field"><label>Objekt</label><select id="rProp">${options(props, '')}</select></div>
      <div class="field" style="align-self:end"><button class="btn primary" type="submit">Anlegen</button></div></form></section>
    <section class="ledger"><header><h2>Fristen &amp; Hinweise</h2>
      <span class="sum">automatisch aus Verträgen, Rückständen und deinen Wiedervorlagen</span></header>
      <div class="tbl-scroll"><table><thead><tr><th>Datum</th><th>Art</th><th>Was</th><th></th></tr></thead><tbody>
        ${rows.join('') || '<tr><td colspan="4" class="empty">Keine Fristen.</td></tr>'}
      </tbody></table></div></section>
    <p class="note">Mieterhöhungen auf die Vergleichsmiete werden frühestens 15 Monate nach der letzten Erhöhung wirksam, das
      Verlangen muss 2 Monate vorher zugehen. Nebenkostenabrechnungen müssen spätestens 12 Monate nach Ende des
      Abrechnungszeitraums beim Mieter sein. Diese Hinweise ersetzen keine Rechtsberatung.</p>`);
}

registerView('hv-fristen', viewDeadlines);

onSubmit('#remForm', async () => {
  await api('POST', '/api/hv/reminders', {
    title: $('#rTitle').value.trim(),
    due_date: $('#rDate').value,
    property_id: +$('#rProp').value || null,
  });
  toast('Wiedervorlage angelegt', TIMING.toastShort);
  renderDeadlines();
});
onClick('hv-rem-done', async el => {
  await api('PATCH', '/api/hv/reminders/' + el.dataset.id, { done: true });
  renderDeadlines();
});
