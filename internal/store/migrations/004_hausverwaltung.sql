-- Books: every account belongs to either the Haushaltsbuch (household) or the Hausverwaltung (property management)
ALTER TABLE accounts ADD COLUMN book TEXT NOT NULL DEFAULT 'haushalt' CHECK (book IN ('haushalt','verwaltung'));

-- Money between Mietkonto (rent account) and private account
INSERT INTO categories (grp, slug, name, sort) VALUES
 ('income',   'vermietung',         'Einnahmen aus Vermietung', 25),
 ('expenses', 'zuschuss-vermietung', 'Zuschuss an Vermietung',  150)
ON CONFLICT (slug) DO NOTHING;

CREATE TABLE properties (
    id             BIGSERIAL PRIMARY KEY,
    name           TEXT NOT NULL,
    address        TEXT NOT NULL DEFAULT '',
    purchase_price NUMERIC(14,2),
    notes          TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE units (
    id          BIGSERIAL PRIMARY KEY,
    property_id BIGINT NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    area_m2     NUMERIC(8,2) NOT NULL DEFAULT 0,
    notes       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE tenants (
    id     BIGSERIAL PRIMARY KEY,
    name   TEXT NOT NULL,
    email  TEXT NOT NULL DEFAULT '',
    phone  TEXT NOT NULL DEFAULT '',
    iban   TEXT NOT NULL DEFAULT '',
    notes  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE leases (
    id            BIGSERIAL PRIMARY KEY,
    unit_id       BIGINT NOT NULL REFERENCES units(id) ON DELETE CASCADE,
    tenant_id     BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    start_date    DATE NOT NULL,
    end_date      DATE,
    rent_cold     NUMERIC(12,2) NOT NULL DEFAULT 0,
    nk_prepay     NUMERIC(12,2) NOT NULL DEFAULT 0,
    due_day       INT NOT NULL DEFAULT 3 CHECK (due_day BETWEEN 1 AND 28),
    deposit       NUMERIC(12,2) NOT NULL DEFAULT 0,
    deposit_paid  BOOLEAN NOT NULL DEFAULT FALSE,
    persons       INT NOT NULL DEFAULT 1,
    rent_type     TEXT NOT NULL DEFAULT 'fest' CHECK (rent_type IN ('fest','staffel','index')),
    last_increase DATE,
    track_from    DATE,              -- track expected vs. actual from this month (default: start of account data)
    match_text    TEXT NOT NULL DEFAULT '', -- extra search term for payment matching
    notes         TEXT NOT NULL DEFAULT ''
);

-- Rent changes (graduated, indexed, increase): effective from valid_from
CREATE TABLE rent_steps (
    id         BIGSERIAL PRIMARY KEY,
    lease_id   BIGINT NOT NULL REFERENCES leases(id) ON DELETE CASCADE,
    valid_from DATE NOT NULL,
    rent_cold  NUMERIC(12,2) NOT NULL,
    nk_prepay  NUMERIC(12,2) NOT NULL,
    UNIQUE (lease_id, valid_from)
);

-- Follow-up reminders
CREATE TABLE reminders (
    id          BIGSERIAL PRIMARY KEY,
    property_id BIGINT REFERENCES properties(id) ON DELETE CASCADE,
    lease_id    BIGINT REFERENCES leases(id) ON DELETE CASCADE,
    due_date    DATE NOT NULL,
    title       TEXT NOT NULL,
    done        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Assignment of transactions to Mietkonten (rent accounts)
ALTER TABLE transactions ADD COLUMN property_id BIGINT REFERENCES properties(id) ON DELETE SET NULL;
ALTER TABLE transactions ADD COLUMN lease_id BIGINT REFERENCES leases(id) ON DELETE SET NULL;
ALTER TABLE transactions ADD COLUMN cost_type TEXT NOT NULL DEFAULT '';
ALTER TABLE transactions ADD COLUMN hv_source TEXT NOT NULL DEFAULT 'auto' CHECK (hv_source IN ('auto','manual'));
CREATE INDEX transactions_lease_idx ON transactions (lease_id);
CREATE INDEX transactions_property_idx ON transactions (property_id);

-- Remember which payee belongs to which property / cost type
CREATE TABLE hv_rules (
    merchant_key TEXT PRIMARY KEY,
    property_id  BIGINT REFERENCES properties(id) ON DELETE CASCADE,
    cost_type    TEXT NOT NULL DEFAULT ''
);

-- Nebenkosten (service charges) not paid via the Mietkonto (e.g. from the WEG/owners' association statement)
CREATE TABLE manual_costs (
    id          BIGSERIAL PRIMARY KEY,
    property_id BIGINT NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    year        INT NOT NULL,
    cost_type   TEXT NOT NULL,
    amount      NUMERIC(12,2) NOT NULL,
    note        TEXT NOT NULL DEFAULT ''
);

-- Allocation key per property and cost type (if there is no entry, the default applies)
CREATE TABLE nk_keys (
    property_id BIGINT NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    cost_type   TEXT NOT NULL,
    key         TEXT NOT NULL CHECK (key IN ('flaeche','personen','einheiten')),
    PRIMARY KEY (property_id, cost_type)
);
