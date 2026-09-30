// Couple split: who pays how much of the shared household costs.
import { OUT_GROUPS, OWNER_A, OWNER_B, OWNER_JOINT } from '../core/constants.js';
import { E, esc, monthLabel } from '../core/format.js';
import { $, render, when } from '../core/dom.js';
import { api } from '../core/api.js';
import { S, nameOf } from '../core/state.js';
import { onChange } from '../core/actions.js';
import { registerView, showPeriod } from '../core/shell.js';
import { loadOverview } from './budget.js';

/** Flex value for an empty share, so the bar still renders. */
const MIN_FLEX = 1e-4;

async function viewCouple() {
  const ov = S.overview?.report.month === S.month ? S.overview : await loadOverview();
  showPeriod(ov.period);
  const by = ov.report.income_by_owner;
  const A = by[OWNER_A] || 0;
  const B = by[OWNER_B] || 0;
  const joint = by[OWNER_JOINT] || 0;
  const costs = ov.report.lines.filter(l => OUT_GROUPS.includes(l.group)).reduce((a, l) => a + l.ist, 0);
  // Joint income (e.g. child benefit) pays part of the costs first
  const shared = Math.max(0, costs - joint);
  const shareA = A + B > 0 ? A / (A + B) : 0.5;
  const shareB = 1 - shareA;
  const nA = esc(nameOf(OWNER_A));
  const nB = esc(nameOf(OWNER_B));
  const money = v => `<span class="${v < 0 ? 'neg' : ''}">${E(v)}</span>`;
  const payA = shared * shareA;
  const payB = shared * shareB;
  const half = shared / 2;
  const noOwners = ov.accounts.length > 0 && ov.accounts.every(a => a.owner === OWNER_JOINT);
  const row = (label, a, b) => `<tr><td>${label}</td><td class="r tnum">${a}</td><td class="r tnum">${b}</td></tr>`;

  render(`${when(
    noOwners,
    `<div class="banner warn"><p>Noch kein Konto einer Person zugeordnet. Lege unter „Konten“ fest,
      wem welches Girokonto gehört. Gehälter werden dann der richtigen Person zugerechnet.</p>
      <a class="btn" href="#konten">Konten zuordnen</a></div>`,
  )}
    <section class="card"><h3>Wer ist wer</h3><div class="names">
      <div class="field"><label for="nameA">Person A</label><input id="nameA" value="${nA}"></div>
      <div class="field"><label for="nameB">Person B</label><input id="nameB" value="${nB}"></div></div>
      <p class="note">Einnahmen zählen für die Person, der das Konto gehört. Einnahmen auf gemeinsamen Konten (z. B.
        Kindergeld) werden zuerst von den Kosten abgezogen.</p></section>
    <section class="paar-grid">
      <div class="card"><h3>Einkommensverhältnis ${monthLabel(S.month)}</h3>
        <div class="share-bar">
          <div style="flex:${shareA || MIN_FLEX};background:var(--c-income)">${nA} ${Math.round(shareA * 100)} %</div>
          <div style="flex:${shareB || MIN_FLEX};background:var(--c-expenses)">${nB} ${Math.round(shareB * 100)} %</div></div>
        <table><tbody>
          <tr><td>Einnahmen ${nA}</td><td class="r tnum">${E(A)}</td></tr>
          <tr><td>Einnahmen ${nB}</td><td class="r tnum">${E(B)}</td></tr>
          <tr><td>Gemeinsame Einnahmen</td><td class="r tnum">${E(joint)}</td></tr>
          <tr><td>Haushaltskosten gesamt</td><td class="r tnum">${E(costs)}</td></tr>
          <tr><td><b>Gemeinsam zu tragen</b></td><td class="r tnum"><b>${E(shared)}</b></td></tr></tbody></table></div>
      <div class="card"><h3>Vergleich der Aufteilung</h3><div class="tbl-scroll"><table>
        <thead><tr><th></th><th class="r">${nA}</th><th class="r">${nB}</th></tr></thead><tbody>
        <tr><td colspan="3"><b>Nach Einkommen</b></td></tr>
        ${row('Zahlt', E(payA), E(payB))}
        ${row('Bleibt für sich', money(A - payA), money(B - payB))}
        <tr><td colspan="3"><b>50/50</b></td></tr>
        ${row('Zahlt', E(half), E(half))}
        ${row('Bleibt für sich', money(A - half), money(B - half))}</tbody></table></div>
        <p class="note">Bei 50/50 bleibt ${A >= B ? nB : nA} ${E(Math.abs(A >= B ? payB - half : payA - half))} weniger als bei
          der Aufteilung nach Einkommen.</p></div>
    </section>`);
}

registerView('paar', viewCouple);

onChange('#nameA, #nameB', async () => {
  const names = { name_a: $('#nameA').value, name_b: $('#nameB').value };
  await api('PUT', '/api/settings', names);
  Object.assign(S.me, names);
  viewCouple();
});
