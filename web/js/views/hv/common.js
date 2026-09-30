// Shared parts of the property management pages.
import { esc } from '../../core/format.js';
import { get } from '../../core/api.js';
import { HV } from '../../core/state.js';

/** Rent ledger status -> [label, style]. */
const RENT_STATUS = {
  bezahlt: ['Bezahlt', 'ok'],
  teilweise: ['Teilweise', 'warn'],
  offen: ['Offen', 'bad'],
  faellig: ['Fällig', 'muted'],
};

/** Allocation keys of the utility settlement. */
export const ALLOCATION_KEYS = { flaeche: 'Fläche', personen: 'Personen', einheiten: 'Einheiten' };

export const RENT_TYPES = [
  ['fest', 'Feste Miete'],
  ['staffel', 'Staffelmiete'],
  ['index', 'Indexmiete'],
];
export const RENT_TYPE_SHORT = { fest: 'Fest', staffel: 'Staffel', index: 'Index' };
export const DEFAULT_DUE_DAY = 3;
export const MAX_DUE_DAY = 28;

/** Deadline kinds (see hv.Deadline*). */
export const DEADLINE_KINDS = {
  nk: 'Nebenkosten',
  staffel: 'Mietänderung',
  erhoehung: 'Mieterhöhung',
  index: 'Index',
  ende: 'Mietende',
  kaution: 'Kaution',
  rueckstand: 'Rückstand',
  manuell: 'Wiedervorlage',
};

/** Cost types, properties and leases; force reloads after changes. */
export async function loadMeta(force) {
  if (!HV.meta || force) HV.meta = await get('/api/hv/meta');
  return HV.meta;
}

export const costTypeName = slug => HV.meta?.cost_types?.find(c => c.slug === slug)?.name || slug || '–';

export function pill(status) {
  const [label, style] = RENT_STATUS[status] || [status, 'muted'];
  return `<span class="st st-${style}">${esc(label)}</span>`;
}

/** Year switcher: ‹ 2025  2026  2027 › */
export const yearSwitch = (year, action) =>
  `<button class="btn small" data-act="${action}" data-step="-1">‹ ${year - 1}</button><b>${year}</b>` +
  `<button class="btn small" data-act="${action}" data-step="1">${year + 1} ›</button>`;

/** Report of a year (income and costs per property, deadlines). */
export const loadReport = year => get('/api/hv/report?year=' + year);
