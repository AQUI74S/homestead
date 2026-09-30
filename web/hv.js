'use strict';
/* Property management: overview, tenants & leases, properties, service charges, rent account, deadlines. */

const HV = {
  month: new Date().toISOString().slice(0,7),
  year: new Date().getFullYear(),
  nkYear: new Date().getFullYear()-1,
  meta: null, lease: null, edit: null, nkProp: 0, txOpen: false, txYear: new Date().getFullYear(), showNew: false,
};
const HV_TABS = [['hv','Übersicht'],['hv-mieter','Mieter & Verträge'],['hv-objekte','Objekte'],['hv-nk','Nebenkosten'],['hv-umsaetze','Mietkonto'],['hv-fristen','Fristen']];
const STATUS = {bezahlt:['Bezahlt','ok'], teilweise:['Teilweise','warn'], offen:['Offen','bad'], faellig:['Fällig','muted']};
const KEYS = {flaeche:'Fläche', personen:'Personen', einheiten:'Einheiten'};
const m2 = c => nf2.format((c||0)/100).replace(/,00$/,'') + ' m²';

async function hvMeta(force){ if(!HV.meta || force) HV.meta = await api('GET','/api/hv/meta'); return HV.meta; }
const ctName = slug => (HV.meta?.cost_types||[]).find(c=>c.slug===slug)?.name || slug || '–';
const propName = id => (HV.meta?.properties||[]).find(p=>p.id===id)?.name || '–';

// Property management navigation lives in the header (section „Hausverwaltung“).
function hvNav(){ return ''; }
function pill(status){ const [l,c]=STATUS[status]||[status,'muted']; return `<span class="st st-${c}">${l}</span>`; }

async function viewHV(tab){
  await hvMeta(true);
  if(tab==='hv') return hvOverview();
  if(tab==='hv-mieter') return hvLeases();
  if(tab==='hv-objekte') return hvProperties();
  if(tab==='hv-nk') return hvNK();
  if(tab==='hv-umsaetze') return hvTxns();
  if(tab==='hv-fristen') return hvDeadlines();
  return hvOverview();
}

/* ---------- Overview ---------- */
async function hvOverview(){
  const ov = await api('GET','/api/hv/overview?month='+HV.month);
  ov.deadlines ||= []; ov.leases ||= []; ov.properties ||= []; ov.accounts ||= [];
  const t = ov.totals;
  const steps = [];
  if(!ov.accounts.length) steps.push(`Unter <a href="#konten">Konten</a> beim Mietkonto „Gehört zu: Hausverwaltung“ wählen.`);
  if(!ov.properties.length) steps.push(`Unter <a href="#hv-objekte">Objekte</a> Häuser bzw. Wohnungen mit ihren Einheiten anlegen.`);
  if(ov.properties.length && !ov.leases.length) steps.push(`Unter <a href="#hv-mieter">Mieter &amp; Verträge</a> die Mietverträge eintragen. Die App ordnet Zahlungen dann automatisch zu.`);
  const rows = ov.leases.map(l=>{
    const r=l.row;
    return `<tr data-open-lease="${l.id}" class="clickable">
      <td><b>${esc(l.tenant.name)}</b><br><span class="muted small">${esc(l.property_name)} · ${esc(l.unit_name)}</span></td>
      <td class="r tnum">${r?E(r.soll):'–'}</td><td class="r tnum">${r?E(r.paid):'–'}</td>
      <td>${r?pill(r.status):'<span class="muted small">kein Soll</span>'}${r&&r.status!=='bezahlt'?`<br><span class="muted small">fällig ${dm(r.due_date)}</span>`:''}</td>
      <td class="r tnum ${l.balance>0?'neg':l.balance<0?'pos':''}"><b>${E(l.balance)}</b><br><span class="muted small">${l.balance>0?'Rückstand':l.balance<0?'Guthaben':'ausgeglichen'}</span></td></tr>`;
  }).join('');
  const dl = ov.deadlines.slice(0,8).map(d=>`<li><span class="date-chip ${d.urgent?'urgent':''}">${dm(d.date)}</span><span>${esc(d.title)}<br><span class="muted small">${esc(d.detail||'')}</span></span><span></span></li>`).join('');
  main.innerHTML = hvNav('hv') + `
    ${steps.length?`<div class="banner"><p><b>So richtest du die Hausverwaltung ein:</b></p><ol class="steps">${steps.map(s=>`<li>${s}</li>`).join('')}</ol></div>`:''}
    ${ov.unassigned && ov.properties.length?`<div class="banner warn"><p>${ov.unassigned} Umsätze auf dem Mietkonto sind noch keinem Objekt bzw. keiner Kostenart zugeordnet.</p><a class="btn" href="#hv-umsaetze" data-hv="open-tx">Zuordnen</a></div>`:''}
    <div class="row"><div class="monthnav"><button data-hv="m-prev" aria-label="Vorheriger Monat">‹</button><span class="mwrap"><strong>${monthLabel(HV.month)}</strong><span class="prange">Mieteingänge</span></span><button data-hv="m-next" aria-label="Nächster Monat">›</button></div></div>
    <section class="stat-grid">
      <div class="card"><h3>Soll ${monthLabel(HV.month)}</h3><div class="bignum">${E(t.soll)}</div><p class="note">Kaltmiete + NK-Vorauszahlung aller Verträge</p></div>
      <div class="card"><h3>Eingegangen</h3><div class="bignum pos">${E(t.paid)}</div><p class="note">${t.soll?Math.round(t.paid/t.soll*100):0} % des Solls</p></div>
      <div class="card"><h3>Offen in diesem Monat</h3><div class="bignum ${t.open?'neg':''}">${E(t.open)}</div><p class="note">inkl. noch nicht fälliger Mieten</p></div>
      <div class="card"><h3>Rückstände gesamt</h3><div class="bignum ${t.arrears?'neg':''}">${E(t.arrears)}</div><p class="note">über alle Mieter und Monate</p></div>
    </section>
    <section class="second">
      <section class="ledger"><header><h2>Mieten ${monthLabel(HV.month)}</h2><span class="sum">Zeile anklicken für Details</span></header>
        <div class="tbl-scroll">${ov.leases.length?`<table><thead><tr><th>Mieter</th><th class="r">Soll</th><th class="r">Ist</th><th>Status</th><th class="r">Saldo</th></tr></thead><tbody>${rows}</tbody></table>`:'<div class="empty">Noch keine Mietverträge.</div>'}</div></section>
      <div class="card"><h3>Fristen &amp; Hinweise</h3>${dl?`<ul class="list">${dl}</ul>`:'<p class="muted">Keine Fristen in den nächsten zwei Monaten.</p>'}<p class="note"><a href="#hv-fristen">Alle Fristen</a></p></div>
    </section>`;
}

/* ---------- Tenants & leases ---------- */
function leaseForm(l){
  const props = HV.meta.properties;
  const units = props.flatMap(p=>p.units.map(u=>({...u, pname:p.name})));
  const v = (x)=> x!=null ? esc(x) : '';
  const money = c => c ? nf2.format(c/100) : '';
  return `<form id="leaseForm" class="card" data-id="${l.id||''}"><h3>${l.id?'Mietvertrag bearbeiten':'Neuer Mietvertrag'}</h3>
    ${units.length?'':'<p class="neg">Lege zuerst unter „Objekte“ ein Objekt mit Einheiten an.</p>'}
    <div class="form-grid">
      <div class="field"><label>Einheit</label><select id="lUnit" required>${units.map(u=>`<option value="${u.id}"${u.id===l.unit_id?' selected':''}>${esc(u.pname)} · ${esc(u.name)}</option>`).join('')}</select></div>
      <div class="field"><label>Mieter (Name)</label><input id="lName" required value="${v(l.tenant?.name)}" placeholder="z. B. Anna Schmidt"></div>
      <div class="field"><label>IBAN des Mieters</label><input id="lIban" value="${v(l.tenant?.iban)}" placeholder="für die Zahlungszuordnung"></div>
      <div class="field"><label>Weiterer Suchbegriff</label><input id="lMatch" value="${v(l.match_text)}" placeholder="falls jemand anderes überweist"></div>
      <div class="field"><label>E-Mail</label><input id="lMail" value="${v(l.tenant?.email)}"></div>
      <div class="field"><label>Telefon</label><input id="lPhone" value="${v(l.tenant?.phone)}"></div>
      <div class="field"><label>Mietbeginn</label><input id="lStart" type="date" required value="${v(l.start_date)}"></div>
      <div class="field"><label>Mietende (leer = unbefristet)</label><input id="lEnd" type="date" value="${v(l.end_date)}"></div>
      <div class="field"><label>Kaltmiete €</label><input id="lCold" inputmode="decimal" required value="${money(l.rent_cold)}"></div>
      <div class="field"><label>NK-Vorauszahlung €</label><input id="lNK" inputmode="decimal" value="${money(l.nk_prepay)}"></div>
      <div class="field"><label>Fällig am (Tag)</label><input id="lDue" type="number" min="1" max="28" value="${l.due_day||3}"></div>
      <div class="field"><label>Personen</label><input id="lPers" type="number" min="0" value="${l.persons??1}"></div>
      <div class="field"><label>Mietart</label><select id="lType"><option value="fest"${l.rent_type==='fest'?' selected':''}>Feste Miete</option><option value="staffel"${l.rent_type==='staffel'?' selected':''}>Staffelmiete</option><option value="index"${l.rent_type==='index'?' selected':''}>Indexmiete</option></select></div>
      <div class="field"><label>Letzte Mieterhöhung</label><input id="lInc" type="date" value="${v(l.last_increase)}"></div>
      <div class="field"><label>Kaution €</label><input id="lDep" inputmode="decimal" value="${money(l.deposit)}"></div>
      <div class="field"><label>Soll/Ist rechnen ab</label><input id="lTrack" type="date" value="${v(l.track_from)}" title="leer = Beginn der Kontodaten"></div>
    </div>
    <label class="small"><input type="checkbox" id="lDepPaid"${l.deposit_paid?' checked':''}> Kaution vollständig erhalten</label>
    <div class="field"><label>Notizen</label><input id="lNotes" value="${v(l.notes)}"></div>
    <div class="row" style="margin-top:10px"><button class="btn primary" type="submit">Speichern</button><button class="btn ghost" type="button" data-hv="cancel-lease">Abbrechen</button>
      ${l.id?`<span class="spacer"></span><button class="btn ghost danger" type="button" data-hv="del-lease" data-id="${l.id}">Vertrag löschen</button>`:''}</div>
  </form>`;
}

async function hvLeases(){
  const leases = HV.meta.leases;
  const today = new Date().toISOString().slice(0,10);
  const groups = {};
  for(const l of leases){ (groups[l.property_name] ||= []).push(l); }
  let detail = '';
  if(HV.lease){
    const d = await api('GET','/api/hv/leases/'+HV.lease).catch(()=>null);
    if(d) detail = leaseDetail(d);
  }
  main.innerHTML = hvNav('hv-mieter') + `
    ${HV.edit?leaseForm(HV.edit):`<div class="row"><span class="spacer"></span><button class="btn primary" data-hv="new-lease">Neuer Mietvertrag</button></div>`}
    ${detail}
    ${Object.keys(groups).length?Object.entries(groups).map(([pn,ls])=>`<section class="ledger"><header><h2>${esc(pn)}</h2></header><div class="tbl-scroll"><table>
      <thead><tr><th>Einheit</th><th>Mieter</th><th>Seit</th><th class="r">Kalt</th><th class="r">NK</th><th>Art</th><th></th></tr></thead><tbody>
      ${ls.map(l=>{const ended=l.end_date&&l.end_date<today; return `<tr class="${ended?'ended':''}"><td>${esc(l.unit_name)}</td><td><b>${esc(l.tenant.name)}</b>${ended?' <span class="muted small">(ausgezogen)</span>':''}</td>
        <td class="tnum">${dmy(l.start_date)}${l.end_date?' – '+dmy(l.end_date):''}</td><td class="r tnum">${E(l.rent_cold)}</td><td class="r tnum">${E(l.nk_prepay)}</td>
        <td>${({fest:'Fest',staffel:'Staffel',index:'Index'})[l.rent_type]}</td>
        <td class="r"><button class="btn small" data-open-lease="${l.id}">Konto</button> <button class="btn ghost small" data-hv="edit-lease" data-id="${l.id}">Bearbeiten</button></td></tr>`}).join('')}
      </tbody></table></div></section>`).join(''):'<div class="card empty">Noch keine Mietverträge.</div>'}`;
  if(HV.edit) $('#leaseForm').scrollIntoView({block:'start'});
  else if(HV.lease) $('#leaseDetail')?.scrollIntoView({block:'start'});
}

function leaseDetail(d){
  const l=d.lease, lg=d.ledger;
  return `<section class="ledger" id="leaseDetail"><header><h2>Mieterkonto ${esc(l.tenant.name)}</h2>
    <span class="sum">${esc(l.property_name)} · ${esc(l.unit_name)} · Saldo <b class="${lg.balance>0?'neg':''}">${E(lg.balance)}</b></span>
    <button class="btn ghost small" data-hv="close-lease">Schließen</button></header>
    <div class="lease-grid">
      <div><h3 class="label">Monate seit ${dmy(lg.from)}</h3><div class="tbl-scroll"><table><thead><tr><th>Monat</th><th class="r">Soll</th><th class="r">Bezahlt</th><th class="r">Offen</th><th>Status</th></tr></thead><tbody>
        ${lg.months.map(m=>`<tr><td>${monthLabel(m.month)}</td><td class="r tnum">${E(m.soll)}</td><td class="r tnum">${E(m.paid)}</td><td class="r tnum ${m.open?'neg':''}">${E(m.open)}</td><td>${pill(m.status)}</td></tr>`).join('')}
      </tbody></table></div></div>
      <div><h3 class="label">Zahlungen</h3><ul class="list">${lg.payments.slice().reverse().map(p=>`<li><span class="date-chip">${dm(p.date)}</span><span class="small">${esc(p.remittance||p.counterparty)}</span><b class="tnum pos">${E(p.amount)}</b></li>`).join('')||'<li><span></span><span class="muted">Keine Zahlungen zugeordnet.</span><span></span></li>'}</ul>
        <h3 class="label" style="margin-top:16px">Mietänderungen / Staffel</h3>
        <ul class="list">${l.steps.map(s=>`<li><span class="date-chip">${dm(s.valid_from)}${s.valid_from.slice(2,4)}</span><span>Kalt ${E(s.rent_cold)} · NK ${E(s.nk_prepay)}</span><button class="btn ghost small danger" data-hv="del-step" data-id="${s.id}">×</button></li>`).join('')||'<li><span></span><span class="muted small">Keine. Gilt: Kalt '+E(l.rent_cold)+' · NK '+E(l.nk_prepay)+'</span><span></span></li>'}</ul>
        <form id="stepForm" class="row small" data-lease="${l.id}" style="margin-top:8px">
          <input type="date" id="sFrom" required aria-label="gültig ab"><input id="sCold" inputmode="decimal" placeholder="Kalt €" required aria-label="neue Kaltmiete" style="width:90px"><input id="sNK" inputmode="decimal" placeholder="NK €" aria-label="neue Vorauszahlung" style="width:80px">
          <button class="btn small" type="submit">Änderung hinzufügen</button></form>
        <p class="note">Kaution ${E(l.deposit)} ${l.deposit_paid?'erhalten':'<span class="neg">noch offen</span>'} · ${l.persons} Person(en) · fällig zum ${l.due_day}.</p>
      </div></div></section>`;
}

/* ---------- Properties ---------- */
async function hvProperties(){
  const rep = await api('GET','/api/hv/report?year='+HV.year);
  rep.properties ||= [];
  const props = HV.meta.properties;
  const byId = Object.fromEntries(rep.properties.map(p=>[p.property_id,p]));
  main.innerHTML = hvNav('hv-objekte') + `
    <section class="card"><h3>Neues Objekt</h3><form id="propForm" class="form-grid">
      <div class="field"><label>Name</label><input id="pName" required placeholder="z. B. Talstraße 3"></div>
      <div class="field"><label>Adresse</label><input id="pAddr" placeholder="Straße Nr., PLZ Ort"></div>
      <div class="field"><label>Kaufpreis € (für Rendite)</label><input id="pPrice" inputmode="decimal"></div>
      <div class="field" style="align-self:end"><button class="btn primary" type="submit">Anlegen</button></div></form></section>
    <div class="row"><h3 class="label" style="margin:0">Jahresübersicht</h3><span class="spacer"></span>
      <button class="btn small" data-hv="y-prev">‹ ${HV.year-1}</button><b>${HV.year}</b><button class="btn small" data-hv="y-next">${HV.year+1} ›</button></div>
    ${props.map(p=>{ const r=byId[p.id]||{costs_by_type:{}};
      const costs=Object.entries(r.costs_by_type||{}).sort((a,b)=>b[1]-a[1]);
      return `<section class="ledger"><header><h2>${esc(p.name)}</h2><span class="sum">${esc(p.address)}</span>
        <button class="btn ghost small danger" data-hv="del-prop" data-id="${p.id}">Objekt löschen</button></header>
        <div class="lease-grid" style="padding:0 14px 14px">
          <div><h3 class="label">Einheiten</h3><table><thead><tr><th>Einheit</th><th class="r">Fläche</th><th></th></tr></thead><tbody>
            ${p.units.map(u=>`<tr><td>${esc(u.name)}</td><td class="r tnum">${m2(u.area)}</td><td class="r"><button class="btn ghost small danger" data-hv="del-unit" data-id="${u.id}">×</button></td></tr>`).join('')}
            <tr><td colspan="3"><form class="row small unitForm" data-prop="${p.id}"><input name="name" placeholder="z. B. EG links" required style="flex:2"><input name="area" inputmode="decimal" placeholder="m²" required style="width:80px"><button class="btn small" type="submit">Einheit hinzufügen</button></form></td></tr>
          </tbody></table>
          <p class="note">${r.occupied||0} von ${p.units.length} Einheiten vermietet. Kaufpreis ${p.purchase_price?E(p.purchase_price):'–'}.</p></div>
          <div><h3 class="label">${HV.year}</h3><table class="fc"><tbody>
            <tr><td>Einnahmen (Mieten inkl. NK)</td><td class="r tnum pos">${E(r.income)}</td></tr>
            ${costs.map(([k,v])=>`<tr><td class="muted">− ${esc(ctName(k))}</td><td class="r tnum">${E(-v)}</td></tr>`).join('')}
            <tr class="total"><td>Überschuss (Cashflow)</td><td class="r tnum ${r.surplus<0?'neg':''}">${E(r.surplus)}</td></tr>
            <tr><td class="muted small">davon umlagefähige Kosten</td><td class="r tnum small">${E(r.costs_umlage)}</td></tr>
            <tr><td class="muted small">Soll-Kaltmiete p. a. (aktuell)</td><td class="r tnum small">${E(r.cold_rent_year)}</td></tr>
            ${r.gross_yield?`<tr><td class="muted small">Bruttomietrendite</td><td class="r tnum small">${r.gross_yield.toFixed(2).replace('.',',')} %</td></tr>`:''}
          </tbody></table><p class="note">Darlehensraten enthalten Tilgung. Steuerlich absetzbar sind nur die Zinsen.</p></div>
        </div></section>`}).join('') || '<div class="card empty">Noch keine Objekte angelegt.</div>'}`;
}

/* ---------- Service charges ---------- */
async function hvNK(){
  const props = HV.meta.properties;
  if(!props.length){ main.innerHTML = hvNav('hv-nk') + '<div class="card empty">Lege zuerst ein Objekt an.</div>'; return; }
  if(!HV.nkProp || !props.find(p=>p.id===HV.nkProp)) HV.nkProp = props[0].id;
  const d = await api('GET',`/api/hv/nk?property_id=${HV.nkProp}&year=${HV.nkYear}`);
  const st = d.settlement;
  const umlage = HV.meta.cost_types.filter(c=>c.umlagefaehig);
  main.innerHTML = hvNav('hv-nk') + `
    <section class="card no-print"><div class="filters" style="grid-template-columns:2fr 1fr 2fr">
      <div class="field"><label for="nkProp">Objekt</label><select id="nkProp">${props.map(p=>`<option value="${p.id}"${p.id===HV.nkProp?' selected':''}>${esc(p.name)}</option>`).join('')}</select></div>
      <div class="field"><label for="nkYear">Abrechnungsjahr</label><select id="nkYear">${[0,1,2,3].map(i=>new Date().getFullYear()-i).map(y=>`<option${y===HV.nkYear?' selected':''}>${y}</option>`).join('')}</select></div>
      <div class="field"><label>Frist</label><div class="tnum" style="padding:8px 0">Zustellung an die Mieter bis <b>${dmy(st.deadline)}</b></div></div></div></section>
    <section class="ledger no-print"><header><h2>Umlagefähige Kosten ${st.year}</h2><span class="sum">gesamt <b>${E(st.cost_total)}</b> · Eigentümeranteil (Leerstand) ${E(st.owner_share)}</span></header>
      <div class="tbl-scroll"><table><thead><tr><th>Kostenart</th><th>Verteilung nach</th><th class="r">Mietkonto</th><th class="r">Ergänzt</th><th class="r">Gesamt</th></tr></thead><tbody>
      ${st.costs.map(c=>`<tr><td>${esc(c.name)}</td><td><select class="inline" data-nkkey="${c.cost_type}">${Object.entries(KEYS).map(([k,l])=>`<option value="${k}"${k===c.key?' selected':''}>${l}</option>`).join('')}</select></td>
        <td class="r tnum">${E(c.from_bank)}</td><td class="r tnum">${c.manual?E(c.manual):''}</td><td class="r tnum"><b>${E(c.total)}</b></td></tr>`).join('') || '<tr><td colspan="5" class="empty">Keine umlagefähigen Kosten für dieses Jahr gefunden.</td></tr>'}
      </tbody></table></div>
      <form id="mcForm" class="row small" style="padding:10px 14px">
        <b>Kosten ergänzen</b> <span class="muted">(z. B. aus der WEG-Abrechnung oder von einem anderen Konto)</span>
        <select id="mcType" class="inline">${umlage.map(c=>`<option value="${c.slug}">${esc(c.name)}</option>`).join('')}</select>
        <input id="mcAmount" inputmode="decimal" placeholder="Betrag €" required style="width:100px"><input id="mcNote" placeholder="Notiz" style="width:160px">
        <button class="btn small" type="submit">Hinzufügen</button></form>
      ${d.manual_costs.length?`<ul class="list" style="padding:0 14px 10px">${d.manual_costs.map(m=>`<li><span class="date-chip">+</span><span>${esc(ctName(m.cost_type))} ${m.note?'· '+esc(m.note):''}</span><span class="tnum">${E(m.amount)} <button class="btn ghost small danger" data-hv="del-mc" data-id="${m.id}">×</button></span></li>`).join('')}</ul>`:''}
      <p class="note" style="padding:0 14px 12px">Heizkosten sind hier vereinfacht nach Fläche verteilt. Gesetzlich müssen sie meist zu 50–70 % nach Verbrauch abgerechnet werden (Heizkostenverordnung). Nimm dafür die Verbrauchsabrechnung (ista, Techem, …) und trage den Betrag je Mieter manuell ein.</p></section>
    ${st.statements.map(s=>`<section class="card statement" id="stmt-${s.lease_id}">
      <div class="row"><div><h3 style="margin:0">Nebenkostenabrechnung ${st.year}</h3><div class="muted small">${esc(d.property.name)}${d.property.address?', '+esc(d.property.address):''}</div></div><span class="spacer"></span>
        <button class="btn small no-print" data-hv="print" data-id="${s.lease_id}">Drucken / PDF</button></div>
      <p><b>${esc(s.tenant)}</b> · ${esc(s.unit)} · Zeitraum ${dmy(s.from)} – ${dmy(s.to)} (${s.days} Tage)</p>
      <div class="tbl-scroll"><table><thead><tr><th>Kostenart</th><th class="r">Gesamtkosten</th><th>Verteilung</th><th class="r">Ihr Anteil</th></tr></thead><tbody>
        ${s.shares.map(x=>`<tr><td>${esc(x.name)}</td><td class="r tnum">${E(st.costs.find(c=>c.cost_type===x.cost_type)?.total)}</td><td class="small muted">${esc(KEYS[x.key])}: ${esc(x.basis)}</td><td class="r tnum">${E(x.amount)}</td></tr>`).join('')}
      </tbody><tfoot>
        <tr><td colspan="3">Ihre Kosten</td><td class="r tnum">${E(s.cost_sum)}</td></tr>
        <tr><td colspan="3">abzüglich geleistete Vorauszahlungen</td><td class="r tnum">${E(-s.prepaid)}</td></tr>
        <tr><td colspan="3"><b>${s.result>0?'Nachzahlung':'Guthaben'}</b></td><td class="r tnum"><b class="${s.result>0?'neg':'pos'}">${E(Math.abs(s.result))}</b></td></tr>
      </tfoot></table></div>
      <p class="note">Vorschlag für die neue monatliche Vorauszahlung: ${E(s.suggested_prepay)}. ${s.result>0?'Die Nachzahlung ist innerhalb von 30 Tagen nach Zugang fällig.':'Das Guthaben wird erstattet bzw. verrechnet.'}</p>
    </section>`).join('') || '<div class="card empty">In diesem Jahr gab es für das Objekt keine Mietverträge.</div>'}`;
}

/* ---------- Rent account transactions ---------- */
async function hvTxns(){
  const p = new URLSearchParams({year:HV.txYear}); if(HV.txOpen) p.set('open','1');
  const txs = (await api('GET','/api/hv/transactions?'+p)) || [];
  const props = HV.meta.properties, leases = HV.meta.leases;
  const inc = HV.meta.cost_types.filter(c=>c.kind!=='kosten'), cost = HV.meta.cost_types.filter(c=>c.kind==='kosten');
  const ctOpts = (sel, credit) => `<optgroup label="Einnahmen / neutral">${inc.map(c=>`<option value="${c.slug}"${c.slug===sel?' selected':''}>${esc(c.name)}</option>`).join('')}</optgroup>
    <optgroup label="Kosten">${cost.map(c=>`<option value="${c.slug}"${c.slug===sel?' selected':''}>${esc(c.name)}${c.umlagefaehig?' (umlagefähig)':''}</option>`).join('')}</optgroup>`;
  main.innerHTML = hvNav('hv-umsaetze') + `
    <div class="row"><button class="btn small" data-hv="ty-prev">‹ ${HV.txYear-1}</button><b>${HV.txYear}</b><button class="btn small" data-hv="ty-next">${HV.txYear+1} ›</button>
      <span class="spacer"></span><label class="small"><input type="checkbox" id="txOpen"${HV.txOpen?' checked':''}> Nur nicht zugeordnete</label></div>
    <section class="ledger"><header><h2>Mietkonto ${HV.txYear}</h2><span class="sum">${txs.length} Umsätze · Änderungen gelten für die Zeile, mit „merken“ für alle vom selben Empfänger</span></header>
      <div class="tbl-scroll">${txs.length?`<table><thead><tr><th>Datum</th><th>Empfänger / Zweck</th><th>Objekt</th><th>Mietvertrag</th><th>Art</th><th class="r">Betrag</th></tr></thead><tbody>
      ${txs.map(t=>`<tr data-hvtx="${t.id}">
        <td class="tnum">${dmy(t.date)}</td>
        <td><div class="tx-main"><b>${esc(t.merchant||t.counterparty||'–')}</b><span class="muted small clip" title="${esc(t.remittance)}">${esc(t.remittance)}</span></div></td>
        <td><select class="inline" data-f="property"><option value="">–</option>${props.map(p=>`<option value="${p.id}"${p.id===t.property_id?' selected':''}>${esc(p.name)}</option>`).join('')}</select></td>
        <td>${t.amount>0?`<select class="inline" data-f="lease"><option value="">–</option>${leases.map(l=>`<option value="${l.id}"${l.id===t.lease_id?' selected':''}>${esc(l.tenant.name)} · ${esc(l.unit_name)}</option>`).join('')}</select>`:''}</td>
        <td><select class="inline" data-f="cost">${ctOpts(t.cost_type)}</select>${t.hv_source==='manual'?' <span class="src manual">Hand</span>':''}<br><label class="small muted"><input type="checkbox" data-f="remember"> merken</label></td>
        <td class="r tnum ${t.amount>0?'amt-in':'amt-out'}">${E(t.amount)}</td></tr>`).join('')}</tbody></table>`:'<div class="empty">Keine Umsätze. Ist das Mietkonto unter „Konten“ der Hausverwaltung zugeordnet?</div>'}</div></section>`;
}

/* ---------- Deadlines ---------- */
async function hvDeadlines(){
  const rep = await api('GET','/api/hv/report?year='+new Date().getFullYear());
  rep.deadlines ||= [];
  const kinds = {nk:'Nebenkosten', staffel:'Mietänderung', erhoehung:'Mieterhöhung', index:'Index', ende:'Mietende', kaution:'Kaution', rueckstand:'Rückstand', manuell:'Wiedervorlage'};
  main.innerHTML = hvNav('hv-fristen') + `
    <section class="card"><h3>Wiedervorlage anlegen</h3><form id="remForm" class="form-grid">
      <div class="field"><label>Was</label><input id="rTitle" required placeholder="z. B. Rauchmelder prüfen, Zählerstände ablesen"></div>
      <div class="field"><label>Wann</label><input id="rDate" type="date" required></div>
      <div class="field"><label>Objekt</label><select id="rProp"><option value="">–</option>${HV.meta.properties.map(p=>`<option value="${p.id}">${esc(p.name)}</option>`).join('')}</select></div>
      <div class="field" style="align-self:end"><button class="btn primary" type="submit">Anlegen</button></div></form></section>
    <section class="ledger"><header><h2>Fristen &amp; Hinweise</h2><span class="sum">automatisch aus Verträgen, Rückständen und deinen Wiedervorlagen</span></header>
      <div class="tbl-scroll"><table><thead><tr><th>Datum</th><th>Art</th><th>Was</th><th></th></tr></thead><tbody>
      ${rep.deadlines.map(d=>`<tr class="${d.urgent?'urgent-row':''}"><td class="tnum">${dmy(d.date)}</td><td><span class="pill">${kinds[d.kind]||d.kind}</span></td>
        <td><b>${esc(d.title)}</b><br><span class="muted small">${esc(d.detail||'')}</span></td>
        <td class="r">${d.reminder_id?`<button class="btn small" data-hv="rem-done" data-id="${d.reminder_id}">Erledigt</button>`:''}</td></tr>`).join('') || '<tr><td colspan="4" class="empty">Keine Fristen.</td></tr>'}
      </tbody></table></div></section>
    <p class="note">Mieterhöhungen auf die Vergleichsmiete werden frühestens 15 Monate nach der letzten Erhöhung wirksam, das Verlangen muss 2 Monate vorher zugehen. Nebenkostenabrechnungen müssen spätestens 12 Monate nach Ende des Abrechnungszeitraums beim Mieter sein. Diese Hinweise ersetzen keine Rechtsberatung.</p>`;
}

/* ---------- Events ---------- */
const cents = v => { const c=parseDE(v); return c===null?0:c; };
document.addEventListener('click', async e=>{
  const ol = e.target.closest('[data-open-lease]');
  if(ol && S.tab.startsWith('hv')){ HV.lease=+ol.dataset.openLease; HV.edit=null; if(location.hash!=='#hv-mieter') location.hash='#hv-mieter'; else hvLeases(); return; }
  const b = e.target.closest('[data-hv]'); if(!b) return;
  const act=b.dataset.hv, id=b.dataset.id;
  try{
    if(act==='m-prev'){ HV.month=shiftMonth(HV.month,-1); hvOverview(); }
    if(act==='m-next'){ HV.month=shiftMonth(HV.month,1); hvOverview(); }
    if(act==='y-prev'){ HV.year--; hvProperties(); }
    if(act==='y-next'){ HV.year++; hvProperties(); }
    if(act==='ty-prev'){ HV.txYear--; hvTxns(); }
    if(act==='ty-next'){ HV.txYear++; hvTxns(); }
    if(act==='open-tx'){ HV.txOpen=true; }
    if(act==='new-lease'){ HV.edit={}; HV.lease=null; hvLeases(); }
    if(act==='edit-lease'){ HV.edit=HV.meta.leases.find(l=>l.id===+id); hvLeases(); }
    if(act==='cancel-lease'){ HV.edit=null; hvLeases(); }
    if(act==='close-lease'){ HV.lease=null; hvLeases(); }
    if(act==='print'){ document.querySelectorAll('.statement').forEach(x=>x.classList.toggle('print-target', x.id==='stmt-'+id)); window.print(); }
    const confirmDelete = async (path, msg)=>{ if(b.dataset.confirm!=='1'){ b.dataset.confirm='1'; b.textContent='Wirklich löschen?'; return false; } await api('DELETE',path); toast(msg,2500); await hvMeta(true); return true; };
    if(act==='del-lease' && await confirmDelete('/api/hv/leases/'+id,'Mietvertrag gelöscht')){ HV.edit=null; HV.lease=null; hvLeases(); }
    if(act==='del-prop' && await confirmDelete('/api/hv/properties/'+id,'Objekt gelöscht')) hvProperties();
    if(act==='del-unit' && await confirmDelete('/api/hv/units/'+id,'Einheit gelöscht')) hvProperties();
    if(act==='del-step'){ await api('DELETE','/api/hv/steps/'+id); await hvMeta(true); hvLeases(); }
    if(act==='del-mc'){ await api('DELETE','/api/hv/manual-costs/'+id); hvNK(); }
    if(act==='rem-done'){ await api('PATCH','/api/hv/reminders/'+id,{done:true}); hvDeadlines(); }
  }catch(err){ toastError(err); }
});

document.addEventListener('submit', async e=>{
  const f=e.target; if(!S.tab?.startsWith('hv')) return;
  e.preventDefault();
  try{
    if(f.id==='leaseForm'){
      const body={unit_id:+$('#lUnit').value, tenant:{id:HV.edit?.tenant?.id||0, name:$('#lName').value.trim(), iban:$('#lIban').value.trim(), email:$('#lMail').value.trim(), phone:$('#lPhone').value.trim()},
        start_date:$('#lStart').value, end_date:$('#lEnd').value, rent_cold:cents($('#lCold').value), nk_prepay:cents($('#lNK').value), due_day:+$('#lDue').value||3,
        persons:+$('#lPers').value||0, rent_type:$('#lType').value, last_increase:$('#lInc').value, deposit:cents($('#lDep').value), deposit_paid:$('#lDepPaid').checked,
        track_from:$('#lTrack').value, match_text:$('#lMatch').value.trim(), notes:$('#lNotes').value};
      const r = f.dataset.id ? await api('PUT','/api/hv/leases/'+f.dataset.id, body) : await api('POST','/api/hv/leases', body);
      toast('Mietvertrag gespeichert, Zahlungen neu zugeordnet',3000); HV.edit=null; HV.lease=r.id; await hvMeta(true); hvLeases();
    }
    if(f.id==='stepForm'){
      await api('POST',`/api/hv/leases/${f.dataset.lease}/steps`,{valid_from:$('#sFrom').value, rent_cold:cents($('#sCold').value), nk_prepay:cents($('#sNK').value)});
      toast('Mietänderung gespeichert',2500); await hvMeta(true); hvLeases();
    }
    if(f.id==='propForm'){
      const price=cents($('#pPrice').value);
      await api('POST','/api/hv/properties',{name:$('#pName').value.trim(), address:$('#pAddr').value.trim(), purchase_price:price||null});
      toast('Objekt angelegt. Jetzt die Einheiten ergänzen.',3500); await hvMeta(true); hvProperties();
    }
    if(f.classList.contains('unitForm')){
      await api('POST','/api/hv/units',{property_id:+f.dataset.prop, name:f.elements.name.value.trim(), area:cents(f.elements.area.value)});
      await hvMeta(true); hvProperties();
    }
    if(f.id==='mcForm'){
      await api('POST','/api/hv/manual-costs',{property_id:HV.nkProp, year:HV.nkYear, cost_type:$('#mcType').value, amount:cents($('#mcAmount').value), note:$('#mcNote').value});
      hvNK();
    }
    if(f.id==='remForm'){
      await api('POST','/api/hv/reminders',{title:$('#rTitle').value.trim(), due_date:$('#rDate').value, property_id:+$('#rProp').value||null});
      toast('Wiedervorlage angelegt',2500); hvDeadlines();
    }
  }catch(err){ toastError(err); }
});

document.addEventListener('change', async e=>{
  const el=e.target; if(!S.tab?.startsWith('hv')) return;
  try{
    if(el.id==='nkProp'){ HV.nkProp=+el.value; hvNK(); }
    if(el.id==='nkYear'){ HV.nkYear=+el.value; hvNK(); }
    if(el.dataset.nkkey){ await api('PUT','/api/hv/nk-keys',{property_id:HV.nkProp, cost_type:el.dataset.nkkey, key:el.value}); hvNK(); }
    if(el.id==='txOpen'){ HV.txOpen=el.checked; hvTxns(); }
    const row=el.closest('[data-hvtx]');
    if(row && el.dataset.f && el.dataset.f!=='remember'){
      const q=s=>row.querySelector(`[data-f="${s}"]`);
      await api('PATCH','/api/hv/transactions/'+row.dataset.hvtx,{property_id:+q('property').value||0, lease_id:+(q('lease')?.value||0), cost_type:q('cost').value, remember:q('remember').checked});
      toast(q('remember').checked?'Gespeichert und für diesen Empfänger gemerkt':'Gespeichert',2500);
      if(q('remember').checked) hvTxns();
    }
  }catch(err){ toastError(err); }
});
