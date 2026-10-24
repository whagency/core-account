CREATE TABLE IF NOT EXISTS balance_histories (
    user_id BIGINT NOT NULL,
    asset_code VARCHAR(10) NOT NULL,
    amount NUMERIC(30,8) NOT NULL,
    rate NUMERIC(30,8) NOT NULL,
    stat_date DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NULL,
    unique (user_id, asset_code, stat_date)
);

CREATE INDEX IF NOT EXISTS idx_balance_histories_stat_date ON balance_histories(stat_date);
CREATE INDEX IF NOT EXISTS balance_histories_user_date_idx ON balance_histories(user_id, stat_date);