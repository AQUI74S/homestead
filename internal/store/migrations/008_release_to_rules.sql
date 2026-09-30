-- Transactions set by hand to the same category as an existing rule follow that rule
UPDATE transactions t SET category_source = 'auto'
FROM rules r
WHERE t.category_source = 'manual' AND t.category_id = r.category_id
  AND ((r.field = 'merchant' AND t.merchant_key = r.pattern)
    OR (r.field = 'iban' AND upper(replace(t.counterparty_iban, ' ', '')) = upper(replace(r.pattern, ' ', ''))));
