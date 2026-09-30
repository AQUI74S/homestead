// Monthly budget: available amount, forecast, upcoming payments, budget vs. actual.
import { BUDGET, DAY_MS, GROUPS, HERO_COLORS, KINDS, OUT_GROUPS, TIMING, UNCATEGORIZED } from '../core/constants.js';
import { E, E0, dm, dmy, esc, monthLabel, monthShort, nf2, parseDE, plural } from '../core/format.js';
import { confirmed, render, toast, when } from '../core/dom.js';
import { api, get } from '../core/api.js';
import { S, accountName, catById, emptyTxFilter } from '../core/state.js';
import { onChange, onClick } from '../core/actions.js';
import { registerView, route, showPeriod } from '../core/shell.js';

/** Loads the overview of the selected month (reused by the couple split). */
export async function loadOverview() {
  const ov = (S.overview = await get('/api/overview?month=' + S.month));
  ov.accounts ||= [];
  ov.upcoming ||= [];
  ov.trend ||= [];
  ov.report.lines ||= [];
  return ov;
}

const categoryName = item =>
  item.category_id && catById(item.category_id) ? catById(item.category_id).name : KINDS[item.kind] || item.kind;

async function viewBudget() {
  const ov = await loadOverview();
  showPeriod(ov.period);
  S.me.name_a = ov.name_a;
  S.me.name_b = ov.name_b;

  const lines = ov.report.lines;
  const sum = (g, f) => lines.filter(l => l.group === g).reduce((a, l) => a + l[f], 0);
  const t = {};
  for (const g of Object.keys(GROUPS)) t[g] = { budget: sum(g, 'budget'), ist: sum(g, 'ist') };
  const outIst = OUT_GROUPS.reduce((a, g) => a + t[g].ist, 0);
  const outBudget = OUT_GROUPS.reduce((a, g) => a + t[g].budget, 0);
  const noBudgets = lines.every(l => l.budget === 0);
  const avgN = ov.report.avg_periods || 0;
  // Budget doesn't match the average: no budget despite regular amounts, or a large deviation
  const offBudget = l =>
    avgN > 0 &&
    l.group !== 'transfer' &&
    (l.budget === 0
      ? l.avg >= BUDGET.minAverageForBudget
      : Math.abs(l.budget - l.avg) > Math.max(l.avg * BUDGET.offShare, BUDGET.offMinCents));
  const offLines = noBudgets ? [] : lines.filter(offBudget);

  render(
    banners(ov, { noBudgets, avgN, offLines }).join('') +
      `<section class="grid3">
        ${heroHTML(ov, t, outIst, outBudget)}
        ${forecastHTML(ov)}
      </section>
      <section class="grid3">
        <div class="span2">${upcomingHTML(ov)}</div>
        <div class="card"><h3>Budget vs. Ist</h3><div class="gbars">${groupBars(t)}</div>
          <div class="divider"></div><h3>Größte Posten</h3>
          <div class="tops">${topCategories(lines, outIst) || '<span class="muted">Keine Ausgaben</span>'}</div></div>
      </section>
      <div class="card"><div class="card-head"><h3>Letzte 12 Monate</h3><div class="legend2">
        <span><i style="background:var(--c-income)"></i>Einnahmen</span>
        <span><i style="background:var(--accent)"></i>Ausgaben gesamt</span></div></div>${trendBars(ov.trend)}</div>
      <section class="ledgers">${['income', ...OUT_GROUPS].map(g => ledgerHTML(g, lines, t, offLines, avgN)).join('')}</section>
      <p class="note">Budgets gelten für jeden Budgetmonat. Umbuchungen zwischen euren eigenen Konten zählen weder als
        Einnahme noch als Ausgabe.${when(
          avgN > 0,
          ` Ø = Monatsdurchschnitt der letzten ${avgN} Budgetmonate, Jahres- und Quartalszahlungen anteilig.
          <button class="linkbtn" data-act="rebuild-budgets">Alle Budgets aus Ist neu berechnen</button>`,
        )}</p>`,
  );
}

function banners(ov, { noBudgets, avgN, offLines }) {
  const out = [];
  const banner = (cls, text, action = '') => `<div class="banner ${cls}"><p>${text}</p>${action}</div>`;
  if (!ov.accounts.length)
    out.push(
      banner(
        '',
        '<b>Noch kein Konto verbunden.</b> Verbinde deine Bank, dann werden Umsätze automatisch ' +
          'abgerufen und zugeordnet.',
        '<a class="btn primary" href="#konten">Bank verbinden</a>',
      ),
    );

  const expired = [...new Set(ov.accounts.filter(a => a.connection_status === 'expired').map(a => esc(a.bank)))];
  if (expired.length)
    out.push(
      banner(
        'bad',
        `<b>Bankfreigabe abgelaufen</b> für ${expired.join(', ')}. Ohne neue Freigabe kommen ` + 'keine neuen Umsätze.',
        '<a class="btn" href="#konten">Neu verbinden</a>',
      ),
    );

  const soon = ov.accounts.filter(
    a =>
      a.connection_status === 'active' &&
      a.valid_until &&
      new Date(a.valid_until) - Date.now() < BUDGET.consentWarnDays * DAY_MS,
  );
  if (soon.length)
    out.push(
      banner(
        'warn',
        `Die Bankfreigabe für ${esc(soon[0].bank)} läuft am ${dmy(soon[0].valid_until.slice(0, 10))} ab.`,
        '<a class="btn" href="#konten">Verlängern</a>',
      ),
    );

  if (ov.accounts.length && noBudgets)
    out.push(
      banner(
        '',
        `<b>Noch keine Budgets.</b> Ich kann jedes Budget auf den Durchschnitt der letzten ` +
          `${avgN > 1 ? avgN + ' Monate' : 'Monate'} setzen, danach passt du einzelne Werte an.`,
        '<button class="btn primary" data-act="suggest">Budgets vorschlagen</button>',
      ),
    );

  if (offLines.length) {
    const list = offLines
      .slice(0, 4)
      .map(l => `${esc(l.name)} ${E0(l.budget)} → Ø ${E0(l.avg)}`)
      .join(', ');
    out.push(
      banner(
        'warn',
        `<b>${offLines.length === 1 ? '1 Budget passt' : offLines.length + ' Budgets passen'} nicht zu den tatsächlichen ` +
          `Beträgen</b> (Durchschnitt der letzten ${avgN} Monate): ${list}${offLines.length > 4 ? ' …' : ''}. ` +
          'Einzeln in der Tabelle unten übernehmen oder alle neu berechnen.',
        '<button class="btn" data-act="rebuild-budgets">Aus Ist neu berechnen</button>',
      ),
    );
  }

  const n = ov.report.uncategorized;
  if (n > 0)
    out.push(
      banner(
        'warn',
        `${plural(n, 'Umsatz', 'Umsätze')} in ${monthLabel(S.month)} konnte ich keiner Kategorie zuordnen.`,
        '<a class="btn" href="#umsaetze" data-act="show-uncat">Ansehen</a>',
      ),
    );

  if (ov.period.mode === 'calendar' && ov.accounts.length)
    out.push(
      banner(
        '',
        'Noch kein regelmäßiges Gehalt erkannt, deshalb gilt vorerst der Kalendermonat. Sobald zwei bis ' +
          'drei Gehaltseingänge da sind, stellt die App auf „Gehalt bis Gehalt“ um. Unter <a href="#konten">Konten</a> ' +
          'kannst du das Gehalt auch selbst festlegen.',
      ),
    );
  return out;
}

/** Hero card: available amount and how the income was used. */
function heroHTML(ov, t, outIst, outBudget) {
  const avail = t.income.ist - outIst;
  const plan = t.income.budget - outBudget;
  const quote = t.income.ist > 0 ? Math.round((t.savings.ist / t.income.ist) * 100) : 0;
  const pct = t.income.ist > 0 ? Math.round((avail / t.income.ist) * 100) : 0;
  const base = Math.max(t.income.ist, outIst, 1);
  const stack = OUT_GROUPS.map(
    g =>
      `<i style="width:${(Math.max(0, t[g].ist) / base) * 100}%;background:var(${HERO_COLORS[g]})" ` +
      `title="${GROUPS[g].label} ${E(t[g].ist)}"></i>`,
  ).join('');
  const legend = OUT_GROUPS.map(
    g =>
      `<div><span><i style="background:var(${HERO_COLORS[g]})"></i>${GROUPS[g].label}</span><b>${E0(t[g].ist)}</b></div>`,
  ).join('');
  const daysLeft = Math.max(0, Math.ceil((new Date(ov.period.end + 'T23:59:59') - Date.now()) / DAY_MS));
  const chip = ov.forecast?.past
    ? '<span class="chip">Monat abgeschlossen</span>'
    : `<span class="chip${ov.forecast?.projected < 0 ? ' bad' : ''}">noch ${plural(daysLeft, 'Tag', 'Tage')}</span>`;
  return `<div class="hero span2">
      <div class="hero-top"><div><div class="lbl">Verfügbar in diesem Monat</div>
        <div class="big ${avail < 0 ? 'neg' : ''}">${E0(avail)}</div>
        <div class="sub">von ${E0(t.income.ist)} Einnahmen${when(t.income.ist > 0, ` · ${pct} % übrig`)} · geplant frei
          ${E0(plan)} · Sparquote ${quote} %</div></div>${chip}</div>
      <div class="stack" role="img" aria-label="Verwendung der Einnahmen">${stack}</div>
      <div class="hero-legend">${legend}</div>
    </div>`;
}

/** Group totals as bars: actual with a marker for the budget. */
function groupBars(t) {
  return OUT_GROUPS.map(g => {
    const b = t[g].budget;
    const i = Math.max(0, t[g].ist);
    const over = b > 0 && i > b;
    const scale = Math.max(b, i, 1);
    const title = b > 0 ? Math.round((i / b) * 100) + ' % des Budgets' : 'kein Budget';
    return `<div class="gbar"><div class="h"><span>${GROUPS[g].label}</span>
        <span class="${over ? 'neg' : ''}">${E0(i)} / ${E0(b)}</span></div>
      <div class="track" title="${title}"><i style="width:${(i / scale) * 100}%;background:var(${over ? '--bad' : GROUPS[g].color})"></i>${when(
        over,
        `<span class="mark" style="left:${(b / scale) * 100}%"></span>`,
      )}</div></div>`;
  }).join('');
}

function topCategories(lines, outIst) {
  const exp = lines.filter(l => OUT_GROUPS.includes(l.group) && l.ist > 0).sort((a, b) => b.ist - a.ist);
  const max = exp[0]?.ist || 1;
  return exp
    .slice(0, 5)
    .map(
      l =>
        `<div><span>${esc(l.name)}</span><span class="track"><i style="width:${(l.ist / max) * 100}%;background:var(--ink)">` +
        `</i></span><em>${outIst > 0 ? Math.round((l.ist / outIst) * 100) : 0} %</em></div>`,
    )
    .join('');
}

function trendBars(trend) {
  if (!trend.length) return '<p class="muted">Noch keine Daten.</p>';
  const outOf = p => p.bills + p.expenses + p.debts + p.savings;
  const max = Math.max(1, ...trend.map(p => Math.max(p.income, outOf(p))));
  const h = v => Math.max(2, Math.round((Math.max(0, v) / max) * BUDGET.trendBarHeight));
  const bars = trend.map(
    p =>
      `<div class="m${p.month === S.month ? ' cur' : ''}" title="${monthLabel(p.month)}: Einnahmen ${E(p.income)}, ` +
      `Ausgaben ${E(outOf(p))}">
    <div class="pair"><i style="height:${h(p.income)}px;background:var(--c-income);opacity:.45"></i>` +
      `<i style="height:${h(outOf(p))}px;background:var(--accent)"></i></div>
    <span class="lbl">${monthShort(p.month)}</span></div>`,
  );
  return `<div class="tbars">${bars.join('')}</div>`;
}

/** Table of one group: budget input, actual and progress per category. */
function ledgerHTML(g, lines, t, offLines, avgN) {
  const rows = lines
    .filter(l => l.group === g)
    .map(l => {
      const over = g !== 'income' && l.budget > 0 && l.ist > l.budget;
      const pct = l.budget > 0 ? Math.min(100, (Math.max(0, l.ist) / l.budget) * 100) : 0;
      const off = offLines.includes(l);
      const avgVal = Math.ceil(l.avg / BUDGET.roundCents) * BUDGET.roundCents;
      const avgHint = !(avgN > 0 && l.avg > 0)
        ? ''
        : off
          ? `<button class="avg-hint off" data-act="take-avg" data-cat="${l.id}" data-amount="${avgVal}" ` +
            `title="Budget auf den Durchschnitt setzen">Ø ${E0(l.avg)} · übernehmen</button>`
          : `<span class="avg-hint">Ø ${E0(l.avg)}</span>`;
      const ist = l.count
        ? `<button class="ist-link" data-act="show-category" data-cat="${l.id}">${E(l.ist)}</button>`
        : `<span class="muted">${E(0)}</span>`;
      const bar = when(
        l.budget > 0,
        `<div class="bar thin"><i style="width:${pct}%;background:var(${over ? '--bad' : GROUPS[g].color})"></i></div>`,
      );
      return `<tr class="${off ? 'off-budget' : ''}"><td><span class="cat-name">${esc(l.name)}</span>${avgHint}</td>
        <td class="r"><input class="num" inputmode="decimal" data-budget="${l.id}" value="${l.budget ? nf2.format(l.budget / 100) : ''}"
          placeholder="0,00" aria-label="Budget ${esc(l.name)}"></td>
        <td class="r ${over ? 'over' : ''}">${ist}</td>
        <td class="prog">${bar}</td></tr>`;
    })
    .join('');
  return `<section class="ledger" style="--gc:var(${GROUPS[g].color})"><header><h2>${GROUPS[g].label}</h2>
      <span class="sum"><b>${E(t[g].ist)}</b> / ${E(t[g].budget)}</span></header>
    <div class="tbl-scroll"><table><thead><tr><th>Kategorie</th><th class="r">Budget</th><th class="r">Ist</th><th></th></tr>
      </thead><tbody>${rows}</tbody></table></div></section>`;
}

/** Forecast until the end of the budget month. */
function forecastHTML(ov) {
  const f = ov.forecast;
  const k = f.open_by_kind || {};
  const nowFree = f.income_ist - f.out_ist;
  const line = (label, v, cls = '') => `<div><span>${label}</span><b class="tnum ${cls}">${v}</b></div>`;
  const open = f.items.filter(i => i.status !== 'bezahlt');
  const paid = f.items.filter(i => i.status === 'bezahlt');
  const item = i =>
    `<li><span class="date-chip">${dm(i.date)}</span><span>${esc(i.label)} <span class="muted small">· ` +
    `${esc(categoryName(i))}</span></span><b class="tnum ${i.amount > 0 ? 'pos' : ''}">${E(i.amount)}</b></li>`;

  if (f.past) {
    const more = paid.length - BUDGET.paidLimit;
    return `<div class="card"><h3>Abgeschlossen</h3><p class="muted" style="margin-top:0">Dieser Budgetmonat ist vorbei.
        Übrig geblieben: <b class="${nowFree < 0 ? 'neg' : ''}">${E(nowFree)}</b>.</p>
      ${when(
        paid.length,
        `<ul class="list">${paid.slice(0, BUDGET.paidLimit).map(item).join('')}</ul>${when(
          more > 0,
          `<p class="note">und ${more} weitere wiederkehrende Zahlungen</p>`,
        )}`,
      )}</div>`;
  }
  const fixed = (k.fixkosten || 0) + (k.kredit || 0);
  const other = (k.sparen || 0) + (k.sonstiges || 0) + (k.einkommen || 0);
  const end = dm(ov.period.end);
  return `<div class="card"><h3>Prognose bis ${end}</h3>
    <div class="fc-rows">
      ${line('Jetzt frei', E(nowFree), nowFree < 0 ? 'neg' : '')}
      ${when(f.open_in, line('+ erwartete Einnahmen', E(f.open_in), 'pos'))}
      ${when(fixed, line('− Fixkosten &amp; Kredite', E(-fixed)))}
      ${when(k.abo, line('− Abos', E(-k.abo)))}
      ${when(other, line('− Sparen &amp; Sonstiges', E(-other)))}
      ${when(
        f.budget_rest,
        line(
          `− variable Ausgaben <span class="muted small">(noch ${plural(f.budget_days, 'Tag', 'Tage')})</span>`,
          E(-f.budget_rest),
        ),
      )}
    </div>
    <div class="fc-total"><span>Voraussichtlich frei</span><b class="tnum ${f.projected < 0 ? 'neg' : ''}">${E(f.projected)}</b></div>
    <p class="note">${
      open.length
        ? `${open.length} wiederkehrende Zahlungen bis ${end} noch offen.`
        : `Bis ${end} ist nichts Wiederkehrendes mehr offen.`
    } Abos ${E(ov.abo_monthly)}, Fixkosten und Kredite
      ${E(ov.fixed_monthly)} pro Monat.</p></div>`;
}

/** Recurring payments of the next 30 days, and per account the balance afterwards. */
function upcomingHTML(ov) {
  const items = [...(ov.upcoming || [])].sort((a, b) => a.date.localeCompare(b.date));
  const accs = new Map(ov.accounts.map(a => [a.id, a]));
  const sum = (arr, f) => arr.filter(f).reduce((a, i) => a + i.amount, 0);
  const totalOut = -sum(items, i => i.amount < 0);
  const totalIn = sum(items, i => i.amount > 0);
  const accName = id => (accs.has(id) ? accountName(accs.get(id)) : 'Ohne Konto');
  const limit = S.upAll ? items.length : BUDGET.upcomingLimit;
  const rows = items
    .slice(0, limit)
    .map(
      i => `<div class="up-row"><span class="d">${dm(i.date)}</span>
    <span class="t"><b>${esc(i.label)}</b> <span>· ${esc(categoryName(i))}${when(i.manual, ' · manuell')}${when(
      i.status === 'ueberfaellig',
      ' · <span class="neg">überfällig</span>',
    )}</span></span>
    <span class="a">${esc(accName(i.account_id))}</span>
    <span class="v tnum ${i.amount > 0 ? 'pos' : ''}">${E(i.amount)}</span></div>`,
    )
    .join('');

  const byAcc = new Map();
  for (const i of items) {
    const k = i.account_id || 0;
    if (!byAcc.has(k)) byAcc.set(k, []);
    byAcc.get(k).push(i);
  }
  const accCards = [...byAcc.entries()]
    .map(([id, list]) => {
      const a = accs.get(id);
      const out = -sum(list, i => i.amount < 0);
      const inn = sum(list, i => i.amount > 0);
      const after = a && a.balance != null ? a.balance + inn - out : null;
      return `<div class="up-acc2"><b>${esc(accName(id))}</b>
      <div class="row2"><span>Abbuchungen</span><span class="tnum">${E(-out)}</span></div>
      ${when(inn, `<div class="row2"><span>Eingänge</span><span class="tnum pos">${E(inn)}</span></div>`)}
      ${when(after != null, `<div class="row2"><span>Kontostand danach</span><b class="tnum ${after < 0 ? 'neg' : ''}">${E(after)}</b></div>`)}</div>`;
    })
    .join('');

  const toggle = when(
    items.length > BUDGET.upcomingLimit,
    `<button class="linkbtn" data-act="up-all" style="margin-top:8px">${S.upAll ? 'Weniger anzeigen' : `Alle ${items.length} Zahlungen anzeigen`}</button>`,
  );
  const total = when(
    items.length,
    `<span class="right"><span class="muted">Summe </span><b class="tnum">${E(-totalOut)}</b>${when(
      totalIn,
      ` · <span class="pos tnum">${E(totalIn)}</span>`,
    )}</span>`,
  );
  return `<div class="card"><div class="card-head"><h3>Demnächst abgebucht</h3><span class="meta">nächste 30 Tage · alle Konten</span>
      ${total}</div>
    ${
      items.length
        ? `<div class="up-list">${rows}</div>${toggle}<div class="up-accs">${accCards}</div>`
        : '<p class="muted">In den nächsten 30 Tagen ist nichts Wiederkehrendes fällig.</p>'
    }
    <p class="note">Aus erkannten Abos, Fixkosten, Krediten und von Hand angelegten Verträgen. <a href="#abos">Alle ansehen</a></p></div>`;
}

registerView('monat', viewBudget);

onChange('[data-budget]', async el => {
  const v = parseDE(el.value);
  if (v === null) return toast('Betrag nicht lesbar, z. B. 250 oder 49,90');
  await api('PUT', `/api/categories/${el.dataset.budget}/budget`, { amount: v });
  el.value = v ? nf2.format(v / 100) : '';
  toast('Budget gespeichert', TIMING.toastShort);
  viewBudget();
});
onClick('up-all', () => {
  S.upAll = !S.upAll;
  viewBudget();
});
onClick('suggest', async () => {
  const r = await api('POST', '/api/budgets/suggest');
  toast(`${r.updated} Budgets gesetzt`, TIMING.toastMedium);
  S.cats = [];
  route();
});
onClick('rebuild-budgets', async el => {
  if (!confirmed(el, 'Alle Budgets überschreiben?')) return;
  const r = await api('POST', '/api/budgets/suggest', { overwrite: true });
  toast(`${r.updated} Budgets neu berechnet (Durchschnitt ${r.months} Monate)`, TIMING.toastMedium);
  S.cats = [];
  route();
});
onClick('take-avg', async el => {
  await api('PUT', `/api/categories/${el.dataset.cat}/budget`, { amount: +el.dataset.amount });
  toast('Budget übernommen', TIMING.toastShort);
  viewBudget();
});
onClick('show-category', el => {
  S.txFilter = { ...emptyTxFilter(), cat: String(el.dataset.cat) };
  location.hash = '#umsaetze';
});
onClick('show-uncat', () => {
  S.txFilter = { ...emptyTxFilter(), cat: UNCATEGORIZED };
});
