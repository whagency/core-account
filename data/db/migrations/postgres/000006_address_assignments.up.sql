CREATE TABLE IF NOT EXISTS address_assignments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id BIGINT,
    asset_code VARCHAR(10) NOT NULL,
    network SMALLINT NOT NULL,
    purpose SMALLINT NOT NULL,
    address TEXT NOT NULL,
    status SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ,
    UNIQUE(address, network)
);

COMMENT ON TABLE address_assignments IS 'Deposit addresses assigned to users';
COMMENT ON COLUMN address_assignments.status IS '1-ALLOCATED, 2-IN_USE, 3-RETIRED';
COMMENT ON COLUMN address_assignments.network IS 'Network identifier, e.g., 1=trx';

CREATE INDEX IF NOT EXISTS address_assignments_user_asset_idx ON address_assignments(owner_id, asset_code);
CREATE INDEX IF NOT EXISTS address_assignments_address_idx ON address_assignments(address);
CREATE INDEX IF NOT EXISTS address_assignments_status_idx ON address_assignments(status);
