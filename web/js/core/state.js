// Application state. Views read and change it, then re-render.
import { BOOK_HOUSEHOLD, OWNER_A, OWNER_B } from './constants.js';
import { calendarMonth } from './format.js';

export const emptyTxFilter = () => ({ q: '', account: '', cat: '', group: '', recurring: '' });

/** State of the household budget and the app shell. */
export const S = {
  tab: 'monat',
  area: BOOK_HOUSEHOLD,
  lastTab: { haushalt: 'monat', verwaltung: 'hv' },
  month: calendarMonth(), // budget month "YYYY-MM"; '' = all months (transactions)
  monthInit: false,
  me: null, // /api/me
  loggedIn: false,
  cats: [], // categories
  overview: null, // last /api/overview
  nextSync: null,
  // transactions
  txFilter: emptyTxFilter(),
  txCache: [],
  // recurring payments
  upAll: false,
  showContract: false,
  prefill: null,
  accs: [],
  // accounts
  banks: null,
  bankFilter: '',
  showBanks: false,
};

const thisYear = new Date().getFullYear();

/** State of the property management. */
export const HV = {
  month: calendarMonth(),
  year: thisYear, // properties: report year
  nkYear: thisYear - 1, // utility settlement: the previous year is due
  txYear: thisYear, // rent account
  txOpen: false,
  meta: null, // /api/hv/meta
  lease: null, // open lease detail
  edit: null, // lease in the form
  nkProp: 0,
};

export const catById = id => S.cats.find(c => c.id === id);

/** Name of an account owner ('' = joint). */
export const nameOf = owner =>
  owner === OWNER_A ? S.me?.name_a || 'Person A' : owner === OWNER_B ? S.me?.name_b || 'Person B' : 'Gemeinsam';

/** The current budget month as the server sees it (salary to salary). */
export const currentMonth = () => S.me?.current_month || calendarMonth();

/** Display name of an account. */
export const accountName = a => a.display_name || a.name;
