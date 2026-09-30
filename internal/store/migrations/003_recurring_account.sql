-- Account a recurring payment is debited from (for "Demnächst abgebucht" / upcoming debits per account)
ALTER TABLE recurring ADD COLUMN account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL;
