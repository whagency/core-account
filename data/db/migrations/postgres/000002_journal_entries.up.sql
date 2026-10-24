CREATE TABLE IF NOT EXISTS journal_entries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ext_idempotency_key TEXT,
    biz_type SMALLINT NOT NULL,
    status SMALLINT NOT NULL,
    related_entry_id BIGINT REFERENCES journal_entries(id) ON DELETE SET NULL ON UPDATE CASCADE,
    reversal_of_id BIGINT REFERENCES journal_entries(id) ON DELETE SET NULL ON UPDATE CASCADE,
    description VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);

COMMENT ON COLUMN journal_entries.status IS 'Business status (1 - pending/ 2 - completed/ 3 - failed/ 4 - cancelled, 5- reversed)';
COMMENT ON COLUMN journal_entries.biz_type IS 'Business type (1 - deposit/ 2 - withdrawal_request/ 3 - withdrawal_complete/ 4 - withdrawal_reject/ 5 - withdrawal_cancel/ 6 - transfer)';

CREATE UNIQUE INDEX IF NOT EXISTS journal_entries_idem_uniq ON journal_entries(ext_idempotency_key) WHERE ext_idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS journal_entries__by_type_time ON journal_entries(biz_type, created_at);
CREATE INDEX IF NOT EXISTS journal_entries_status_idx ON journal_entries(status);
