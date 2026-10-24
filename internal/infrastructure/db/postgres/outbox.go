package postgres

import (
	"account-service/internal/domain"

	outboxpostgres "github.com/whagency/core-common/outbox/postgres"
	"gorm.io/gorm"
)

// NewOutboxRepository creates a domain.OutboxRepository backed by the shared
// outbox/postgres implementation, targeting the outbox_messages table.
func NewOutboxRepository(db *gorm.DB) domain.OutboxRepository {
	return outboxpostgres.New(db, "outbox_messages")
}
