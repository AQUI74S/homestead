// Tenants & leases: list, lease form, tenant account with ledger and rent steps.
import { TIMING } from '../../core/constants.js';
import { E, amountInput, cents, dm, dmy, esc, monthLabel, todayISO } from '../../core/format.js';
import { $, confirmed, options, render, scrollToTop, toast, when } from '../../core/dom.js';
import { api, get } from '../../core/api.js';
import { HV } from '../../core/state.js';
import { onClick, onSubmit } from '../../core/actions.js';
import { registerView } from '../../core/shell.js';
import { DEFAULT_DUE_DAY, MAX_DUE_DAY, RENT_TYPES, RENT_TYPE_SHORT, loadMeta, pill } from './common.js';

function leaseForm(l) {
  const units = HV.meta.properties.flatMap(p => p.units.map(u => [u.id, `${p.name} · ${u.name}`]));
  const v = x => (x != null ? esc(x) : '');
  const field = (label, input) => `<div class="field"><label>${label}</label>${input}</div>`;
  return `<form id="leaseForm" class="card" data-id="${l.id || ''}"><h3>${l.id ? 'Mietvertrag bearbeiten' : 'Neuer Mietvertrag'}</h3>
    ${when(!units.length, '<p class="neg">Lege zuerst unter „Objekte“ ein Objekt mit Einheiten an.</p>')}
    <div class="form-grid">
      ${field('Einheit', `<select id="lUnit" required>${options(units, l.unit_id)}</select>`)}
      ${field('Mieter (Name)', `<input id="lName" required value="${v(l.tenant?.name)}" placeholder="z. B. Anna Schmidt">`)}
      ${field('IBAN des Mieters', `<input id="lIban" value="${v(l.tenant?.iban)}" placeholder="für die Zahlungszuordnung">`)}
      ${field('Weiterer Suchbegriff', `<input id="lMatch" value="${v(l.match_text)}" placeholder="falls jemand anderes überweist">`)}
      ${field('E-Mail', `<input id="lMail" value="${v(l.tenant?.email)}">`)}
      ${field('Telefon', `<input id="lPhone" value="${v(l.tenant?.phone)}">`)}
      ${field('Mietbeginn', `<input id="lStart" type="date" required value="${v(l.start_date)}">`)}
      ${field('Mietende (leer = unbefristet)', `<input id="lEnd" type="date" value="${v(l.end_date)}">`)}
      ${field('Kaltmiete €', `<input id="lCold" inputmode="decimal" required value="${amountInput(l.rent_cold)}">`)}
      ${field('NK-Vorauszahlung €', `<input id="lNK" inputmode="decimal" value="${amountInput(l.nk_prepay)}">`)}
      ${field('Fällig am (Tag)', `<input id="lDue" type="number" min="1" max="${MAX_DUE_DAY}" value="${l.due_day || DEFAULT_DUE_DAY}">`)}
      ${field('Personen', `<input id="lPers" type="number" min="0" value="${l.persons ?? 1}">`)}
      ${field('Mietart', `<select id="lType">${options(RENT_TYPES, l.rent_type)}</select>`)}
      ${field('Letzte Mieterhöhung', `<input id="lInc" type="date" value="${v(l.last_increase)}">`)}
      ${field('Kaution €', `<input id="lDep" inputmode="decimal" value="${amountInput(l.deposit)}">`)}
      ${field('Soll/Ist rechnen ab', `<input id="lTrack" type="date" value="${v(l.track_from)}" title="leer = Beginn der Kontodaten">`)}
    </div>
    <label class="small"><input type="checkbox" id="lDepPaid"${l.deposit_paid ? ' checked' : ''}> Kaution vollständig erhalten</label>
    ${field('Notizen', `<input id="lNotes" value="${v(l.notes)}">`)}
    <div class="row" style="margin-top:10px"><button class="btn primary" type="submit">Speichern</button>
      <button class="btn ghost" type="button" data-act="hv-cancel-lease">Abbrechen</button>
      ${when(
        l.id,
        () => `<span class="spacer"></span>
        <button class="btn ghost danger" type="button" data-act="hv-del-lease" data-id="${l.id}">Vertrag löschen</button>`,
      )}</div>
  </form>`;
}

function leaseDetail({ lease: l, ledger: lg }) {
  const months = lg.months.map(
    m => `<tr><td>${monthLabel(m.month)}</td><td class="r tnum">${E(m.soll)}</td>
      <td class="r tnum">${E(m.paid)}</td><td class="r tnum ${m.open ? 'neg' : ''}">${E(m.open)}</td><td>${pill(m.status)}</td></tr>`,
  );
  const payments = lg.payments
    .slice()
    .reverse()
    .map(
      p => `<li><span class="date-chip">${dm(p.date)}</span>
      <span class="small">${esc(p.remittance || p.counterparty)}</span><b class="tnum pos">${E(p.amount)}</b></li>`,
    );
  const steps = l.steps.map(
    s => `<li><span class="date-chip">${dm(s.valid_from)}${s.valid_from.slice(2, 4)}</span>
      <span>Kalt ${E(s.rent_cold)} · NK ${E(s.nk_prepay)}</span>
      <button class="btn ghost small danger" data-act="hv-del-step" data-id="${s.id}">×</button></li>`,
  );
  const emptyItem = (text, cls = 'muted small') =>
    `<li><span></span><span class="${cls}">${text}</span><span></span></li>`;
  return `<section class="ledger" id="leaseDetail"><header><h2>Mieterkonto ${esc(l.tenant.name)}</h2>
      <span class="sum">${esc(l.property_name)} · ${esc(l.unit_name)} · Saldo <b class="${lg.balance > 0 ? 'neg' : ''}">${E(lg.balance)}</b></span>
      <button class="btn ghost small" data-act="hv-close-lease">Schließen</button></header>
    <div class="lease-grid">
      <div><h3 class="label">Monate seit ${dmy(lg.from)}</h3><div class="tbl-scroll"><table><thead><tr><th>Monat</th>
        <th class="r">Soll</th><th class="r">Bezahlt</th><th class="r">Offen</th><th>Status</th></tr></thead>
        <tbody>${months.join('')}</tbody></table></div></div>
      <div><h3 class="label">Zahlungen</h3><ul class="list">${payments.join('') || emptyItem('Keine Zahlungen zugeordnet.', 'muted')}</ul>
        <h3 class="label" style="margin-top:16px">Mietänderungen / Staffel</h3>
        <ul class="list">${steps.join('') || emptyItem(`Keine. Gilt: Kalt ${E(l.rent_cold)} · NK ${E(l.nk_prepay)}`)}</ul>
        <form id="stepForm" class="row small" data-lease="${l.id}" style="margin-top:8px">
          <input type="date" id="sFrom" required aria-label="gültig ab">
          <input id="sCold" inputmode="decimal" placeholder="Kalt €" required aria-label="neue Kaltmiete" style="width:90px">
          <input id="sNK" inputmode="decimal" placeholder="NK €" aria-label="neue Vorauszahlung" style="width:80px">
          <button class="btn small" type="submit">Änderung hinzufügen</button></form>
        <p class="note">Kaution ${E(l.deposit)} ${l.deposit_paid ? 'erhalten' : '<span class="neg">noch offen</span>'} ·
          ${l.persons} Person(en) · fällig zum ${l.due_day}.</p>
      </div></div></section>`;
}

async function viewLeases() {
  await loadMeta(true);
  renderLeases();
}

async function renderLeases() {
  const today = todayISO();
  const byProperty = {};
  for (const l of HV.meta.leases) (byProperty[l.property_name] ||= []).push(l);
  const detail = HV.lease ? await get('/api/hv/leases/' + HV.lease).catch(() => null) : null;

  const tables = Object.entries(byProperty).map(([name, leases]) => {
    const rows = leases.map(l => {
      const ended = l.end_date && l.end_date < today;
      return `<tr class="${ended ? 'ended' : ''}"><td>${esc(l.unit_name)}</td>
        <td><b>${esc(l.tenant.name)}</b>${when(ended, ' <span class="muted small">(ausgezogen)</span>')}</td>
        <td class="tnum">${dmy(l.start_date)}${l.end_date ? ' – ' + dmy(l.end_date) : ''}</td>
        <td class="r tnum">${E(l.rent_cold)}</td><td class="r tnum">${E(l.nk_prepay)}</td>
        <td>${RENT_TYPE_SHORT[l.rent_type]}</td>
        <td class="r"><button class="btn small" data-act="open-lease" data-id="${l.id}">Konto</button>
          <button class="btn ghost small" data-act="hv-edit-lease" data-id="${l.id}">Bearbeiten</button></td></tr>`;
    });
    return `<section class="ledger"><header><h2>${esc(name)}</h2></header><div class="tbl-scroll"><table>
      <thead><tr><th>Einheit</th><th>Mieter</th><th>Seit</th><th class="r">Kalt</th><th class="r">NK</th><th>Art</th><th></th></tr></thead>
      <tbody>${rows.join('')}</tbody></table></div></section>`;
  });

  render(`${
    HV.edit
      ? leaseForm(HV.edit)
      : '<div class="row"><span class="spacer"></span><button class="btn primary" data-act="hv-new-lease">Neuer Mietvertrag</button></div>'
  }
    ${detail ? leaseDetail(detail) : ''}
    ${tables.join('') || '<div class="card empty">Noch keine Mietverträge.</div>'}`);
  if (HV.edit) scrollToTop($('#leaseForm'));
  else if (HV.lease) scrollToTop($('#leaseDetail'));
}

/** Reloads the leases after a change and shows the list. */
async function refresh() {
  await loadMeta(true);
  renderLeases();
}

registerView('hv-mieter', viewLeases);

onClick('open-lease', el => {
  HV.lease = +el.dataset.id;
  HV.edit = null;
  if (location.hash !== '#hv-mieter') location.hash = '#hv-mieter';
  else renderLeases();
});
onClick('hv-new-lease', () => {
  HV.edit = {};
  HV.lease = null;
  renderLeases();
});
onClick('hv-edit-lease', el => {
  HV.edit = HV.meta.leases.find(l => l.id === +el.dataset.id);
  renderLeases();
});
onClick('hv-cancel-lease', () => {
  HV.edit = null;
  renderLeases();
});
onClick('hv-close-lease', () => {
  HV.lease = null;
  renderLeases();
});
onClick('hv-del-lease', async el => {
  if (!confirmed(el, 'Wirklich löschen?')) return;
  await api('DELETE', '/api/hv/leases/' + el.dataset.id);
  toast('Mietvertrag gelöscht', TIMING.toastShort);
  HV.edit = null;
  HV.lease = null;
  refresh();
});
onClick('hv-del-step', async el => {
  await api('DELETE', '/api/hv/steps/' + el.dataset.id);
  refresh();
});
onSubmit('#leaseForm', async form => {
  const body = {
    unit_id: +$('#lUnit').value,
    tenant: {
      id: HV.edit?.tenant?.id || 0,
      name: $('#lName').value.trim(),
      iban: $('#lIban').value.trim(),
      email: $('#lMail').value.trim(),
      phone: $('#lPhone').value.trim(),
    },
    start_date: $('#lStart').value,
    end_date: $('#lEnd').value,
    rent_cold: cents($('#lCold').value),
    nk_prepay: cents($('#lNK').value),
    due_day: +$('#lDue').value || DEFAULT_DUE_DAY,
    persons: +$('#lPers').value || 0,
    rent_type: $('#lType').value,
    last_increase: $('#lInc').value,
    deposit: cents($('#lDep').value),
    deposit_paid: $('#lDepPaid').checked,
    track_from: $('#lTrack').value,
    match_text: $('#lMatch').value.trim(),
    notes: $('#lNotes').value,
  };
  const id = form.dataset.id;
  const r = id ? await api('PUT', '/api/hv/leases/' + id, body) : await api('POST', '/api/hv/leases', body);
  toast('Mietvertrag gespeichert, Zahlungen neu zugeordnet', TIMING.toastMedium);
  HV.edit = null;
  HV.lease = r.id;
  refresh();
});
onSubmit('#stepForm', async form => {
  await api('POST', `/api/hv/leases/${form.dataset.lease}/steps`, {
    valid_from: $('#sFrom').value,
    rent_cold: cents($('#sCold').value),
    nk_prepay: cents($('#sNK').value),
  });
  toast('Mietänderung gespeichert', TIMING.toastShort);
  refresh();
});
