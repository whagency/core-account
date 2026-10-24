CREATE TABLE IF NOT EXISTS transfer_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_from_id BIGINT NOT NULL,
    user_to_id BIGINT NOT NULL,
    asset_code VARCHAR(10) NOT NULL,
    amount NUMERIC(38,0) NOT NULL,
    fee NUMERIC(38,0) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transfer_requests_user_from_id ON transfer_requests(user_from_id);
CREATE INDEX IF NOT EXISTS idx_transfer_requests_user_to_id ON transfer_requests(user_to_id);