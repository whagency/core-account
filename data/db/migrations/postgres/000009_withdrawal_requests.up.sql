CREATE TABLE IF NOT EXISTS withdrawal_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL,
    asset_code VARCHAR(10) NOT NULL,
    network SMALLINT NOT NULL,
    to_address TEXT NOT NULL,
    amount NUMERIC(38,0) NOT NULL,
    fee NUMERIC(38,0) NOT NULL DEFAULT 0,
    fee_tier SMALLINT NOT NULL,
    status SMALLINT NOT NULL,
    blockchain_tx_id BIGINT REFERENCES blockchain_txs(id) ON DELETE SET NULL ON UPDATE CASCADE,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ
);

COMMENT ON TABLE withdrawal_requests IS 'Withdrawal transaction requests';
COMMENT ON COLUMN withdrawal_requests.fee_tier IS '1-SLOW, 2-STANDARD, 3-FAST';
COMMENT ON COLUMN withdrawal_requests.status IS '1-NEW, 2-PENDING, 3-SUCCESS, 4-FAILED, 5-CANCELLED, 6-BLOCKED, 7-REJECTED, 8-MANUAL_REVIEW';
COMMENT ON COLUMN withdrawal_requests.network IS 'Network identifier, e.g., 1=trx';

CREATE INDEX IF NOT EXISTS withdrawal_requests_user_id_idx ON withdrawal_requests(user_id);
CREATE INDEX IF NOT EXISTS withdrawal_requests_status_idx ON withdrawal_requests(status);
CREATE INDEX IF NOT EXISTS withdrawal_requests_created_at_idx ON withdrawal_requests(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS withdrawal_requests_blockchain_tx_id_uniq_idx ON withdrawal_requests(blockchain_tx_id)  WHERE blockchain_tx_id IS NOT NULL;