CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

CREATE TABLE IF NOT EXISTS accounts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id BIGINT,
    asset_code VARCHAR(10) NOT NULL,
    acct_type SMALLINT NOT NULL,
    balance NUMERIC(38,0) NOT NULL DEFAULT 0,
    security_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ,
    unique (owner_id, asset_code, acct_type)
);

COMMENT ON COLUMN accounts.owner_id IS 'Owner ID; NULL for system accounts';
COMMENT ON COLUMN accounts.acct_type IS 'Account type (1 - liability/ 2 - hold / 3 - revenue, 4 - expense)';

CREATE INDEX IF NOT EXISTS accounts_asset_acct_type on accounts(asset_code, acct_type);
CREATE INDEX IF NOT EXISTS accounts_main_idx_hint on accounts(owner_id, asset_code);
CREATE INDEX IF NOT EXISTS accounts_positive_user_balances_idx ON accounts(owner_id, asset_code, acct_type) WHERE owner_id IS NOT NULL AND balance > 0;
CREATE UNIQUE INDEX IF NOT EXISTS accounts_owner_null_unique ON accounts(asset_code, acct_type) WHERE owner_id IS NULL;

ALTER TABLE accounts ADD CONSTRAINT balance_nonneg_for_nonnull_owner CHECK (owner_id IS NULL OR balance >= 0);
