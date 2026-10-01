// Categories and own rules: create, rename, move and delete categories; rules
// "if <field> contains <text> then <category>".
import { GROUPS, RULE_FIELDS, TIMING } from '../core/constants.js';
import { esc, plural } from '../core/format.js';
import { confirmed, options, render, toast, when } from '../core/dom.js';
import { api, get } from '../core/api.js';
import { S } from '../core/state.js';
import { onChange, onClick, onSubmit } from '../core/actions.js';
import { registerView } from '../core/shell.js';
import { categoryOptions } from './transactions.js';

/** Groups that can hold own categories (transfers are recognized, never chosen). */
const CUSTOM_GROUPS = Object.keys(GROUPS).filter(g => g !== 'transfer');
const groupOptions = selected =>
  options(
    CUSTOM_GROUPS.map(g => [g, GROUPS[g].label]),
    selected,
  );

/** Fields a new rule can look at, most specific first. */
const NEW_RULE_FIELDS = ['remittance', 'merchant', 'iban'];

async function viewCategories() {
  const rules = (await get('/api/rules')) || [];
  render(`${rulesHTML(rules)}${categoriesHTML()}`);
}

/** Reloads the categories everywhere and shows the page again. */
async function reload() {
  S.cats = await get('/api/categories');
  await viewCategories();
}

function rulesHTML(rules) {
  const rows = rules.map(
    r => `<tr><td>${RULE_FIELDS[r.field] || esc(r.field)}</td><td><b>${esc(r.pattern)}</b></td>
      <td><select class="inline" data-rulecat="${r.id}" data-field="${r.field}" data-pattern="${esc(r.pattern)}"
        aria-label="Kategorie der Regel">${categoryOptions(r.category_id, false)}</select></td>
      <td class="r tnum">${r.hits}</td>
      <td class="r"><button class="btn ghost small danger" data-act="del-rule" data-id="${r.id}">Löschen</button></td></tr>`,
  );
  return `<section class="ledger"><header><h2>Eigene Regeln</h2><span class="sum">gehen vor der automatischen
      Erkennung · Regeln auf den Verwendungszweck vor Regeln auf den Empfänger</span></header>
    <form id="ruleForm" class="rule-form">
      <div class="field"><label for="ruleField">Wenn</label>
        <select id="ruleField" name="field">${options(NEW_RULE_FIELDS.map(f => [f, RULE_FIELDS[f]]))}</select></div>
      <div class="field"><label for="rulePattern">enthält</label>
        <input id="rulePattern" name="pattern" required minlength="3" autocomplete="off" placeholder="z. B. Kindergarten"></div>
      <div class="field"><label for="ruleCat">dann Kategorie</label>
        <select id="ruleCat" name="category_id" required><option value="">Kategorie wählen …</option>${categoryOptions('', false)}</select></div>
      <button class="btn primary">Regel anlegen</button>
    </form>
    ${
      rules.length
        ? `<div class="tbl-scroll"><table><thead><tr><th>Wenn</th><th>enthält</th><th>dann</th><th class="r">Umsätze</th>
          <th></th></tr></thead><tbody>${rows.join('')}</tbody></table></div>`
        : '<div class="empty">Noch keine eigenen Regeln. Sie entstehen auch, wenn du bei einem Umsatz „Alle von … so zuordnen“ wählst.</div>'
    }</section>`;
}

function categoriesHTML() {
  const groups = CUSTOM_GROUPS.map(g => {
    const rows = S.cats
      .filter(c => c.group === g)
      .map(
        c => `<div class="cat-row"><input class="inline" data-catname="${c.id}" value="${esc(c.name)}"
          aria-label="Name der Kategorie" maxlength="60">${
            c.custom
              ? `<select class="inline" data-catgroup="${c.id}" aria-label="Bereich">${groupOptions(c.group)}</select>
                <button class="btn ghost small danger" data-act="del-cat" data-id="${c.id}">Löschen</button>`
              : ''
          }</div>`,
      )
      .join('');
    return `<div class="cat-group" style="--gc:var(${GROUPS[g].color})"><h3>${GROUPS[g].label}</h3>${rows}</div>`;
  }).join('');
  return `<section class="ledger"><header><h2>Kategorien</h2><span class="sum">Zum Umbenennen auf den Namen klicken · eigene kannst du auch verschieben und löschen</span></header>
    <form id="catForm" class="cat-form">
      <div class="field"><label for="catName">Neue Kategorie</label>
        <input id="catName" name="name" required maxlength="60" autocomplete="off" placeholder="z. B. Haustiere"></div>
      <div class="field"><label for="catGroup">Bereich</label>
        <select id="catGroup" name="group">${groupOptions('expenses')}</select></div>
      <button class="btn primary">Kategorie anlegen</button>
    </form>
    <div class="cat-grid">${groups}</div>
    <p class="note" style="padding:0 18px 16px">Löschst du eine eigene Kategorie, werden ihre Umsätze wieder automatisch
      zugeordnet, auch die von Hand gesetzten. Ihre Regeln und Budgets fallen weg.</p></section>`;
}

registerView('kategorien', viewCategories);

onSubmit('#ruleForm', async form => {
  const f = new FormData(form);
  const { hits } = await api('POST', '/api/rules', {
    field: f.get('field'),
    pattern: f.get('pattern'),
    category_id: +f.get('category_id'),
  });
  toast(
    `Regel gespeichert – ${hits ? 'trifft ' + plural(hits, 'Umsatz', 'Umsätze') : 'trifft noch keinen Umsatz'}`,
    TIMING.toastMedium,
  );
  await viewCategories();
});
onChange('[data-rulecat]', async el => {
  const { field, pattern } = el.dataset;
  await api('POST', '/api/rules', { field, pattern, category_id: +el.value });
  toast('Regel geändert', TIMING.toastShort);
  await viewCategories();
});
onClick('del-rule', async el => {
  await api('DELETE', `/api/rules/${el.dataset.id}`);
  toast('Regel gelöscht', TIMING.toastShort);
  await viewCategories();
});

onSubmit('#catForm', async form => {
  const f = new FormData(form);
  const name = f.get('name').trim();
  await api('POST', '/api/categories', { name, group: f.get('group') });
  toast(`Kategorie „${esc(name)}“ angelegt`, TIMING.toastShort);
  await reload();
});
onChange('[data-catname]', async el => {
  const name = el.value.trim();
  const cat = S.cats.find(c => c.id === +el.dataset.catname);
  if (!name || name === cat?.name) {
    el.value = cat?.name || '';
    return;
  }
  try {
    await api('PATCH', `/api/categories/${el.dataset.catname}`, { name });
  } catch (err) {
    el.value = cat?.name || '';
    throw err;
  }
  toast('Umbenannt', TIMING.toastShort);
  await reload();
});
onChange('[data-catgroup]', async el => {
  await api('PATCH', `/api/categories/${el.dataset.catgroup}`, { group: el.value });
  toast(`Verschoben nach ${GROUPS[el.value].label}`, TIMING.toastShort);
  await reload();
});
onClick('del-cat', async el => {
  if (!confirmed(el, 'Wirklich löschen?')) return;
  const { released } = await api('DELETE', `/api/categories/${el.dataset.id}`);
  toast(
    `Kategorie gelöscht${when(released, () => `, ${plural(released, 'Umsatz', 'Umsätze')} neu zugeordnet`)}`,
    TIMING.toastMedium,
  );
  await reload();
});
