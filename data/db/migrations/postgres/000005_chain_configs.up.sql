CREATE TABLE IF NOT EXISTS chain_configs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    asset_code VARCHAR(10) NOT NULL,
    network SMALLINT NOT NULL,
    decimals INTEGER NOT NULL,
    min_confirmations INTEGER NOT NULL,
    dust_threshold NUMERIC(38,0) NOT NULL,
    confirm_finality INTEGER NOT NULL,
    asset_name VARCHAR(255) NOT NULL,
    block_url VARCHAR(50),
    tx_url VARCHAR(50),
    address_url VARCHAR(50),
    contract_address_url VARCHAR(50),
    address_regex VARCHAR(50),
    withdraw_fee NUMERIC(38,8) NOT NULL DEFAULT 0,
    deposit_fee NUMERIC(38,8) NOT NULL DEFAULT 0,
    min_amount_withdrawal_review NUMERIC(38,8) NOT NULL DEFAULT 0,
    withdraw_fee_type SMALLINT NOT NULL DEFAULT 1,
    deposit_fee_type SMALLINT NOT NULL DEFAULT 1,
    estimated_arrival_time INTEGER NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ,
    UNIQUE(asset_code, network)
);

COMMENT ON TABLE chain_configs IS 'Runtime configuration for blockchain networks and assets';
COMMENT ON COLUMN chain_configs.min_confirmations IS 'Minimum confirmations required for deposit credit';
COMMENT ON COLUMN chain_configs.dust_threshold IS 'Minimum amount threshold in quants';
COMMENT ON COLUMN chain_configs.confirm_finality IS 'Number of confirmations for finality';
COMMENT ON COLUMN chain_configs.network IS 'Network identifier, e.g., 1=trx';

CREATE INDEX IF NOT EXISTS chain_configs_asset_idx ON chain_configs(asset_code);
CREATE INDEX IF NOT EXISTS chain_configs_network_idx ON chain_configs(network);
