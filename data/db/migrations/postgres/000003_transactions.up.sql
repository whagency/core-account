CREATE TABLE IF NOT EXISTS transactions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    journal_id BIGINT NOT NULL REFERENCES journal_entries(id) ON DELETE CASCADE ON UPDATE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE ON UPDATE CASCADE,
    asset_code VARCHAR(10) NOT NULL,
    type SMALLINT NOT NULL,
    amount NUMERIC(38,0) NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON COLUMN transactions.type IS 'Transaction type (1 - debit/ 2 - credit/ 3 - fee)';

CREATE INDEX IF NOT EXISTS postings_by_account ON transactions(account_id);
CREATE INDEX IF NOT EXISTS postings_by_journal ON transactions(journal_id);
CREATE INDEX IF NOT EXISTS postings_by_asset ON transactions(asset_code);
CREATE INDEX IF NOT EXISTS postings_created_at ON transactions(created_at);