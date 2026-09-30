// Property management overview: rent due and paid this month, deadlines, properties.
import { E, E0, dm, esc, initials, m2, monthLabel, pct2 } from '../../core/format.js';
import { render, when } from '../../core/dom.js';
import { get } from '../../core/api.js';
import { HV } from '../../core/state.js';
import { onClick } from '../../core/actions.js';
import { registerView } from '../../core/shell.js';
import { costTypeName, loadMeta, loadReport, pill } from './common.js';

/** Deadlines shown on the overview. */
const DEADLINE_LIMIT = 7;
/** Largest cost types shown per property. */
const COST_LIMIT = 4;

const TONE = { bezahlt: 'ok', teilweise: 'warn', offen: 'bad' };

async function viewOverview() {
  await loadMeta(true);
  const ov = await get('/api/hv/overview?month=' + HV.month);
  ov.deadlines ||= [];
  ov.leases ||= [];
  ov.properties ||= [];
  ov.accounts ||= [];
  const rep = await loadReport(HV.year).catch(() => ({ properties: [] }));
  const reportOf = Object.fromEntries((rep.properties || []).map(p => [p.property_id, p]));
  const t = ov.totals;

  const steps = [];
  if (!ov.accounts.length)
    steps.push('Unter <a href="#konten">Konten</a> beim Mietkonto „Gehört zu: Hausverwaltung“ wählen.');
  if (!ov.properties.length)
    steps.push('Unter <a href="#hv-objekte">Objekte</a> Häuser bzw. Wohnungen mit ihren Einheiten anlegen.');
  if (ov.properties.length && !ov.leases.length)
    steps.push(
      'Unter <a href="#hv-mieter">Mieter &amp; Verträge</a> die Mietverträge eintragen. Die App ordnet Zahlungen dann automatisch zu.',
    );

  const rows = ov.leases
    .map(l => {
      const r = l.row;
      const balance = l.balance > 0 ? 'Rückstand' : l.balance < 0 ? 'Guthaben' : 'ausgeglichen';
      return `<tr data-act="open-lease" data-id="${l.id}" class="clickable">
      <td><div class="who"><span class="avatar ${TONE[r?.status] || ''}" aria-hidden="true">${esc(initials(l.tenant.name))}</span>
        <div><b>${esc(l.tenant.name)}</b><br><span class="muted small">${esc(l.property_name)} · ${esc(l.unit_name)}</span></div></div></td>
      <td class="r tnum">${r ? E(r.soll) : '–'}</td><td class="r tnum">${r ? E(r.paid) : '–'}</td>
      <td>${r ? pill(r.status) : '<span class="muted small">kein Soll</span>'}${when(
        r && r.status !== 'bezahlt',
        () => ` <span class="muted small">fällig ${dm(r.due_date)}</span>`,
      )}</td>
      <td class="r tnum ${l.balance > 0 ? 'neg' : l.balance < 0 ? 'pos' : ''}"><b>${E(-l.balance)}</b><br>
        <span class="muted small">${balance}</span></td></tr>`;
    })
    .join('');

  const deadlines = ov.deadlines
    .slice(0, DEADLINE_LIMIT)
    .map(
      d =>
        `<li><span class="date-chip ${d.urgent ? 'urgent' : ''}">${dm(d.date)}</span><span><b>${esc(d.title)}</b>${when(
          d.detail,
          () => `<br><span class="muted small">${esc(d.detail)}</span>`,
        )}</span></li>`,
    )
    .join('');

  const props = ov.properties.map(p => propertyCard(p, reportOf[p.id] || { costs_by_type: {} })).join('');
  const paidPct = t.soll ? Math.min(100, (t.paid / t.soll) * 100) : 0;

  render(`${when(
    steps.length,
    () => `<div class="banner"><p><b>So richtest du die Hausverwaltung ein:</b></p>
      <ol class="steps">${steps.map(s => `<li>${s}</li>`).join('')}</ol></div>`,
  )}
    ${when(
      ov.unassigned && ov.properties.length,
      () => `<div class="banner warn"><p>${ov.unassigned} Umsätze auf dem Mietkonto
      sind noch keinem Objekt bzw. keiner Kostenart zugeordnet.</p>
      <a class="btn" href="#hv-umsaetze" data-act="hv-open-tx">Zuordnen</a></div>`,
    )}
    <section class="kpi4">
      <div class="tile"><span class="l">Soll ${monthLabel(HV.month)}</span><span class="n tnum">${E0(t.soll)}</span>
        <span class="s">Kaltmiete + NK aller Verträge</span></div>
      <div class="tile"><span class="l">Eingegangen</span><span class="n tnum pos">${E0(t.paid)}</span>
        <span class="track"><i style="width:${paidPct}%;background:var(--good)"></i></span></div>
      <div class="tile"><span class="l">Offen in diesem Monat</span><span class="n tnum ${t.open ? 'neg' : ''}">${E0(t.open)}</span>
        <span class="s">inkl. noch nicht fälliger Mieten</span></div>
      <div class="tile dark"><span class="l">Rückstände gesamt</span><span class="n tnum ${t.arrears ? 'neg' : ''}">${E0(t.arrears)}</span>
        <span class="s">über alle Mieter und Monate</span></div>
    </section>
    <section class="grid3">
      <section class="ledger span2"><header><h2>Mieter</h2><span class="sum">Zeile anklicken für das Mieterkonto</span></header>
        <div class="tbl-scroll">${
          ov.leases.length
            ? `<table><thead><tr><th>Mieter · Einheit</th><th class="r">Soll</th>
          <th class="r">Ist</th><th>Status</th><th class="r">Saldo</th></tr></thead><tbody>${rows}</tbody></table>`
            : '<div class="empty">Noch keine Mietverträge.</div>'
        }</div></section>
      <div class="card dl"><div class="card-head" style="margin:0"><h3>Fristen &amp; Hinweise</h3><a class="right" href="#hv-fristen">Alle</a></div>
        ${deadlines ? `<ul>${deadlines}</ul>` : '<p class="muted">Keine Fristen in den nächsten zwei Monaten.</p>'}</div>
    </section>
    ${when(props, () => `<section class="prop-cards">${props}</section>`)}`);
}

function propertyCard(p, r) {
  const costs = Object.entries(r.costs_by_type || {}).sort((a, b) => b[1] - a[1]);
  const totalCost = costs.reduce((a, [, v]) => a + v, 0);
  const area = (p.units || []).map(u => `${esc(u.name)} ${m2(u.area)}`).join(' · ');
  const costBar =
    r.income > 0
      ? `<i style="width:${Math.min(100, (totalCost / r.income) * 100)}%;background:var(--accent)"></i>`
      : '';
  const split = costs
    .slice(0, COST_LIMIT)
    .map(([k, v]) => `<span>${esc(costTypeName(k))} ${E0(v)}</span>`)
    .join('');
  return `<div class="card prop-card">
      <div class="card-head" style="margin:0"><h3>${esc(p.name)}</h3><span class="meta">${area}</span>${when(
        r.gross_yield,
        () => `<span class="right badge">Rendite ${pct2(r.gross_yield)} %</span>`,
      )}</div>
      <div class="top3"><div><span>Einnahmen ${HV.year}</span><b class="tnum">${E0(r.income)}</b></div>
        <div><span>Kosten</span><b class="tnum">${E0(totalCost)}</b></div>
        <div><span>Überschuss</span><b class="tnum ${r.surplus < 0 ? 'neg' : 'pos'}">${E0(r.surplus)}</b></div></div>
      <div class="track" style="height:10px">${costBar}</div>
      <div class="cost-split">${split || '<span>Noch keine Kosten gebucht</span>'}</div></div>`;
}

registerView('hv', viewOverview);

onClick('hv-open-tx', () => {
  HV.txOpen = true;
});
