// Values shared with the server (see internal/domain) and fixed UI settings.

/** Category groups with label and CSS color token. */
export const GROUPS = {
  income: { label: 'Einnahmen', color: '--c-income' },
  bills: { label: 'Fixkosten', color: '--c-bills' },
  expenses: { label: 'Ausgaben', color: '--c-expenses' },
  savings: { label: 'Sparen', color: '--c-savings' },
  debts: { label: 'Schulden', color: '--c-debts' },
  transfer: { label: 'Umbuchungen', color: '--c-transfer' },
};

/** Groups that count as money going out. */
export const OUT_GROUPS = ['bills', 'expenses', 'savings', 'debts'];

/** Groups planned from the recognized contracts (Soll); variable spending has budgets. */
export const CONTRACT_GROUPS = ['income', 'bills', 'savings', 'debts'];

/** Order of the tables on the month page: the spending you control first. */
export const LEDGER_ORDER = ['expenses', 'bills', 'debts', 'savings', 'income'];

/** Colors of the spending stack on the dark hero card. */
export const HERO_COLORS = {
  bills: '--h-bills',
  expenses: '--h-expenses',
  savings: '--h-savings',
  debts: '--h-debts',
};

/** Kinds of recurring payments. */
export const KINDS = {
  abo: 'Abo',
  fixkosten: 'Fixkosten',
  kredit: 'Kredit',
  sparen: 'Sparen',
  einkommen: 'Einkommen',
  sonstiges: 'Sonstiges',
};

/** Payment cycles offered when a contract is added by hand (days). */
export const CYCLES = [
  [30, 'Monatlich'],
  [91, 'Vierteljährlich'],
  [182, 'Halbjährlich'],
  [365, 'Jährlich'],
];
export const DEFAULT_CYCLE = 365;

/** Calendar months per cycle; other cycles are counted in days. */
export const CYCLE_MONTHS = { 30: 1, 61: 2, 91: 3, 182: 6, 365: 12 };

export const MONTHS = [
  'Januar',
  'Februar',
  'März',
  'April',
  'Mai',
  'Juni',
  'Juli',
  'August',
  'September',
  'Oktober',
  'November',
  'Dezember',
];

/** Categories the classifier falls back to; a transaction there counts as not assigned until confirmed. */
export const CATCH_ALL_SLUGS = ['sonstiges', 'einnahmen-sonst'];

/** Filter value in the category select for transactions nobody has placed yet. */
export const UNCATEGORIZED = 'uncat';

/** Who set a category (shown as a small badge). */
export const SOURCE_LABELS = { manual: 'Hand', rule: 'Regel' };

export const RULE_FIELDS = {
  merchant: 'Empfänger',
  counterparty: 'Empfänger laut Bank',
  iban: 'IBAN',
  remittance: 'Verwendungszweck',
};

export const CONNECTION_STATUS = {
  active: 'aktiv',
  expired: 'abgelaufen',
  error: 'Fehler',
  pending: 'wartet auf Freigabe',
  revoked: 'getrennt',
};

/** Messages after returning from the bank (?bank=...): [banner style, text]. */
export const BANK_RESULTS = {
  ok: ['', '<b>Bank verbunden.</b> Die Umsätze werden jetzt abgerufen und zugeordnet, das dauert einen Moment.'],
  abgebrochen: ['warn', 'Die Freigabe bei der Bank wurde abgebrochen.'],
  fehler: ['bad', 'Die Freigabe hat nicht geklappt. Bitte erneut versuchen; Details stehen im Server-Log.'],
};

/** The two books (areas) of the app. */
export const BOOK_HOUSEHOLD = 'haushalt';
export const BOOK_PROPERTY = 'verwaltung';

/** Owners of an account for the couple split ('' = joint). */
export const OWNER_A = 'A';
export const OWNER_B = 'B';
export const OWNER_JOINT = '';

export const PERIOD_SALARY = 'salary';
export const PERIOD_CALENDAR = 'calendar';

/** Thresholds of the budget page. */
export const BUDGET = {
  // A category needs a budget if it has at least this average (cents) ...
  minAverageForBudget: 2000,
  // ... and a budget is off if it differs from the average by more than 30 % and 50 €.
  offShare: 0.3,
  offMinCents: 5000,
  // Averages are suggested rounded up to full 10 €.
  roundCents: 1000,
  // Upcoming payments shown before "show all".
  upcomingLimit: 10,
  // Paid recurring payments listed on a closed month.
  paidLimit: 8,
  // Warn this many days before a bank consent expires.
  consentWarnDays: 14,
  // Height of the trend bars in pixels.
  trendBarHeight: 118,
};

export const DAY_MS = 864e5;

/** Delays (ms). */
export const TIMING = {
  searchDebounce: 300,
  syncPoll: 1500,
  toast: 5000,
  toastShort: 2500, // "gespeichert"
  toastMedium: 3500, // result with a number
  toastLong: 9000, // message with a follow-up button
  toastError: 7000,
};

/** Mobile layout breakpoint (matches app.css). */
export const MOBILE_MAX_WIDTH = 900;

/** Upper limits for lists. */
export const LIMITS = { transactions: 1000, banks: 200 };

/** localStorage key of the theme choice. */
export const THEME_KEY = 'hs-theme';
