-- Bank request limits. PSD2 lets banks restrict unattended access (without the user
-- present) to about 4 requests per day and account; some allow fewer.

-- Learned limit per account (NULL = configured default) and a pause after the bank
-- rejected a request because of its limit.
ALTER TABLE accounts ADD COLUMN daily_limit   INT;
ALTER TABLE accounts ADD COLUMN limited_until TIMESTAMPTZ;

-- Every request to the bank; attended = made while the user was present (PSU headers
-- sent), which does not count towards the unattended limit.
CREATE TABLE bank_calls (
    id         BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    attended   BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX bank_calls_account_at ON bank_calls(account_id, at);
