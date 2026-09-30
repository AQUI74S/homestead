-- Bank connections (one per consent granted at a bank)
CREATE TABLE bank_connections (
    id            BIGSERIAL PRIMARY KEY,
    aspsp_name    TEXT NOT NULL,
    aspsp_country TEXT NOT NULL,
    auth_state    TEXT UNIQUE,
    session_id    TEXT UNIQUE,
    valid_until   TIMESTAMPTZ,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','expired','error','revoked')),
    last_error    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
    id             BIGSERIAL PRIMARY KEY,
    connection_id  BIGINT REFERENCES bank_connections(id) ON DELETE SET NULL,
    provider_uid   TEXT NOT NULL UNIQUE,
    iban           TEXT NOT NULL DEFAULT '',
    bank_name      TEXT NOT NULL DEFAULT '',
    name           TEXT NOT NULL DEFAULT '',
    display_name   TEXT NOT NULL DEFAULT '',
    currency       TEXT NOT NULL DEFAULT 'EUR',
    owner          TEXT NOT NULL DEFAULT '' CHECK (owner IN ('A','B','')),
    balance        NUMERIC(14,2),
    balance_at     TIMESTAMPTZ,
    last_synced_at TIMESTAMPTZ,
    sync_error     TEXT NOT NULL DEFAULT '',
    active         BOOLEAN NOT NULL DEFAULT TRUE
);

-- grp: income | bills | expenses | savings | debts | transfer
CREATE TABLE categories (
    id      BIGSERIAL PRIMARY KEY,
    grp     TEXT NOT NULL CHECK (grp IN ('income','bills','expenses','savings','debts','transfer')),
    slug    TEXT NOT NULL UNIQUE,
    name    TEXT NOT NULL,
    budget  NUMERIC(14,2) NOT NULL DEFAULT 0,
    sort    INT NOT NULL DEFAULT 0
);

CREATE TABLE recurring (
    id           BIGSERIAL PRIMARY KEY,
    merchant_key TEXT NOT NULL,
    direction    TEXT NOT NULL CHECK (direction IN ('in','out')),
    label        TEXT NOT NULL,
    cycle_days   INT NOT NULL,
    avg_amount   NUMERIC(14,2) NOT NULL,
    last_amount  NUMERIC(14,2) NOT NULL,
    first_date   DATE NOT NULL,
    last_date    DATE NOT NULL,
    next_date    DATE NOT NULL,
    occurrences  INT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('abo','fixkosten','einkommen','kredit','sparen','sonstiges')),
    kind_locked  BOOLEAN NOT NULL DEFAULT FALSE,
    category_id  BIGINT REFERENCES categories(id) ON DELETE SET NULL,
    status       TEXT NOT NULL DEFAULT 'detected' CHECK (status IN ('detected','confirmed','ignored')),
    ended        BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (merchant_key, direction)
);

CREATE TABLE transactions (
    id                BIGSERIAL PRIMARY KEY,
    account_id        BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    ext_id            TEXT NOT NULL,
    booking_date      DATE NOT NULL,
    value_date        DATE,
    amount            NUMERIC(14,2) NOT NULL,          -- negative = expense
    currency          TEXT NOT NULL DEFAULT 'EUR',
    counterparty      TEXT NOT NULL DEFAULT '',
    counterparty_iban TEXT NOT NULL DEFAULT '',
    remittance        TEXT NOT NULL DEFAULT '',
    bank_code         TEXT NOT NULL DEFAULT '',
    merchant          TEXT NOT NULL DEFAULT '',        -- readable merchant name
    merchant_key      TEXT NOT NULL DEFAULT '',        -- normalized key
    category_id       BIGINT REFERENCES categories(id) ON DELETE SET NULL,
    category_source   TEXT NOT NULL DEFAULT 'auto' CHECK (category_source IN ('auto','rule','manual')),
    reason            TEXT NOT NULL DEFAULT '',        -- why it was categorized this way
    recurring_id      BIGINT REFERENCES recurring(id) ON DELETE SET NULL,
    note              TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, ext_id)
);
CREATE INDEX transactions_booking_date_idx ON transactions (booking_date);
CREATE INDEX transactions_merchant_key_idx ON transactions (merchant_key);

-- Custom rules: pattern is a lowercase substring
CREATE TABLE rules (
    id          BIGSERIAL PRIMARY KEY,
    field       TEXT NOT NULL CHECK (field IN ('merchant','counterparty','iban','remittance')),
    pattern     TEXT NOT NULL,
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (field, pattern)
);

-- Budget per month; if there is no entry, categories.budget applies
CREATE TABLE budgets (
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    month       TEXT NOT NULL,
    amount      NUMERIC(14,2) NOT NULL,
    PRIMARY KEY (category_id, month)
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO settings (key, value) VALUES ('name_a', 'Person A'), ('name_b', 'Person B');

INSERT INTO categories (grp, slug, name, sort) VALUES
 ('income',  'gehalt',            'Gehalt & Lohn',               10),
 ('income',  'kindergeld',        'Kindergeld',                  20),
 ('income',  'erstattung',        'Erstattungen',                30),
 ('income',  'einnahmen-sonst',   'Sonstige Einnahmen',          90),
 ('bills',   'wohnen',            'Miete / Hauskredit',          10),
 ('bills',   'strom',             'Strom',                       20),
 ('bills',   'gas',               'Gas & Heizung',               30),
 ('bills',   'wasser',            'Wasser & Abwasser',           40),
 ('bills',   'internet',          'Internet & Festnetz',         50),
 ('bills',   'mobilfunk',         'Mobilfunk',                   60),
 ('bills',   'rundfunk',          'Rundfunkbeitrag',             70),
 ('bills',   'versicherung',      'Versicherungen',              80),
 ('bills',   'abos',              'Abos & Streaming',            90),
 ('bills',   'mitgliedschaft',    'Mitgliedschaften & Vereine', 100),
 ('bills',   'kita-schule',       'Kita & Schule',              110),
 ('bills',   'steuern',           'Steuern & Gebühren',         120),
 ('expenses','lebensmittel',      'Lebensmittel',                10),
 ('expenses','drogerie',          'Drogerie & Haushalt',         20),
 ('expenses','essen-gehen',       'Essen gehen & Lieferdienst',  30),
 ('expenses','tanken',            'Tanken & Laden',              40),
 ('expenses','mobilitaet',        'Mobilität',                   50),
 ('expenses','online-shopping',   'Online-Shopping',             60),
 ('expenses','kleidung',          'Kleidung',                    70),
 ('expenses','haus-garten',       'Haus, Garten & Baumarkt',     80),
 ('expenses','gesundheit',        'Gesundheit & Apotheke',       90),
 ('expenses','freizeit',          'Freizeit & Hobby',           100),
 ('expenses','kinder',            'Kinder',                     110),
 ('expenses','geschenke',         'Geschenke & Spenden',        120),
 ('expenses','bargeld',           'Bargeld',                    130),
 ('expenses','sonstiges',         'Sonstiges',                  190),
 ('savings', 'sparen',            'Sparen & Anlegen',            10),
 ('debts',   'kredite',           'Kredite & Raten',             10),
 ('debts',   'kreditkarte',       'Kreditkarte',                 20),
 ('transfer','umbuchung',         'Umbuchung eigene Konten',     10);
