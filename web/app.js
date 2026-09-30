'use strict';
/* Homestead – web UI. No build steps, no dependencies. */

const GROUPS = {
  income:   {label:'Einnahmen', color:'--c-income'},
  bills:    {label:'Fixkosten', color:'--c-bills'},
  expenses: {label:'Ausgaben',  color:'--c-expenses'},
  savings:  {label:'Sparen',    color:'--c-savings'},
  debts:    {label:'Schulden',  color:'--c-debts'},
  transfer: {label:'Umbuchungen', color:'--c-transfer'},
};
const OUT = ['bills','expenses','savings','debts'];
const KINDS = {abo:'Abo', fixkosten:'Fixkosten', kredit:'Kredit', sparen:'Sparen', einkommen:'Einkommen', sonstiges:'Sonstiges'};
const MONTHS = ['Januar','Februar','März','April','Mai','Juni','Juli','August','September','Oktober','November','Dezember'];

const eur = new Intl.NumberFormat('de-DE',{style:'currency',currency:'EUR'});
const eur0 = new Intl.NumberFormat('de-DE',{style:'currency',currency:'EUR',maximumFractionDigits:0});
const nf2 = new Intl.NumberFormat('de-DE',{minimumFractionDigits:2,maximumFractionDigits:2});
const E = c => eur.format((c||0)/100);
const E0 = c => eur0.format((c||0)/100);
const esc = s => String(s ?? '').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const $ = s => document.querySelector(s);
const main = $('#main');

function parseDE(s){
  s = String(s ?? '').replace(/[€\s]/g,'');
  if(!s) return 0;
  if(s.includes(',')) s = s.replace(/\./g,'').replace(',','.');
  const n = parseFloat(s);
  return isFinite(n) ? Math.round(n*100) : null;
}
const dmy = iso => iso ? iso.split('-').reverse().join('.') : '';
const dm = iso => iso ? iso.slice(8,10)+'.'+iso.slice(5,7)+'.' : '';
function monthLabel(id){const [y,m]=id.split('-');return `${MONTHS[+m-1]} ${y}`}
function shiftMonth(id,d){let [y,m]=id.split('-').map(Number);m+=d;while(m<1){m+=12;y--}while(m>12){m-=12;y++}return `${y}-${String(m).padStart(2,'0')}`}
function ago(iso){
  if(!iso) return 'noch nie';
  const s=(Date.now()-new Date(iso))/1000;
  if(s<90) return 'gerade eben';
  if(s<3600) return `vor ${Math.round(s/60)} Min.`;
  if(s<86400) return `vor ${Math.round(s/3600)} Std.`;
  return `vor ${Math.round(s/86400)} Tagen`;
}

/* ---------- API ---------- */
async function api(method, path, body){
  const res = await fetch(path, {method, headers: body!==undefined?{'Content-Type':'application/json'}:{}, body: body!==undefined?JSON.stringify(body):undefined, credentials:'same-origin'});
  if(res.status===401 && path!=='/api/login'){ S.loggedIn=false; location.hash='#login'; throw new Error('Bitte anmelden'); }
  const data = await res.json().catch(()=>({}));
  if(!res.ok) throw new Error(data.error || `Fehler ${res.status}`);
  return data;
}

/* ---------- State ---------- */
const now = new Date();
const S = {
  tab:'monat', month:`${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,'0')}`,
  me:null, loggedIn:false, cats:[], overview:null,
  txFilter:{q:'', account:'', cat:'', group:''},
  bankFilter:'', banks:null, showBanks:false,
};
const catById = id => S.cats.find(c=>c.id===id);
const nameOf = o => o==='A' ? (S.me?.name_a||'Person A') : o==='B' ? (S.me?.name_b||'Person B') : 'Gemeinsam';

let toastTimer;
function toast(html, ms=5000){
  const t=$('#toast'); t.innerHTML=html; t.hidden=false;
  clearTimeout(toastTimer); if(ms) toastTimer=setTimeout(()=>t.hidden=true, ms);
}
function toastError(e){ toast(`<span>${esc(e.message||e)}</span>`, 7000); }

/* ---------- Routing ---------- */
async function route(){
  const h=(location.hash||'#monat').slice(1);
  if(!S.me){ try{ S.me = await api('GET','/api/me'); S.loggedIn=S.me.logged_in; }catch(e){ main.innerHTML=`<div class="card empty">Server nicht erreichbar: ${esc(e.message)}</div>`; return; } }
  if(S.me.auth_required && !S.loggedIn){ return viewLogin(); }
  S.tab=['monat','umsaetze','abos','paar','konten'].includes(h)||AREAS.verwaltung.tabs.some(t=>t[0]===h)?h:'monat';
  if(!S.monthInit){ S.month=curMonth(); S.monthInit=true; }
  if(!S.month && S.tab!=='umsaetze') S.month=curMonth();
  $('#top').hidden=false; $('#side').hidden=false;
  const isHV = S.tab.startsWith('hv');
  renderArea(isHV ? 'verwaltung' : 'haushalt');
  const monthTab = ['monat','umsaetze','paar'].includes(S.tab);
  $('#monthnav').hidden = !(monthTab || S.tab==='hv');
  const tabLabel = AREAS[S.area].tabs.find(t=>t[0]===S.tab)?.[1] || '';
  $('#mLabel').textContent = S.tab==='hv' ? `Mieteingänge ${monthLabel(HV.month)}`
    : monthTab ? (S.month ? monthLabel(S.month) : 'Alle Monate') : tabLabel;
  $('#areaTitle').textContent = monthTab && S.tab!=='monat' ? `${AREAS[S.area].title} · ${tabLabel}` : AREAS[S.area].title;
  $('#pRange').textContent = '';
  if(!S.cats.length){ try{ S.cats = await api('GET','/api/categories'); }catch(e){ return toastError(e); } }
  try{
    if(S.tab==='monat') await viewMonat();
    else if(S.tab==='umsaetze') await viewUmsaetze();
    else if(S.tab==='abos') await viewAbos();
    else if(S.tab==='paar') await viewPaar();
    else if(S.tab==='konten') await viewKonten();
    else if(S.tab==='hv-konten') await viewKonten('verwaltung');
    else if(isHV) await viewHV(S.tab);
  }catch(e){ if(e.message!=='Bitte anmelden') main.innerHTML=`<div class="card empty">${esc(e.message)}</div>`; }
  updateSyncState();
  renderSide();
}
window.addEventListener('hashchange', route);

/* ---------- Theme: Auto (follows the system) | Hell | Dunkel ---------- */
const THEMES = [
  ['auto','Auto',svgIcon('<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>')],
  ['light','Hell',svgIcon('<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>')],
  ['dark','Dunkel',svgIcon('<path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z"/>')],
];
function svgIcon(d){ return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`; }
function themePref(){ try{ return localStorage.getItem('hs-theme')||'auto'; }catch{ return 'auto'; } }
const darkMQ = matchMedia('(prefers-color-scheme: dark)');
function applyTheme(){
  const p = themePref();
  document.documentElement.dataset.theme = p==='auto' ? (darkMQ.matches?'dark':'light') : p;
  document.querySelectorAll('.theme-slot').forEach(el=>{ el.innerHTML = themeSwitchHTML(); });
}
function themeSwitchHTML(){
  const p = themePref();
  return `<div class="theme-sw" role="group" aria-label="Erscheinungsbild">${THEMES.map(([k,l,i])=>`<button type="button" data-act="theme" data-theme="${k}" aria-pressed="${p===k}" title="${l}">${i}<span>${l}</span></button>`).join('')}</div>`;
}
darkMQ.addEventListener?.('change', ()=>{ if(themePref()==='auto') applyTheme(); });
applyTheme();

/* ---------- Sidebar: account balances ---------- */
async function renderSide(){
  try{
    const accs = (await api('GET','/api/accounts')) || [];
    const mine = accs.filter(a=>a.active && (a.book||'haushalt')===(S.area==='verwaltung'?'verwaltung':'haushalt'));
    $('#sideAccTitle').textContent = S.area==='verwaltung' ? 'Mietkonten' : 'Konten';
    $('#sideAccounts').innerHTML = mine.length
      ? mine.map(a=>`<div><span title="${esc(a.bank)}">${esc(a.display_name||a.name)}</span><b class="${a.balance<0?'neg':''}">${a.balance!=null?E0(a.balance):'–'}</b></div>`).join('')
      : '<div><span>Noch kein Konto</span></div>';
    const next = mine.map(a=>a.schedule?.next).filter(Boolean).sort()[0];
    S.nextSync = next || null;
    updateSyncState();
  }catch{}
}

/* ---------- Sections: household budget | property management ---------- */
const AREAS = {
  haushalt: {title:'Haushaltsbuch', tabs:[['monat','Monatsbudget'],['umsaetze','Umsätze'],['abos','Abos & Fixkosten'],['paar','Paar-Aufteilung'],['konten','Konten']]},
  verwaltung: {title:'Hausverwaltung', tabs:[['hv','Übersicht'],['hv-mieter','Mieter & Verträge'],['hv-objekte','Objekte'],['hv-nk','Nebenkosten'],['hv-umsaetze','Mietkonto'],['hv-fristen','Fristen'],['hv-konten','Konten']]},
};
S.lastTab = {haushalt:'monat', verwaltung:'hv'};
const svg = d => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`;
const ICONS = {
  monat: svg('<circle cx="12" cy="12" r="9"/><path d="M12 3v9l6 4"/>'),
  umsaetze: svg('<path d="M4 6h16M4 12h16M4 18h10"/>'),
  abos: svg('<path d="M17 2l4 4-4 4"/><path d="M3 11V9a3 3 0 0 1 3-3h15"/><path d="M7 22l-4-4 4-4"/><path d="M21 13v2a3 3 0 0 1-3 3H3"/>'),
  paar: svg('<circle cx="9" cy="8" r="3.5"/><circle cx="17" cy="9" r="2.5"/><path d="M3 20c0-3.3 2.7-5.5 6-5.5s6 2.2 6 5.5"/><path d="M15.5 14.6c2.9.2 5.5 2 5.5 5.4"/>'),
  konten: svg('<path d="M3 10l9-6 9 6"/><path d="M5 10v8M9.5 10v8M14.5 10v8M19 10v8"/><path d="M3 21h18"/>'),
  hv: svg('<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>'),
  'hv-mieter': svg('<circle cx="9" cy="8" r="3.5"/><path d="M3 20c0-3.3 2.7-5.5 6-5.5s6 2.2 6 5.5"/><path d="M16 11h5M18.5 8.5v5"/>'),
  'hv-objekte': svg('<path d="M4 21V8l8-5 8 5v13"/><path d="M9 21v-5h6v5"/>'),
  'hv-nk': svg('<path d="M6 3h9l4 4v14H6z"/><path d="M9 12h7M9 16h7M9 8h3"/>'),
  'hv-umsaetze': svg('<path d="M4 6h16M4 12h16M4 18h10"/>'),
  'hv-fristen': svg('<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/>'),
  'hv-konten': svg('<path d="M3 10l9-6 9 6"/><path d="M5 10v8M9.5 10v8M14.5 10v8M19 10v8"/><path d="M3 21h18"/>'),
};
// Short labels for the mobile bottom bar
const NAV_SHORT = {monat:'Budget', abos:'Abos', paar:'Paar', 'hv':'Übersicht', 'hv-mieter':'Mieter', 'hv-umsaetze':'Mietkonto'};
function renderArea(area){
  S.area = area;
  S.lastTab[area] = S.tab;
  document.body.dataset.area = area;
  document.title = AREAS[area].title + ' · Homestead';
  document.querySelectorAll('.areas a').forEach(a=>{
    const on = a.dataset.area===area;
    a.toggleAttribute('aria-current', on); if(on) a.setAttribute('aria-current','page');
    a.href = '#' + S.lastTab[a.dataset.area];
  });
  $('#tabs').innerHTML = AREAS[area].tabs.map(([h,l])=>`<a href="#${h}"${h===S.tab?' aria-current="page"':''}>${ICONS[h]||''}<span>${esc(NAV_SHORT[h]&&window.innerWidth<=900?NAV_SHORT[h]:l)}</span></a>`).join('');
}
const curMonth=()=> S.me?.current_month || `${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,'0')}`;
$('#prevM').onclick=()=>{ if(S.tab==='hv') HV.month=shiftMonth(HV.month,-1); else S.month=shiftMonth(S.month||curMonth(),-1); route(); };
$('#nextM').onclick=()=>{ if(S.tab==='hv') HV.month=shiftMonth(HV.month,1); else S.month=shiftMonth(S.month||curMonth(),1); route(); };

/* ---------- Login ---------- */
function viewLogin(){
  $('#top').hidden=true; $('#side').hidden=true;
  main.innerHTML=`<form class="card login" id="loginForm">
    <h2>Homestead</h2>
    <div class="field"><label for="pw">Passwort</label><input type="password" id="pw" autocomplete="current-password" required autofocus></div>
    <button class="btn primary" type="submit">Anmelden</button>
    <p class="note" id="loginMsg"></p></form>`;
  $('#loginForm').onsubmit=async e=>{
    e.preventDefault();
    try{ await api('POST','/api/login',{password:$('#pw').value}); S.loggedIn=true; S.me=null; location.hash='#monat'; route(); }
    catch(err){ $('#loginMsg').textContent=err.message; }
  };
}

/* ---------- Charts ---------- */
function trendBars(trend){
  if(!trend.length) return '<p class="muted">Noch keine Daten.</p>';
  const outOf=p=>p.bills+p.expenses+p.debts+p.savings;
  const max=Math.max(1,...trend.map(p=>Math.max(p.income,outOf(p))));
  const h=v=>Math.max(2,Math.round(Math.max(0,v)/max*118));
  return `<div class="tbars">${trend.map(p=>`<div class="m${p.month===S.month?' cur':''}" title="${monthLabel(p.month)}: Einnahmen ${E(p.income)}, Ausgaben ${E(outOf(p))}">
    <div class="pair"><i style="height:${h(p.income)}px;background:var(--c-income);opacity:.45"></i><i style="height:${h(outOf(p))}px;background:var(--accent)"></i></div>
    <span class="lbl">${MONTHS[+p.month.slice(5)-1].slice(0,3)}</span></div>`).join('')}</div>`;
}

/* ---------- Monthly budget ---------- */
async function viewMonat(){
  const ov = S.overview = await api('GET','/api/overview?month='+S.month);
  showPeriod(ov.period);
  ov.accounts ||= []; ov.upcoming ||= []; ov.trend ||= []; ov.report.lines ||= [];
  S.me.name_a=ov.name_a; S.me.name_b=ov.name_b;
  const lines = ov.report.lines;
  const sum = (g,f) => lines.filter(l=>l.group===g).reduce((a,l)=>a+l[f],0);
  const t={}; for(const g of Object.keys(GROUPS)) t[g]={budget:sum(g,'budget'),ist:sum(g,'ist')};
  const outIst=OUT.reduce((a,g)=>a+t[g].ist,0), outBudget=OUT.reduce((a,g)=>a+t[g].budget,0);
  const avail=t.income.ist-outIst, plan=t.income.budget-outBudget;
  const quote=t.income.ist>0?Math.round(t.savings.ist/t.income.ist*100):0;
  const noBudgets = lines.every(l=>l.budget===0);
  const avgN = ov.report.avg_periods||0;
  // Budget doesn't match the average: no budget despite regular amounts, or deviation > 30 % and > €50
  const offBudget = l => avgN>0 && l.group!=='transfer' && (l.budget===0 ? l.avg>=2000 : Math.abs(l.budget-l.avg) > Math.max(l.avg*0.3, 5000));
  const offLines = noBudgets ? [] : lines.filter(offBudget);

  const banners=[];
  if(!ov.accounts.length) banners.push(`<div class="banner"><p><b>Noch kein Konto verbunden.</b> Verbinde deine Bank, dann werden Umsätze automatisch abgerufen und zugeordnet.</p><a class="btn primary" href="#konten">Bank verbinden</a></div>`);
  const exp = ov.accounts.filter(a=>a.connection_status==='expired');
  if(exp.length) banners.push(`<div class="banner bad"><p><b>Bankfreigabe abgelaufen</b> für ${exp.map(a=>esc(a.bank)).filter((v,i,s)=>s.indexOf(v)===i).join(', ')}. Ohne neue Freigabe kommen keine neuen Umsätze.</p><a class="btn" href="#konten">Neu verbinden</a></div>`);
  const soon = ov.accounts.filter(a=>a.connection_status==='active' && a.valid_until && (new Date(a.valid_until)-Date.now())<14*864e5);
  if(soon.length) banners.push(`<div class="banner warn"><p>Die Bankfreigabe für ${esc(soon[0].bank)} läuft am ${dmy(soon[0].valid_until.slice(0,10))} ab.</p><a class="btn" href="#konten">Verlängern</a></div>`);
  if(ov.accounts.length && noBudgets) banners.push(`<div class="banner"><p><b>Noch keine Budgets.</b> Ich kann jedes Budget auf den Durchschnitt der letzten ${avgN>1?avgN+' Monate':'Monate'} setzen, danach passt du einzelne Werte an.</p><button class="btn primary" data-act="suggest">Budgets vorschlagen</button></div>`);
  if(offLines.length) banners.push(`<div class="banner warn"><p><b>${offLines.length===1?'1 Budget passt':offLines.length+' Budgets passen'} nicht zu den tatsächlichen Beträgen</b> (Durchschnitt der letzten ${avgN} Monate): ${offLines.slice(0,4).map(l=>`${esc(l.name)} ${E0(l.budget)} → Ø ${E0(l.avg)}`).join(', ')}${offLines.length>4?' …':''}. Einzeln in der Tabelle unten übernehmen oder alle neu berechnen.</p><button class="btn" data-act="rebuild-budgets">Aus Ist neu berechnen</button></div>`);
  if(ov.report.uncategorized>0) banners.push(`<div class="banner warn"><p>${ov.report.uncategorized===1?'1 Umsatz':`${ov.report.uncategorized} Umsätze`} in ${monthLabel(S.month)} konnte ich keiner Kategorie zuordnen.</p><a class="btn" href="#umsaetze" data-act="show-uncat">Ansehen</a></div>`);

  // Group totals as bars: budget marker + actual
  const gbars=OUT.map(g=>{const b=t[g].budget,i=Math.max(0,t[g].ist),over=b>0&&i>b; const scale=Math.max(b,i,1);
    return `<div class="gbar"><div class="h"><span>${GROUPS[g].label}</span><span class="${over?'neg':''}">${E0(i)} / ${E0(b)}</span></div>
      <div class="track" title="${b>0?Math.round(i/b*100)+' % des Budgets':'kein Budget'}"><i style="width:${i/scale*100}%;background:var(${over?'--bad':GROUPS[g].color})"></i>${b>0&&i>b?`<span class="mark" style="left:${b/scale*100}%"></span>`:''}</div></div>`}).join('');
  const expLines = lines.filter(l=>OUT.includes(l.group) && l.ist>0).sort((a,b)=>b.ist-a.ist);
  const topMax = expLines[0]?.ist || 1;
  const tops = expLines.slice(0,5).map(l=>`<div><span>${esc(l.name)}</span><span class="track"><i style="width:${l.ist/topMax*100}%;background:var(--ink)"></i></span><em>${outIst>0?Math.round(l.ist/outIst*100):0} %</em></div>`).join('');

  const forecastCard = forecastHTML(ov);
  if(ov.period.mode==='calendar' && ov.accounts.length) banners.push(`<div class="banner"><p>Noch kein regelmäßiges Gehalt erkannt, deshalb gilt vorerst der Kalendermonat. Sobald zwei bis drei Gehaltseingänge da sind, stellt die App auf „Gehalt bis Gehalt“ um. Unter <a href="#konten">Konten</a> kannst du das Gehalt auch selbst festlegen.</p></div>`);

  // Hero: available amount and how the income was used
  const base = Math.max(t.income.ist, outIst, 1);
  const HCOL = {bills:'--h-bills', expenses:'--h-expenses', savings:'--h-savings', debts:'--h-debts'};
  const stack = OUT.map(g=>`<i style="width:${Math.max(0,t[g].ist)/base*100}%;background:var(${HCOL[g]})" title="${GROUPS[g].label} ${E(t[g].ist)}"></i>`).join('');
  const legend = OUT.map(g=>`<div><span><i style="background:var(${HCOL[g]})"></i>${GROUPS[g].label}</span><b>${E0(t[g].ist)}</b></div>`).join('');
  const past = ov.forecast?.past;
  const daysLeft = Math.max(0, Math.ceil((new Date(ov.period.end+'T23:59:59') - Date.now())/864e5));
  const pct = t.income.ist>0 ? Math.round(avail/t.income.ist*100) : 0;
  const chip = past ? '<span class="chip">Monat abgeschlossen</span>'
    : `<span class="chip${ov.forecast?.projected<0?' bad':''}">noch ${daysLeft} ${daysLeft===1?'Tag':'Tage'}</span>`;

  const ledgers = ['income',...OUT].map(g=>{
    const ls=lines.filter(l=>l.group===g);
    const rows=ls.map(l=>{const over=g!=='income'&&l.budget>0&&l.ist>l.budget; const pct=l.budget>0?Math.min(100,Math.max(0,l.ist)/l.budget*100):0;
      const off=offLines.includes(l), avgVal=Math.ceil(l.avg/1000)*1000;
      const avgHint = avgN>0 && l.avg>0 ? (off
        ? `<button class="avg-hint off" data-act="take-avg" data-cat="${l.id}" data-amount="${avgVal}" title="Budget auf den Durchschnitt setzen">Ø ${E0(l.avg)} · übernehmen</button>`
        : `<span class="avg-hint">Ø ${E0(l.avg)}</span>`) : '';
      return `<tr class="${off?'off-budget':''}"><td><span class="cat-name">${esc(l.name)}</span>${avgHint}</td>
        <td class="r"><input class="num" inputmode="decimal" data-budget="${l.id}" value="${l.budget?nf2.format(l.budget/100):''}" placeholder="0,00" aria-label="Budget ${esc(l.name)}"></td>
        <td class="r ${over?'over':''}">${l.count?`<button class="ist-link" data-cat="${l.id}">${E(l.ist)}</button>`:`<span class="muted">${E(0)}</span>`}</td>
        <td class="prog">${l.budget>0?`<div class="bar thin"><i style="width:${pct}%;background:var(${over?'--bad':GROUPS[g].color})"></i></div>`:''}</td></tr>`}).join('');
    return `<section class="ledger" style="--gc:var(${GROUPS[g].color})"><header><h2>${GROUPS[g].label}</h2><span class="sum"><b>${E(t[g].ist)}</b> / ${E(t[g].budget)}</span></header>
      <div class="tbl-scroll"><table><thead><tr><th>Kategorie</th><th class="r">Budget</th><th class="r">Ist</th><th></th></tr></thead><tbody>${rows}</tbody></table></div></section>`;
  }).join('');

  main.innerHTML = banners.join('') + `
  <section class="grid3">
    <div class="hero span2">
      <div class="hero-top"><div><div class="lbl">Verfügbar in diesem Monat</div>
        <div class="big ${avail<0?'neg':''}">${E0(avail)}</div>
        <div class="sub">von ${E0(t.income.ist)} Einnahmen${t.income.ist>0?` · ${pct} % übrig`:''} · geplant frei ${E0(plan)} · Sparquote ${quote} %</div></div>${chip}</div>
      <div class="stack" role="img" aria-label="Verwendung der Einnahmen">${stack}</div>
      <div class="hero-legend">${legend}</div>
    </div>
    ${forecastCard}
  </section>
  <section class="grid3">
    <div class="span2">${upcomingHTML(ov)}</div>
    <div class="card"><h3>Budget vs. Ist</h3><div class="gbars">${gbars}</div>
      <div class="divider"></div><h3>Größte Posten</h3><div class="tops">${tops||'<span class="muted">Keine Ausgaben</span>'}</div></div>
  </section>
  <div class="card"><div class="card-head"><h3>Letzte 12 Monate</h3><div class="legend2"><span><i style="background:var(--c-income)"></i>Einnahmen</span><span><i style="background:var(--accent)"></i>Ausgaben gesamt</span></div></div>${trendBars(ov.trend)}</div>
  <section class="ledgers">${ledgers}</section>
  <p class="note">Budgets gelten für jeden Budgetmonat. Umbuchungen zwischen euren eigenen Konten zählen weder als Einnahme noch als Ausgabe.${avgN>0?` Ø = Monatsdurchschnitt der letzten ${avgN} Budgetmonate, Jahres- und Quartalszahlungen anteilig. <button class="linkbtn" data-act="rebuild-budgets">Alle Budgets aus Ist neu berechnen</button>`:''}</p>`;
}

main.addEventListener('change', async e=>{
  const el=e.target;
  if(el.dataset.budget){
    const v=parseDE(el.value); if(v===null){ toast('Betrag nicht lesbar, z. B. 250 oder 49,90'); return; }
    try{ await api('PUT',`/api/categories/${el.dataset.budget}/budget`,{amount:v}); el.value=v?nf2.format(v/100):''; toast('Budget gespeichert',2000); viewMonat(); }catch(err){ toastError(err); }
  }
  if(el.dataset.txcat){
    const id=+el.dataset.txcat, cat=+el.value;
    try{ await api('PATCH',`/api/transactions/${id}`,{category_id:cat});
      const merchant=el.closest('tr').dataset.merchant;
      toast(`<span>Auf „${esc(catById(cat)?.name)}“ gesetzt.</span>${merchant?`<button class="btn small" data-act="rule" data-tx="${id}" data-cat="${cat}">Alle von ${esc(merchant)} so zuordnen</button>`:''}`, 9000);
      loadTx();
    }catch(err){ toastError(err); }
  }
  if(el.dataset.kind){ try{ await api('PATCH',`/api/recurring/${el.dataset.kind}`,{kind:el.value}); viewAbos(); }catch(err){ toastError(err); } }
  if(el.dataset.owner!==undefined && el.dataset.acc){
    const card=el.closest('[data-accid]'); const id=card.dataset.accid;
    try{ await api('PATCH',`/api/accounts/${id}`,{owner:card.querySelector('[data-owner]').value, display_name:card.querySelector('[data-dname]').value, active:card.querySelector('[data-active]').checked, book:card.querySelector('[data-book]').value}); 
      const moved = card.querySelector('[data-book]').value !== S.area;
      toast(moved?'Konto verschoben':'Konto gespeichert',2000); if(moved) viewKonten(S.area); }catch(err){ toastError(err); }
  }
  if(el.id==='nameA'||el.id==='nameB'){
    try{ await api('PUT','/api/settings',{name_a:$('#nameA').value,name_b:$('#nameB').value}); S.me.name_a=$('#nameA').value; S.me.name_b=$('#nameB').value; viewPaar(); }catch(err){ toastError(err); }
  }
  if(el.closest && el.closest('#txFilters')){ readTxFilters(); loadTx(); }
  if(el.id==='cNext'||el.id==='cCycle') updateNextHint();
  if(el.dataset.setting==='period'){
    const ids=[...document.querySelectorAll('[data-series]:checked')].map(c=>c.dataset.series).join(',');
    try{ await api('PUT','/api/settings',{period_mode:$('#pMode').value, salary_series:ids}); S.me=await api('GET','/api/me'); S.month=S.me.current_month; toast('Budgetmonat gespeichert',2500); viewKonten(); }catch(err){ toastError(err); }
  }
  if(el.dataset.setting==='names'){
    try{ await api('PUT','/api/settings',{own_names:el.value}); toast('Gespeichert, Umsätze neu zugeordnet',2500); }catch(err){ toastError(err); }
  }
  if(el.dataset.import){
    const file=el.files[0]; if(!file) return;
    const fd=new FormData(); fd.append('file',file);
    toast('Importiere …',0);
    try{
      const res=await fetch(`/api/accounts/${el.dataset.import}/import`,{method:'POST',body:fd,credentials:'same-origin'});
      const d=await res.json().catch(()=>({}));
      if(!res.ok) throw new Error(d.error||`Fehler ${res.status}`);
      toast(`${d.added} Umsätze importiert (${dmy(d.from)} bis ${dmy(d.to)}), ${d.skipped} schon vorhanden und übersprungen.`,8000);
      viewKonten(S.area);
    }catch(err){ toastError(err); }
    el.value='';
  }
});
main.addEventListener('submit', async e=>{
  if(e.target.id!=='contractForm') return;
  e.preventDefault();
  const amount=parseDE($('#cAmount').value);
  if(!amount){ toast('Betrag angeben, z. B. 612,00'); return; }
  try{
    await api('POST','/api/recurring',{label:$('#cLabel').value, amount:Math.abs(amount), cycle_days:+$('#cCycle').value, next_date:$('#cNext').value, kind:$('#cKind').value, category_id:+$('#cCat').value||0, account_id:+$('#cAcc').value||0});
    S.showContract=false; S.prefill=null; toast('Vertrag angelegt, erscheint jetzt in der Prognose',3500); viewAbos();
  }catch(err){ toastError(err); }
});

document.addEventListener('click', async e=>{
  const b=e.target.closest('[data-act],[data-cat].ist-link'); if(!b) return;
  const act=b.dataset.act;
  if(b.classList.contains('ist-link')){ S.txFilter={q:'',account:'',cat:String(b.dataset.cat),group:''}; location.hash='#umsaetze'; return; }
  if(act==='theme'){ try{ localStorage.setItem('hs-theme', b.dataset.theme); }catch{} applyTheme(); return; }
  try{
    if(act==='up-all'){ S.upAll=!S.upAll; viewMonat(); return; }
    if(act==='suggest'){ const r=await api('POST','/api/budgets/suggest'); toast(`${r.updated} Budgets gesetzt`,3000); S.cats=[]; route(); }
    if(act==='rebuild-budgets'){
      if(b.dataset.confirm!=='1'){ b.dataset.confirm='1'; b.textContent='Alle Budgets überschreiben?'; return; }
      const r=await api('POST','/api/budgets/suggest',{overwrite:true});
      toast(`${r.updated} Budgets neu berechnet (Durchschnitt ${r.months} Monate)`,3500); S.cats=[]; route();
    }
    if(act==='take-avg'){ await api('PUT',`/api/categories/${b.dataset.cat}/budget`,{amount:+b.dataset.amount}); toast('Budget übernommen',2000); viewMonat(); return; }
    if(act==='show-uncat'){ S.txFilter={q:'',account:'',cat:'uncat',group:''}; }
    if(act==='rule'){ await api('PATCH',`/api/transactions/${b.dataset.tx}`,{category_id:+b.dataset.cat, rule:true}); toast('Regel gespeichert, alle passenden Umsätze neu zugeordnet',4000); loadTx(); }
    if(act==='reset-tx'){ await api('PATCH',`/api/transactions/${b.dataset.tx}`,{reset:true}); toast('Wieder automatisch zugeordnet',2500); loadTx(); }
    if(act==='rec-status'){ const cur=b.dataset.cur; const next=cur===b.dataset.status?'detected':b.dataset.status; await api('PATCH',`/api/recurring/${b.dataset.id}`,{status:next}); viewAbos(); }
    if(act==='rec-tx'){ S.txFilter={q:'',account:'',cat:'',group:'',recurring:b.dataset.id}; S.month=''; location.hash='#umsaetze'; }
    if(act==='toggle-contract'){ S.showContract=!S.showContract; if(!S.showContract) S.prefill=null; viewAbos(); }
    if(act==='as-contract'){ const t=S.txCache.find(x=>x.id===+b.dataset.tx); if(!t) return;
      S.prefill={label:t.merchant||t.counterparty, amount:Math.abs(t.amount), date:t.date, category_id:t.category_id, account_id:t.account_id, kind:t.amount>0?'einkommen':'fixkosten'};
      S.showContract=true; location.hash='#abos'; }
    if(act==='del-contract'){ if(b.dataset.confirm!=='1'){ b.dataset.confirm='1'; b.textContent='?'; b.title='Nochmal klicken zum Löschen'; return; } await api('DELETE',`/api/recurring/${b.dataset.id}`); toast('Vertrag gelöscht',2500); viewAbos(); }
    if(act==='sync'){ const r=await api('POST','/api/sync'); toast(r.started?'Abruf gestartet …':'Abruf läuft bereits',3000); pollSync(); }
    if(act==='show-banks'){ S.showBanks=!S.showBanks; if(S.showBanks && !S.banks) S.banks=await api('GET','/api/banks'); viewKonten(S.area); }
    if(act==='connect'){ b.disabled=true; const r=await api('POST','/api/connections',{bank:b.dataset.bank,country:b.dataset.country,book:S.area}); location.href=r.url; }
    if(act==='disconnect'){ if(b.dataset.confirm!=='1'){ b.dataset.confirm='1'; b.textContent='Wirklich trennen?'; return; } await api('DELETE',`/api/connections/${b.dataset.id}`); toast('Verbindung getrennt',3000); viewKonten(S.area); }
    if(act==='del-rule'){ await api('DELETE',`/api/rules/${b.dataset.id}`); toast('Regel gelöscht',2500); viewKonten(); }
    if(act==='logout'){ await api('POST','/api/logout'); S.loggedIn=false; location.hash='#login'; route(); }
    if(act==='tx-month'){ S.month = S.month ? '' : curMonth(); route(); }
  }catch(err){ toastError(err); }
});


/* ---------- Period & forecast ---------- */
function showPeriod(p){
  if(!p) return;
  $('#pRange').textContent = `${dm(p.start)} – ${dm(p.end)}${p.end.slice(0,4)}${p.mode==='salary'?' · von Gehalt zu Gehalt':''}`;
  $('#pRange').title = p.mode==='salary' ? `Budgetmonat von Gehalt zu Gehalt${p.salary_series?` (${p.salary_series})`:''}` : 'Kalendermonat';
}
function upcomingHTML(ov){
  const items = [...(ov.upcoming || [])].sort((a,b)=>a.date.localeCompare(b.date));
  const accs = new Map(ov.accounts.map(a=>[a.id,a]));
  const sum = (arr,f)=>arr.filter(f).reduce((a,i)=>a+i.amount,0);
  const totalOut = -sum(items,i=>i.amount<0), totalIn = sum(items,i=>i.amount>0);
  const catOf = i => i.category_id&&catById(i.category_id) ? catById(i.category_id).name : (KINDS[i.kind]||i.kind);
  const accName = id => { const a=accs.get(id); return a ? (a.display_name||a.name) : 'Ohne Konto'; };
  const LIMIT=10, all=!!S.upAll;
  const rows = items.slice(0, all?items.length:LIMIT).map(i=>`<div class="up-row"><span class="d">${dm(i.date)}</span>
    <span class="t"><b>${esc(i.label)}</b> <span>· ${esc(catOf(i))}${i.manual?' · manuell':''}${i.status==='ueberfaellig'?' · <span class="neg">überfällig</span>':''}</span></span>
    <span class="a">${esc(accName(i.account_id))}</span>
    <span class="v tnum ${i.amount>0?'pos':''}">${E(i.amount)}</span></div>`).join('');
  // Per account: debits, credits and the balance afterwards
  const byAcc = new Map();
  for(const i of items){ const k=i.account_id||0; if(!byAcc.has(k)) byAcc.set(k,[]); byAcc.get(k).push(i); }
  const accCards = [...byAcc.entries()].map(([id,list])=>{
    const a=accs.get(id); const out=-sum(list,i=>i.amount<0), inn=sum(list,i=>i.amount>0);
    const bal=a&&a.balance!=null?a.balance:null; const after=bal!=null?bal+inn-out:null;
    return `<div class="up-acc2"><b>${esc(accName(id))}</b>
      <div class="row2"><span>Abbuchungen</span><span class="tnum">${E(-out)}</span></div>
      ${inn?`<div class="row2"><span>Eingänge</span><span class="tnum pos">${E(inn)}</span></div>`:''}
      ${after!=null?`<div class="row2"><span>Kontostand danach</span><b class="tnum ${after<0?'neg':''}">${E(after)}</b></div>`:''}</div>`;
  }).join('');
  return `<div class="card"><div class="card-head"><h3>Demnächst abgebucht</h3><span class="meta">nächste 30 Tage · alle Konten</span>
      ${items.length?`<span class="right"><span class="muted">Summe </span><b class="tnum">${E(-totalOut)}</b>${totalIn?` · <span class="pos tnum">${E(totalIn)}</span>`:''}</span>`:''}</div>
    ${items.length?`<div class="up-list">${rows}</div>${items.length>LIMIT?`<button class="linkbtn" data-act="up-all" style="margin-top:8px">${all?'Weniger anzeigen':`Alle ${items.length} Zahlungen anzeigen`}</button>`:''}<div class="up-accs">${accCards}</div>`
      :'<p class="muted">In den nächsten 30 Tagen ist nichts Wiederkehrendes fällig.</p>'}
    <p class="note">Aus erkannten Abos, Fixkosten, Krediten und von Hand angelegten Verträgen. <a href="#abos">Alle ansehen</a></p></div>`;
}
function forecastHTML(ov){
  const f=ov.forecast, k=f.open_by_kind||{};
  const nowFree=f.income_ist-f.out_ist;
  const line=(label,v,cls='')=>`<div><span>${label}</span><b class="tnum ${cls}">${v}</b></div>`;
  const open=f.items.filter(i=>i.status!=='bezahlt');
  const paid=f.items.filter(i=>i.status==='bezahlt');
  const item=i=>`<li><span class="date-chip">${dm(i.date)}</span><span>${esc(i.label)} <span class="muted small">· ${i.category_id&&catById(i.category_id)?esc(catById(i.category_id).name):(KINDS[i.kind]||i.kind)}</span></span><b class="tnum ${i.amount>0?'pos':''}">${E(i.amount)}</b></li>`;
  if(f.past) return `<div class="card"><h3>Abgeschlossen</h3><p class="muted" style="margin-top:0">Dieser Budgetmonat ist vorbei. Übrig geblieben: <b class="${nowFree<0?'neg':''}">${E(nowFree)}</b>.</p>
    ${paid.length?`<ul class="list">${paid.slice(0,8).map(item).join('')}</ul>${paid.length>8?`<p class="note">und ${paid.length-8} weitere wiederkehrende Zahlungen</p>`:''}`:''}</div>`;
  const other=(k.sparen||0)+(k.sonstiges||0)+(k.einkommen||0);
  return `<div class="card"><h3>Prognose bis ${dm(ov.period.end)}</h3>
    <div class="fc-rows">
      ${line('Jetzt frei', E(nowFree), nowFree<0?'neg':'')}
      ${f.open_in?line('+ erwartete Einnahmen', E(f.open_in), 'pos'):''}
      ${(k.fixkosten||0)+(k.kredit||0)?line('− Fixkosten &amp; Kredite', E(-((k.fixkosten||0)+(k.kredit||0)))):''}
      ${k.abo?line('− Abos', E(-k.abo)):''}
      ${other?line('− Sparen &amp; Sonstiges', E(-other)):''}
      ${f.budget_rest?line(`− variable Ausgaben <span class="muted small">(${f.budget_days===1?'noch 1 Tag':`noch ${f.budget_days} Tage`})</span>`, E(-f.budget_rest)):''}
    </div>
    <div class="fc-total"><span>Voraussichtlich frei</span><b class="tnum ${f.projected<0?'neg':''}">${E(f.projected)}</b></div>
    <p class="note">${open.length?`${open.length} wiederkehrende Zahlungen bis ${dm(ov.period.end)} noch offen.`:`Bis ${dm(ov.period.end)} ist nichts Wiederkehrendes mehr offen.`} Abos ${E(ov.abo_monthly)}, Fixkosten und Kredite ${E(ov.fixed_monthly)} pro Monat.</p></div>`;
}

/* ---------- Transactions ---------- */
function catOptions(sel, withEmpty){
  let o = withEmpty?`<option value="">Alle Kategorien</option>`:'';
  for(const g of Object.keys(GROUPS)){
    const cs=S.cats.filter(c=>c.group===g); if(!cs.length) continue;
    o+=`<optgroup label="${GROUPS[g].label}">${cs.map(c=>`<option value="${c.id}"${String(c.id)===String(sel)?' selected':''}>${esc(c.name)}</option>`).join('')}</optgroup>`;
  }
  return o;
}
function readTxFilters(){
  S.txFilter.q=$('#fq').value; S.txFilter.account=$('#facc').value; S.txFilter.cat=$('#fcat').value; S.txFilter.group=$('#fgroup').value; S.txFilter.recurring='';
}
let accountsCache=[];
async function viewUmsaetze(){
  accountsCache = (await api('GET','/api/accounts')) || [];
  const f=S.txFilter;
  main.innerHTML=`<section class="card" id="txFilters"><div class="filters">
      <div class="field"><label for="fq">Suche</label><input id="fq" type="search" placeholder="Händler, Verwendungszweck …" value="${esc(f.q)}"></div>
      <div class="field"><label for="facc">Konto</label><select id="facc"><option value="">Alle Konten</option>${accountsCache.map(a=>`<option value="${a.id}"${String(a.id)===f.account?' selected':''}>${esc(a.display_name||a.name)} · ${esc(a.bank)}</option>`).join('')}</select></div>
      <div class="field"><label for="fgroup">Bereich</label><select id="fgroup"><option value="">Alle Bereiche</option>${Object.entries(GROUPS).map(([k,g])=>`<option value="${k}"${k===f.group?' selected':''}>${g.label}</option>`).join('')}</select></div>
      <div class="field"><label for="fcat">Kategorie</label><select id="fcat">${catOptions(f.cat,true).replace('</option>',`</option><option value="uncat"${f.cat==='uncat'?' selected':''}>Nicht zugeordnet</option>`)}</select></div>
    </div><div class="row" style="margin-top:10px"><span class="note" style="margin:0" id="txRange">${S.month?`Zeitraum: Budgetmonat ${monthLabel(S.month)} (oben umschalten)`:'Zeitraum: alle Monate'}</span><button class="btn ghost small" data-act="tx-month">${S.month?'Alle Monate zeigen':'Nur aktuellen Monat'}</button></div></section>
    <section class="ledger"><header><h2>Umsätze</h2><span class="sum" id="txSum"></span></header><div class="tbl-scroll" id="txTable"><div class="empty">Lädt …</div></div></section>`;
  let tmr; $('#fq').addEventListener('input',()=>{clearTimeout(tmr);tmr=setTimeout(()=>{readTxFilters();loadTx();},300)});
  await loadTx();
}
async function loadTx(){
  if(S.tab!=='umsaetze') return;
  const f=S.txFilter, p=new URLSearchParams();
  if(S.month) p.set('month',S.month); if(f.q) p.set('q',f.q); if(f.account) p.set('account_id',f.account);
  if(f.cat==='uncat') p.set('uncategorized','1'); else if(f.cat) p.set('category_id',f.cat); if(f.group) p.set('group',f.group); if(f.recurring) p.set('recurring_id',f.recurring);
  p.set('limit','1000');
  const txs = await api('GET','/api/transactions?'+p);
  S.txCache = txs;
  if(S.tab!=='umsaetze' || !$('#txSum')) return; // user has navigated away in the meantime
  const inn=txs.filter(t=>t.amount>0&&t.group!=='transfer').reduce((a,t)=>a+t.amount,0), out=txs.filter(t=>t.amount<0&&t.group!=='transfer').reduce((a,t)=>a+t.amount,0);
  $('#txSum').innerHTML = `${txs.length} Umsätze · <span class="pos">${E(inn)}</span> / <b>${E(out)}</b>`;
  const srcLabel={manual:'Hand',rule:'Regel'};
  $('#txTable').innerHTML = txs.length ? `<table><thead><tr><th>Datum</th><th>Empfänger / Zweck</th><th>Konto</th><th>Kategorie</th><th class="r">Betrag</th></tr></thead><tbody>
    ${txs.map(t=>`<tr data-merchant="${esc(t.merchant)}">
      <td class="tnum">${dmy(t.date)}</td>
      <td><div class="tx-main"><b>${esc(t.merchant||t.counterparty||'–')}${t.recurring_id?' <span class="src" title="Wiederkehrende Zahlung">↻</span>':` <button class="btn ghost small" data-act="as-contract" data-tx="${t.id}" title="Als wiederkehrenden Vertrag anlegen">↻ Als Vertrag</button>`}</b><span class="muted small clip" title="${esc(t.remittance)}">${esc(t.remittance)}</span></div></td>
      <td class="small">${esc(t.account)}<br><span class="muted">${esc(nameOf(t.owner))}</span></td>
      <td><select class="inline" data-txcat="${t.id}" aria-label="Kategorie" title="${esc(t.reason)}">${catOptions(t.category_id,false)}</select>${srcLabel[t.source]?`<span class="src ${t.source}" title="${esc(t.reason)}">${srcLabel[t.source]}</span>`:''}${t.source==='manual'?` <button class="btn ghost small" data-act="reset-tx" data-tx="${t.id}" title="Wieder automatisch zuordnen">↺</button>${t.merchant?`<br><button class="btn small" style="margin-top:4px" data-act="rule" data-tx="${t.id}" data-cat="${t.category_id}">Für alle von ${esc(t.merchant)} übernehmen</button>`:''}`:''}</td>
      <td class="r tnum ${t.amount>0?'amt-in':'amt-out'}">${E(t.amount)}</td></tr>`).join('')}</tbody></table>`
    : `<div class="empty">Keine Umsätze für diese Auswahl.</div>`;
}

/* ---------- Subscriptions & fixed costs ---------- */
function nextFrom(dateStr, cycle){
  if(!dateStr) return '';
  let d=new Date(dateStr+'T00:00:00Z'); const t=new Date(); t.setUTCHours(0,0,0,0);
  const months={30:1,61:2,91:3,182:6,365:12}[cycle];
  for(let i=0;d<t&&i<520;i++){ if(months) d.setUTCMonth(d.getUTCMonth()+months); else d.setUTCDate(d.getUTCDate()+cycle); }
  return d.toISOString().slice(0,10);
}
function updateNextHint(){
  const n=$('#cNext'), c=$('#cCycle'), h=$('#cNextHint'); if(!n||!h) return;
  const nx=nextFrom(n.value,+c.value); h.textContent = nx && nx!==n.value ? `→ nächste Fälligkeit ${dmy(nx)}` : '';
}
async function viewAbos(){
  const [rec, accs] = await Promise.all([api('GET','/api/recurring'), api('GET','/api/accounts')]);
  S.accs = accs || [];
  const pf = S.prefill || {};
  const live = rec.filter(r=>!r.ended && r.status!=='ignored');
  const sumK = k => live.filter(r=>r.direction==='out'&&r.kind===k).reduce((a,r)=>a+r.monthly,0);
  const incomeM = live.filter(r=>r.direction==='in').reduce((a,r)=>a+r.monthly,0);
  const row = r => `<tr class="${r.status==='ignored'?'ignored':''} ${r.ended?'ended':''}">
      <td><b>${esc(r.label)}</b><br><span class="muted small">${r.category_id&&catById(r.category_id)?esc(catById(r.category_id).name)+' · ':''}${r.manual?'von Hand angelegt':`seit ${dmy(r.first_date)} · ${r.occurrences}× erkannt`}</span></td>
      <td><span class="cycle c${r.cycle_days}">${esc(r.cycle)}</span></td>
      <td class="r tnum">${E(r.amount)}</td>
      <td class="r tnum"><b>${E(r.monthly)}</b></td>
      <td class="tnum">${r.ended?`<span class="muted">zuletzt ${dmy(r.last_date)}</span>`:dmy(r.next_date)}</td>
      <td><select class="inline" data-kind="${r.id}" aria-label="Art">${Object.entries(KINDS).map(([k,l])=>`<option value="${k}"${k===r.kind?' selected':''}>${l}</option>`).join('')}</select></td>
      <td><div class="state-btns">
        <button class="icon-btn ${r.status==='confirmed'?'on':''}" data-act="rec-status" data-status="confirmed" data-cur="${r.status}" data-id="${r.id}" title="Bestätigen">✓</button>
        <button class="icon-btn ${r.status==='ignored'?'on':''}" data-act="rec-status" data-status="ignored" data-cur="${r.status}" data-id="${r.id}" title="Ignorieren (zählt nicht in Summen)">✕</button>
        ${r.manual?`<button class="icon-btn" data-act="del-contract" data-id="${r.id}" title="Vertrag löschen">🗑</button>`:`<button class="icon-btn" data-act="rec-tx" data-id="${r.id}" title="Zahlungen ansehen">≡</button>`}</div></td></tr>`;
  const table = (items, title, color, note) => items.length ? `<section class="ledger" style="--gc:var(${color})"><header><h2>${title}</h2><span class="sum"><b>${E(items.filter(r=>r.status!=='ignored').reduce((a,r)=>a+r.monthly,0))}</b> pro Monat${note?` · ${note}`:''}</span></header>
    <div class="tbl-scroll"><table><thead><tr><th>Zahlung</th><th>Rhythmus</th><th class="r">Betrag</th><th class="r">Monatlich</th><th>Nächste</th><th>Art</th><th></th></tr></thead><tbody>${items.map(row).join('')}</tbody></table></div></section>` : '';
  const act = rec.filter(r=>!r.ended);
  const by = k => act.filter(r=>r.direction==='out'&&r.kind===k);
  const ended = rec.filter(r=>r.ended);
  main.innerHTML = `<section class="stat-grid">
      <div class="card"><h3>Abos pro Monat</h3><div class="bignum">${E(sumK('abo'))}</div><p class="note">${E0(sumK('abo')*12)} im Jahr · ${by('abo').filter(r=>r.status!=='ignored').length} aktive Abos</p></div>
      <div class="card"><h3>Fixkosten pro Monat</h3><div class="bignum">${E(sumK('fixkosten'))}</div><p class="note">Energie, Versicherungen, Telefon & Co.</p></div>
      <div class="card"><h3>Kredite &amp; Sparen</h3><div class="bignum">${E(sumK('kredit')+sumK('sparen'))}</div><p class="note">Kredite ${E(sumK('kredit'))} · Sparen ${E(sumK('sparen'))}</p></div>
      <div class="card"><h3>Regelmäßige Einnahmen</h3><div class="bignum">${E(incomeM)}</div><p class="note">Gehalt, Kindergeld und andere feste Eingänge</p></div>
    </section>
    <section class="card" id="addContract">
      <div class="row"><h3 style="margin:0">Vertrag von Hand anlegen</h3><span class="spacer"></span><button class="btn small" data-act="toggle-contract">${S.showContract?'Schließen':'Hinzufügen'}</button></div>
      <p class="note" style="margin-top:6px">Für Zahlungen, die im Abruf noch fehlen, z. B. jährliche Versicherungen oder halbjährliche Beiträge. Als Datum reicht die letzte Abbuchung, den nächsten Termin rechnet die App aus. Am schnellsten geht es über „↻ Als Vertrag“ direkt in der Umsatzliste.</p>
      ${S.showContract?`<form id="contractForm" class="filters" style="margin-top:12px">
        <div class="field"><label for="cLabel">Name</label><input id="cLabel" required placeholder="z. B. Kfz-Versicherung" value="${esc(pf.label||'')}"></div>
        <div class="field"><label for="cAmount">Betrag €</label><input id="cAmount" inputmode="decimal" required placeholder="612,00" value="${pf.amount?nf2.format(pf.amount/100):''}"></div>
        <div class="field"><label for="cCycle">Rhythmus</label><select id="cCycle"><option value="30">Monatlich</option><option value="91">Vierteljährlich</option><option value="182">Halbjährlich</option><option value="365" selected>Jährlich</option></select></div>
        <div class="field"><label for="cNext">Letzte oder nächste Zahlung</label><input id="cNext" type="date" required value="${esc(pf.date||'')}"><span class="muted small" id="cNextHint"></span></div>
        <div class="field"><label for="cKind">Art</label><select id="cKind">${Object.entries(KINDS).map(([k,l])=>`<option value="${k}"${k===(pf.kind||'fixkosten')?' selected':''}>${l}</option>`).join('')}</select></div>
        <div class="field"><label for="cCat">Kategorie</label><select id="cCat"><option value="">–</option>${catOptions(pf.category_id||'',false)}</select></div>
        <div class="field"><label for="cAcc">Abbuchung von</label><select id="cAcc"><option value="">–</option>${(S.accs||[]).map(a=>`<option value="${a.id}"${a.id===pf.account_id?' selected':''}>${esc(a.display_name||a.name)} · ${esc(a.bank)}</option>`).join('')}</select></div>
        <div class="field" style="align-self:end"><button class="btn primary" type="submit">Speichern</button></div>
      </form>`:''}
    </section>
    <p class="note" style="margin:0">Erkannt anhand von Rhythmus und Betrag über die letzten zwei Jahre. Mit ✓ bestätigst du eine Zahlung, mit ✕ blendest du sie aus. Die Art lässt sich per Auswahl ändern und bleibt dann fest.</p>
    ${table(by('abo'),'Abos','--c-expenses','im Jahr '+E0(sumK('abo')*12))}
    ${table(by('fixkosten'),'Fixkosten','--c-bills')}
    ${table(by('kredit'),'Kredite','--c-debts')}
    ${table(by('sparen'),'Sparen','--c-savings')}
    ${table(act.filter(r=>r.direction==='in'),'Einnahmen','--c-income')}
    ${table(by('sonstiges'),'Sonstige Regelmäßigkeiten','--c-transfer','prüfen, ob etwas davon ein Abo ist')}
    ${ended.length?`<section class="ledger"><details class="ended"><summary>Beendet (${ended.length}): keine Zahlung mehr im erwarteten Rhythmus</summary>
      <div class="tbl-scroll"><table><thead><tr><th>Zahlung</th><th>Rhythmus</th><th class="r">Betrag</th><th class="r">Monatlich</th><th>Letzte</th><th>Art</th><th></th></tr></thead><tbody>${ended.map(row).join('')}</tbody></table></div></details></section>`:''}
    ${rec.length?'':'<div class="card empty">Noch keine wiederkehrenden Zahlungen erkannt. Dafür braucht es ein paar Monate Kontoumsätze.</div>'}`;
  updateNextHint();
  if(S.showContract && S.prefill) $('#addContract').scrollIntoView({block:'start'});
}

/* ---------- Couple split ---------- */
async function viewPaar(){
  const ov = S.overview && S.overview.report.month===S.month ? S.overview : (S.overview = await api('GET','/api/overview?month='+S.month));
  ov.accounts ||= []; ov.report.lines ||= [];
  showPeriod(ov.period);
  const lines=ov.report.lines, by=ov.report.income_by_owner;
  const A=by.A||0, B=by.B||0, C=by['']||0;
  const out=lines.filter(l=>OUT.includes(l.group)).reduce((a,l)=>a+l.ist,0);
  const joint=Math.max(0,out-C);
  const shA=A+B>0?A/(A+B):.5, shB=1-shA;
  const nA=esc(nameOf('A')), nB=esc(nameOf('B'));
  const money=v=>`<span class="${v<0?'neg':''}">${E(v)}</span>`;
  const cA=joint*shA, cB=joint*shB, h=joint/2;
  const unassigned = ov.accounts.filter(a=>a.owner==='' ).length===ov.accounts.length && ov.accounts.length>0;
  main.innerHTML=`${unassigned?`<div class="banner warn"><p>Noch kein Konto einer Person zugeordnet. Lege unter „Konten“ fest, wem welches Girokonto gehört. Gehälter werden dann der richtigen Person zugerechnet.</p><a class="btn" href="#konten">Konten zuordnen</a></div>`:''}
    <section class="card"><h3>Wer ist wer</h3><div class="names">
      <div class="field"><label for="nameA">Person A</label><input id="nameA" value="${nA}"></div>
      <div class="field"><label for="nameB">Person B</label><input id="nameB" value="${nB}"></div></div>
      <p class="note">Einnahmen zählen für die Person, der das Konto gehört. Einnahmen auf gemeinsamen Konten (z. B. Kindergeld) werden zuerst von den Kosten abgezogen.</p></section>
    <section class="paar-grid">
      <div class="card"><h3>Einkommensverhältnis ${monthLabel(S.month)}</h3>
        <div class="share-bar"><div style="flex:${shA||1e-4};background:var(--c-income)">${nA} ${Math.round(shA*100)} %</div><div style="flex:${shB||1e-4};background:var(--c-expenses)">${nB} ${Math.round(shB*100)} %</div></div>
        <table><tbody><tr><td>Einnahmen ${nA}</td><td class="r tnum">${E(A)}</td></tr><tr><td>Einnahmen ${nB}</td><td class="r tnum">${E(B)}</td></tr>
          <tr><td>Gemeinsame Einnahmen</td><td class="r tnum">${E(C)}</td></tr><tr><td>Haushaltskosten gesamt</td><td class="r tnum">${E(out)}</td></tr>
          <tr><td><b>Gemeinsam zu tragen</b></td><td class="r tnum"><b>${E(joint)}</b></td></tr></tbody></table></div>
      <div class="card"><h3>Vergleich der Aufteilung</h3><div class="tbl-scroll"><table>
        <thead><tr><th></th><th class="r">${nA}</th><th class="r">${nB}</th></tr></thead><tbody>
        <tr><td colspan="3"><b>Nach Einkommen</b></td></tr>
        <tr><td>Zahlt</td><td class="r tnum">${E(cA)}</td><td class="r tnum">${E(cB)}</td></tr>
        <tr><td>Bleibt für sich</td><td class="r tnum">${money(A-cA)}</td><td class="r tnum">${money(B-cB)}</td></tr>
        <tr><td colspan="3"><b>50/50</b></td></tr>
        <tr><td>Zahlt</td><td class="r tnum">${E(h)}</td><td class="r tnum">${E(h)}</td></tr>
        <tr><td>Bleibt für sich</td><td class="r tnum">${money(A-h)}</td><td class="r tnum">${money(B-h)}</td></tr></tbody></table></div>
        <p class="note">Bei 50/50 bleibt ${A>=B?nB:nA} ${E(Math.abs(A>=B?cB-h:cA-h))} weniger als bei der Aufteilung nach Einkommen.</p></div>
    </section>`;
}

/* ---------- Accounts ---------- */
async function viewKonten(book){
  book = book || 'haushalt';
  const isHH = book==='haushalt';
  let [accs, conns, rules, recs] = await Promise.all([api('GET','/api/accounts'), api('GET','/api/connections'), api('GET','/api/rules'), api('GET','/api/recurring')]);
  accs ||= []; conns ||= []; rules ||= []; recs ||= [];
  const accsAll = accs;
  accs = accsAll.filter(a=>(a.book||'haushalt')===book);
  conns = conns.filter(c=>(c.book||'haushalt')===book || accsAll.some(a=>a.connection_id===c.id && a.book===book));
  const otherCount = accsAll.length - accs.length;
  S.me = await api('GET','/api/me');
  const incomeSeries = recs.filter(r=>r.direction==='in');
  const params=new URLSearchParams(location.search); const bankMsg=params.get('bank');
  if(bankMsg) history.replaceState(null,'',location.pathname+location.hash);
  const msg = {ok:['','<b>Bank verbunden.</b> Die Umsätze werden jetzt abgerufen und zugeordnet, das dauert einen Moment.'], abgebrochen:['warn','Die Freigabe bei der Bank wurde abgebrochen.'], fehler:['bad','Die Freigabe hat nicht geklappt. Bitte erneut versuchen; Details stehen im Server-Log.']}[bankMsg];
  const statusText={active:'aktiv',expired:'abgelaufen',error:'Fehler',pending:'wartet auf Freigabe',revoked:'getrennt'};
  const banks = (S.banks||[]).filter(b=>!S.bankFilter || b.name.toLowerCase().includes(S.bankFilter.toLowerCase()));
  main.innerHTML = `${msg?`<div class="banner ${msg[0]}"><p>${msg[1]}</p></div>`:''}
    ${S.me.demo?'<div class="banner warn"><p><b>Demo-Modus:</b> Die Banken hier sind simuliert. Für echte Konten <code>HS_DEMO</code> ausschalten und Enable Banking einrichten (siehe README).</p></div>':''}
    <section class="card"><div class="row"><div><h3 style="margin:0">Bankverbindungen ${isHH?'Haushaltsbuch':'Hausverwaltung'}</h3></div><span class="spacer"></span>
      <button class="btn" data-act="sync" title="Ein Abruf von Hand zählt nicht zum Tageslimit der Bank, weil du dabei bist.">Jetzt abrufen</button><button class="btn primary" data-act="show-banks">${S.showBanks?'Schließen':'Bank verbinden'}</button></div>
      ${S.showBanks?`<div class="field" style="margin-top:12px"><label for="bankq">Bank suchen</label><input id="bankq" type="search" placeholder="z. B. Sparkasse, DKB, ING, Volksbank …" value="${esc(S.bankFilter)}"></div>
        <div class="bank-list" id="bankList">${bankButtons(banks)}</div>
        <p class="note">Du wirst zu deiner Bank weitergeleitet und bestätigst dort mit Login und TAN. Die Freigabe ist nur lesend und gilt je nach Bank 90 bis 180 Tage. Neue Konten landen im Bereich <b>${isHH?'Haushaltsbuch':'Hausverwaltung'}</b>.</p>`:''}
      <div class="tbl-scroll" style="margin-top:12px">${conns.filter(c=>c.status!=='pending').length?`<table><thead><tr><th>Bank</th><th>Status</th><th>Freigabe bis</th><th></th></tr></thead><tbody>
        ${conns.filter(c=>c.status!=='pending').map(c=>`<tr><td><b>${esc(c.bank)}</b></td><td><span class="dot ${c.status}"></span> ${statusText[c.status]||c.status}${c.last_error?`<br><span class="muted small">${esc(c.last_error)}</span>`:''}</td>
          <td class="tnum">${c.valid_until?dmy(c.valid_until.slice(0,10)):'–'}</td>
          <td class="r">${c.status!=='active'?`<button class="btn small" data-act="connect" data-bank="${esc(c.bank)}" data-country="${esc(c.country)}">Neu verbinden</button>`:`<button class="btn small" data-act="connect" data-bank="${esc(c.bank)}" data-country="${esc(c.country)}">Verlängern</button> <button class="btn ghost small danger" data-act="disconnect" data-id="${c.id}">Trennen</button>`}</td></tr>`).join('')}</tbody></table>`:'<p class="muted">Noch keine Bank verbunden.</p>'}</div></section>
    ${isHH?periodSettingsHTML(incomeSeries):''}
    ${accs.length?'':`<div class="card empty">Noch kein Konto im Bereich ${isHH?'Haushaltsbuch':'Hausverwaltung'}. Verbinde eine Bank oder verschiebe ein Konto über „Gehört zu“ hierher.</div>`}
    <section class="acc-grid">${accs.map(a=>`<div class="card acc" data-accid="${a.id}">
      <div class="top"><div><b>${esc(a.display_name||a.name)}</b><div class="muted small">${esc(a.bank)}</div><div class="iban">${esc((a.iban||'').replace(/(.{4})/g,'$1 ').trim())}</div></div>
        <div class="bal"><b class="${a.balance<0?'neg':''}">${a.balance!=null?E(a.balance):'–'}</b><div class="muted small">abgerufen ${ago(a.last_synced_at)}</div></div></div>
      ${a.sync_error?`<div class="banner bad"><p>${esc(a.sync_error)}</p></div>`:''}
      ${scheduleHTML(a)}
      <div class="row">
        <div class="field" style="flex:1 1 140px"${isHH?'':' hidden'}><label>Gehört</label><select data-owner data-acc="1"><option value="A"${a.owner==='A'?' selected':''}>${esc(nameOf('A'))}</option><option value="B"${a.owner==='B'?' selected':''}>${esc(nameOf('B'))}</option><option value=""${a.owner===''?' selected':''}>Gemeinsam</option></select></div>
        <div class="field" style="flex:2 1 180px"><label>Anzeigename</label><input data-dname data-acc="1" data-owner-skip value="${esc(a.display_name)}" placeholder="${esc(a.name)}"></div>
        <div class="field" style="flex:1 1 150px"><label>Bereich</label><select data-book data-acc="1" data-owner-skip><option value="haushalt"${a.book!=='verwaltung'?' selected':''}>Haushaltsbuch</option><option value="verwaltung"${a.book==='verwaltung'?' selected':''}>Hausverwaltung</option></select></div>
      </div>
      <label class="small"><input type="checkbox" data-active data-acc="1" data-owner-skip${a.active?' checked':''}> In Auswertungen einbeziehen und abrufen</label>
      <div class="row small"><label class="btn small" style="cursor:pointer">Ältere Umsätze importieren (CSV)<input type="file" accept=".csv,text/csv" data-import="${a.id}" hidden></label><span class="muted">Kontoauszug-Export aus dem Online-Banking</span></div></div>`).join('')}</section>
    ${otherCount?`<p class="note">${otherCount===1?'Ein weiteres Konto gehört':otherCount+' weitere Konten gehören'} zum Bereich ${isHH?'<a href="#hv-konten">Hausverwaltung</a>':'<a href="#konten">Haushaltsbuch</a>'}.</p>`:''}
    ${isHH?`<section class="ledger"><header><h2>Eigene Regeln</h2><span class="sum">entstehen, wenn du einen Umsatz umsortierst und „Alle … so zuordnen“ wählst</span></header>
      ${rules.length?`<div class="tbl-scroll"><table><thead><tr><th>Wenn</th><th>enthält</th><th>dann</th><th></th></tr></thead><tbody>${rules.map(r=>`<tr><td>${({merchant:'Händler',counterparty:'Empfänger',iban:'IBAN',remittance:'Verwendungszweck'})[r.field]}</td><td><b>${esc(r.pattern)}</b></td><td>${esc(S.cats.find(c=>c.id===r.category_id)?.name||r.category_slug)}</td><td class="r"><button class="btn ghost small danger" data-act="del-rule" data-id="${r.id}">Löschen</button></td></tr>`).join('')}</tbody></table></div>`:'<div class="empty">Noch keine eigenen Regeln.</div>'}</section>`:''}
    <section class="ledger"><header><h2>Erscheinungsbild</h2><span class="sum">Auto folgt der Einstellung deines Geräts. Die Wahl gilt für diesen Browser.</span></header><div class="theme-slot" style="max-width:340px;padding:4px 18px 18px">${themeSwitchHTML()}</div></section>
    ${S.me.auth_required?'<div class="row"><span class="spacer"></span><button class="btn ghost" data-act="logout">Abmelden</button></div>':''}`;
  const bq=$('#bankq'); if(bq){ bq.addEventListener('input',()=>{ S.bankFilter=bq.value; const f=(S.banks||[]).filter(b=>b.name.toLowerCase().includes(bq.value.toLowerCase())); $('#bankList').innerHTML=bankButtons(f); }); bq.focus(); }
  if(bankMsg==='ok') pollSync();
}
// Automatic sync plan of an account (bank request limits, see syncer.Schedule).
function scheduleHTML(a){
  const sc=a.schedule; if(!sc||!a.active||a.connection_status!=='active') return '';
  const when=iso=>{ const d=new Date(iso), t=new Date(); const hm=d.toLocaleTimeString('de-DE',{hour:'2-digit',minute:'2-digit'});
    return d.toDateString()===t.toDateString()?`heute ${hm}`:d.toDateString()===new Date(t.getTime()+864e5).toDateString()?`morgen ${hm}`:`${d.toLocaleDateString('de-DE',{day:'2-digit',month:'2-digit'})} ${hm}`; };
  const hours=Math.round(parseFloat(sc.interval)||0);
  const limit=`${sc.used} von ${sc.limit} automatischen Abrufen in 24 h${sc.learned?' (Limit dieser Bank, automatisch erkannt)':''}`;
  if(sc.limited_until && new Date(sc.limited_until)>new Date())
    return `<p class="small sched warn">Bank-Limit erreicht – automatischer Abruf wieder ab ${when(sc.limited_until)}. ${limit}.</p>`;
  return `<p class="small muted sched">Automatisch etwa alle ${hours} h${sc.next?`, nächster Abruf ${new Date(sc.next)<=new Date()?'in Kürze':when(sc.next)}`:''} · ${limit}.</p>`;
}
// Account fields: data-owner marks the owner select; the other fields trigger the same save.
main.addEventListener('change', e=>{ if(e.target.dataset.acc && e.target.dataset.ownerSkip!==undefined){ const sel=e.target.closest('[data-accid]').querySelector('[data-owner]'); sel.dispatchEvent(new Event('change',{bubbles:true})); } });

function periodSettingsHTML(series){
  const mode=S.me.period_mode||'salary';
  const sel=new Set(String(S.me.salary_series||'').split(',').filter(Boolean));
  const auto=sel.size===0;
  const list=series.filter(r=>!r.ended||sel.has(String(r.id))).sort((a,b)=>b.amount-a.amount);
  return `<section class="card"><h3>Budgetmonat</h3>
    <div class="filters" style="grid-template-columns:1fr 2fr">
      <div class="field"><label for="pMode">Ein Monat läuft</label><select id="pMode" data-setting="period">
        <option value="salary"${mode==='salary'?' selected':''}>von Gehalt zu Gehalt</option>
        <option value="calendar"${mode==='calendar'?' selected':''}>vom 1. bis Monatsende</option></select></div>
      <div class="field"><label for="ownNames">Weitere eigene Namen</label><input id="ownNames" data-setting="names" value="${esc(S.me.own_names||'')}" placeholder="z. B. Max Mustermann, Erika Mustermann"></div>
    </div>
    ${mode==='salary'?`<fieldset class="salary-pick"><legend>Welche Gehälter beginnen einen neuen Monat?</legend>
      <p class="note">${auto
        ?`Automatisch: der erste große Zahlungseingang (mind. 30&nbsp;% eures üblichen Monatseinkommens) zwischen dem 22. und dem 5.${S.me.salary_series_name?` Zuletzt erkannt: ${esc(S.me.salary_series_name)}.`:''} Wähle Gehälter aus, um das genau festzulegen.`
        :'Der Monat beginnt mit dem ersten der ausgewählten Gehälter. Keine Auswahl = automatisch.'}</p>
      ${list.length?list.map(r=>`<label class="small"><input type="checkbox" data-setting="period" data-series="${r.id}"${sel.has(String(r.id))?' checked':''}> ${esc(r.label)} · ${E(r.amount)} · ${esc(r.cycle)}${r.kind==='einkommen'?'':` <span class="muted">(${esc(r.kind)})</span>`}</label>`).join(''):'<p class="muted small">Noch keine regelmäßigen Eingänge erkannt.</p>'}
    </fieldset>`:''}
    <p class="note">Ein Budgetmonat heißt nach dem Monat, für den das Geld gedacht ist: Gehalt am 28.09. → Budgetmonat Oktober. Solange ein fälliges Gehalt noch nicht gebucht ist, läuft der alte Monat weiter (höchstens 7 Tage). Überweisungen auf eigene Namen zu nicht verbundenen Konten zählen als „Sparen“.</p></section>`;
}
function bankButtons(list){
  if(!list.length) return '<div class="empty">Keine Bank gefunden.</div>';
  return list.slice(0,200).map(b=>`<button data-act="connect" data-bank="${esc(b.name)}" data-country="${esc(b.country)}">${b.logo?`<img src="${esc(b.logo)}" alt="" loading="lazy">`:''}<span>${esc(b.name)}${b.beta?' <span class="src">Beta</span>':''}</span><span class="spacer"></span><span class="muted small">${b.consent_days?b.consent_days+' Tage':''}</span></button>`).join('');
}

/* ---------- Sync-Status ---------- */
let pollTimer;
async function updateSyncState(){
  try{
    const s=await api('GET','/api/sync');
    const probs=(s.problems||[]).filter(p=>!p.book||p.book===S.area).length;
    const nextTxt = S.nextSync && new Date(S.nextSync)>new Date() ? ` · nächster ${new Date(S.nextSync).toLocaleTimeString('de-DE',{hour:'2-digit',minute:'2-digit'})}` : '';
    const el=$('#syncState');
    el.textContent = s.running ? 'Abruf läuft …' : (s.last_finish && !s.last_finish.startsWith('0001') ? `Abgerufen ${ago(s.last_finish)}${nextTxt}` : '') + (probs?` · ${probs} Problem${probs>1?'e':''}`:'');
    el.classList.toggle('running', !!s.running); el.classList.toggle('problem', probs>0);
    return s;
  }catch{ return null; }
}
async function pollSync(){
  clearTimeout(pollTimer);
  const s=await updateSyncState();
  if(s&&s.running){ pollTimer=setTimeout(pollSync,1500); }
  else if(s){ if(s.new_transactions) toast(`${s.new_transactions} neue Umsätze abgerufen`,3500); S.cats=[]; route(); }
}

route();
