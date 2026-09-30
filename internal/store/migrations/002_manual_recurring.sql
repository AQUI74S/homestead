-- Manually created contracts (e.g. annual insurance not yet showing up in the bank fetch)
ALTER TABLE recurring ADD COLUMN manual BOOLEAN NOT NULL DEFAULT FALSE;
INSERT INTO settings (key, value) VALUES ('period_mode', 'salary'), ('salary_series', '') ON CONFLICT (key) DO NOTHING;
