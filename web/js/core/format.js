// Formatting of amounts, dates and HTML text.
import { MONTHS } from './constants.js';

const eur = new Intl.NumberFormat('de-DE', { style: 'currency', currency: 'EUR' });
const eur0 = new Intl.NumberFormat('de-DE', { style: 'currency', currency: 'EUR', maximumFractionDigits: 0 });
export const nf2 = new Intl.NumberFormat('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** Cents as euros with cents, e.g. "1.234,56 €". */
export const E = cents => eur.format((cents || 0) / 100);
/** Cents as whole euros, e.g. "1.235 €". */
export const E0 = cents => eur0.format((cents || 0) / 100);
/** Whole euros with a sign, e.g. "+25 €" / "−130 €". */
export const signedE0 = cents => (cents > 0 ? '+' : '') + E0(cents);
/** Cents for an input field ("" for zero). */
export const amountInput = cents => (cents ? nf2.format(cents / 100) : '');
/** Area in 1/100 m². */
export const m2 = c => nf2.format((c || 0) / 100).replace(/,00$/, '') + ' m²';

const HTML_ESCAPES = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
/** Escapes text for use in HTML. */
export const esc = s => String(s ?? '').replace(/[&<>"']/g, c => HTML_ESCAPES[c]);

/** Parses a German amount ("1.234,56 €", "49,90", "250") into cents; null if unreadable, 0 if empty. */
export function parseDE(s) {
  s = String(s ?? '').replace(/[€\s]/g, '');
  if (!s) return 0;
  if (s.includes(',')) s = s.replace(/\./g, '').replace(',', '.');
  const n = parseFloat(s);
  return isFinite(n) ? Math.round(n * 100) : null;
}
/** Like parseDE, but 0 for unreadable input. */
export const cents = v => parseDE(v) ?? 0;

/** "2026-09-30" -> "30.09.2026" */
export const dmy = iso => (iso ? iso.split('-').reverse().join('.') : '');
/** "2026-09-30" -> "30.09." */
export const dm = iso => (iso ? `${iso.slice(8, 10)}.${iso.slice(5, 7)}.` : '');

/** "2026-10" -> "Oktober 2026" */
export function monthLabel(id) {
  const [y, m] = id.split('-');
  return `${MONTHS[+m - 1]} ${y}`;
}
/** Short month name of "2026-10" -> "Okt" */
export const monthShort = id => MONTHS[+id.slice(5) - 1].slice(0, 3);

const monthId = (y, m) => `${y}-${String(m).padStart(2, '0')}`;

/** "2026-10" moved by d months. */
export function shiftMonth(id, d) {
  let [y, m] = id.split('-').map(Number);
  m += d;
  while (m < 1) {
    m += 12;
    y--;
  }
  while (m > 12) {
    m -= 12;
    y++;
  }
  return monthId(y, m);
}
/** Current calendar month as "YYYY-MM". */
export const calendarMonth = (d = new Date()) => monthId(d.getFullYear(), d.getMonth() + 1);
/** Today as "YYYY-MM-DD" (UTC). */
export const todayISO = () => new Date().toISOString().slice(0, 10);
/** The day before an ISO date: "2026-11-01" -> "2026-10-31". */
export const dayBefore = iso => new Date(Date.parse(iso + 'T00:00:00Z') - 86400000).toISOString().slice(0, 10);

/** Relative time, e.g. "vor 5 Min." */
export function ago(iso) {
  if (!iso) return 'noch nie';
  const s = (Date.now() - new Date(iso)) / 1000;
  if (s < 90) return 'gerade eben';
  if (s < 3600) return `vor ${Math.round(s / 60)} Min.`;
  if (s < 86400) return `vor ${Math.round(s / 3600)} Std.`;
  return `vor ${Math.round(s / 86400)} Tagen`;
}

/** Local day and time of a timestamp, e.g. "30.09., 21:04". */
export const stamp = iso => {
  const d = new Date(iso);
  return `${d.toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit' })}, ${hm(d)}`;
};

/** Clock time "14:05". */
export const hm = d => new Date(d).toLocaleTimeString('de-DE', { hour: '2-digit', minute: '2-digit' });

/** Singular or plural: plural(1, 'Tag', 'Tage') -> "1 Tag". */
export const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** Percent with a comma: 5.25 -> "5,25". */
export const pct2 = v => v.toFixed(2).replace('.', ',');

/** Initials of a name: "Anna Schmidt" -> "AS". */
export const initials = name =>
  String(name || '?')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map(w => w[0].toUpperCase())
    .join('');

/** Groups an IBAN in blocks of four. */
export const ibanBlocks = iban => (iban || '').replace(/(.{4})/g, '$1 ').trim();
