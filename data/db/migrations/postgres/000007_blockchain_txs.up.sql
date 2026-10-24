CREATE TABLE IF NOT EXISTS blockchain_txs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tx_id TEXT,
    external_request_id TEXT NOT NULL UNIQUE,
    asset_code VARCHAR(10) NOT NULL,
    network SMALLINT NOT NULL,
    tx_type SMALLINT NOT NULL,
    from_address TEXT NOT NULL,
    to_address TEXT NOT NULL,
    amount NUMERIC(38,0) NOT NULL,
    confirmations INTEGER NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);

COMMENT ON TABLE blockchain_txs IS 'Blockchain transactions';
COMMENT ON COLUMN blockchain_txs.status IS '1-NEW, 2-PENDING, 3-CONFIRMED, 4-FAILED, 5-IGNORED, 6-BLOCKED, 7-PENDING_SIGNATURES';
COMMENT ON COLUMN blockchain_txs.network IS 'Network identifier, e.g., 1=trx';
COMMENT ON COLUMN blockchain_txs.tx_type IS '1-INBOUND, 2-OUTBOUND';

CREATE INDEX IF NOT EXISTS blockchain_txs_txid_idx ON blockchain_txs(tx_id);
CREATE INDEX IF NOT EXISTS blockchain_txs_from_address_idx ON blockchain_txs(from_address);
CREATE INDEX IF NOT EXISTS blockchain_txs_to_address_idx ON blockchain_txs(to_address);
CREATE INDEX IF NOT EXISTS blockchain_txs_status_idx ON blockchain_txs(status);
CREATE INDEX IF NOT EXISTS blockchain_txs_created_at_idx ON blockchain_txs(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS blockchain_txs_tx_id_network_idx ON blockchain_txs(tx_id, network) WHERE tx_id IS NOT NULL;