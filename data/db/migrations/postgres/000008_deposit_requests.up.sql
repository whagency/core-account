CREATE TABLE IF NOT EXISTS deposit_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL,
    asset_code VARCHAR(10) NOT NULL,
    network SMALLINT NOT NULL,
    address_assignment_id BIGINT REFERENCES address_assignments(id) ON DELETE SET NULL ON UPDATE CASCADE,
    blockchain_tx_id BIGINT REFERENCES blockchain_txs(id) ON DELETE SET NULL ON UPDATE CASCADE,
    amount NUMERIC(38,0) NOT NULL,
    fee NUMERIC(38,0) NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ
);

COMMENT ON TABLE deposit_requests IS 'Deposit transaction requests';
COMMENT ON COLUMN deposit_requests.status IS '1-NEW, 2-PENDING, 3-SUCCESS, 4-FAILED, 5-BLOCKED, 6-MANUAL_REVIEW';
COMMENT ON COLUMN deposit_requests.network IS 'Network identifier, e.g., 1=trx';

CREATE INDEX IF NOT EXISTS deposit_requests_user_id_idx ON deposit_requests(user_id);
CREATE INDEX IF NOT EXISTS deposit_requests_status_idx ON deposit_requests(status);
CREATE INDEX IF NOT EXISTS deposit_requests_created_at_idx ON deposit_requests(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS deposit_requests_blockchain_tx_id_uniq_idx ON deposit_requests(blockchain_tx_id) WHERE blockchain_tx_id IS NOT NULL;