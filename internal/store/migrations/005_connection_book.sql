-- New accounts of a bank connection land in the area the connection was made from
ALTER TABLE bank_connections ADD COLUMN book TEXT NOT NULL DEFAULT 'haushalt' CHECK (book IN ('haushalt','verwaltung'));
UPDATE bank_connections c SET book='verwaltung'
  WHERE EXISTS (SELECT 1 FROM accounts a WHERE a.connection_id=c.id AND a.book='verwaltung')
    AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.connection_id=c.id AND a.book='haushalt');
