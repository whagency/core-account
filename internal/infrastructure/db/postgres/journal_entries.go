package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"account-service/internal/domain"
	"account-service/tools"

	"github.com/whagency/core-common/db"
	"github.com/whagency/core-common/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type journalEntrySQL struct {
	ID                uint64     `gorm:"column:id"`
	ExtIdempotencyKey *string    `gorm:"column:ext_idempotency_key"`
	BizType           uint8      `gorm:"column:biz_type"`
	Status            uint8      `gorm:"column:status"`
	RelatedEntryID    *uint64    `gorm:"column:related_entry_id"`
	ReversalOfID      *uint64    `gorm:"column:reversal_of_id"`
	Description       *string    `gorm:"column:description"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	CompletedAt       *time.Time `gorm:"column:completed_at"`
	UpdatedAt         *time.Time `gorm:"column:updated_at"`
}

func (journalEntrySQL) TableName() string { return "journal_entries" }

// decode converts journalEntrySQL to domain.JournalEntry
func (j journalEntrySQL) decode() domain.JournalEntry {
	return domain.JournalEntry{
		ID:                     domain.NewIDFromUint64(j.ID),
		ExternalIdempotencyKey: j.ExtIdempotencyKey,
		BizType:                domain.NewJournalBizType(j.BizType),
		Status:                 domain.NewJournalStatus(j.Status),
		RelatedEntryID:         domain.NewNullableIDFromUint64(j.RelatedEntryID),
		ReversalOfID:           domain.NewNullableIDFromUint64(j.ReversalOfID),
		Description:            j.Description,
		CreatedAt:              j.CreatedAt,
		CompletedAt:            j.CompletedAt,
		UpdatedAt:              j.UpdatedAt,
	}
}

type JournalEntryRepo struct {
	db *gorm.DB
}

func NewJournalEntryRepository(db *gorm.DB) *JournalEntryRepo {
	return &JournalEntryRepo{db: db}
}

// WithTxFromContext checks for an active transaction in the context and returns a new repository instance using that transaction if it exists.
func (r JournalEntryRepo) WithTxFromContext(ctx context.Context) domain.JournalEntryRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// Create persists a new journal entry in the database and updates the ID, CreatedAt, and UpdatedAt fields of the provided entry.
func (r JournalEntryRepo) Create(ctx context.Context, entry *domain.JournalEntry) error {
	logger.LogCtx(ctx).Debugf("[JournalEntryRepo:Create][entry: %+v]", entry)
	defer tools.EndDBRequestTimer(time.Now())

	row := encodeJournalEntryForCreate(entry)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[JournalEntryRepo:Create][err: %w]", err)
	}

	entry.ID = domain.NewIDFromUint64(row.ID)
	entry.CreatedAt = row.CreatedAt
	entry.UpdatedAt = row.UpdatedAt
	entry.CompletedAt = row.CompletedAt

	return nil
}

// encodeJournalEntryForCreate converts domain.JournalEntry to journalEntrySQL
func encodeJournalEntryForCreate(entry *domain.JournalEntry) *journalEntrySQL {
	timeNow := time.Now().UTC()
	row := &journalEntrySQL{
		ExtIdempotencyKey: entry.ExternalIdempotencyKey,
		BizType:           entry.BizType.Uint8(),
		Status:            entry.Status.Uint8(),
		Description:       entry.Description,
		CreatedAt:         timeNow,
	}

	if entry.RelatedEntryID != nil {
		id := entry.RelatedEntryID.Uint64()
		row.RelatedEntryID = &id
	}
	if entry.ReversalOfID != nil {
		id := entry.ReversalOfID.Uint64()
		row.ReversalOfID = &id
	}

	if entry.Status == domain.JournalStatusCompleted {
		row.CompletedAt = &timeNow
	}

	return row
}

// GetByIdempotencyKey retrieves a journal entry by its external idempotency key.
func (r JournalEntryRepo) GetByIdempotencyKey(ctx context.Context, key string) (domain.JournalEntry, error) {
	logger.LogCtx(ctx).Debugf("[JournalEntryRepo:GetByIdempotencyKey][key: %s]", key)
	defer tools.EndDBRequestTimer(time.Now())

	var row journalEntrySQL
	if err := r.db.WithContext(ctx).
		Where("ext_idempotency_key = ?", key).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.JournalEntry{}, fmt.Errorf("[JournalEntryRepo:GetByIdempotencyKey][err: %w]", domain.ErrRecordNotFound)
		}
		tools.TrackMetricDBErrorAdd()
		return domain.JournalEntry{}, fmt.Errorf("[JournalEntryRepo:GetByIdempotencyKey][err: %w]", err)
	}

	return row.decode(), nil
}

// GetByIDForUpdate retrieves a journal entry by its ID with a row-level lock for update.
func (r JournalEntryRepo) GetByIDForUpdate(ctx context.Context, id domain.ID) (domain.JournalEntry, error) {
	logger.LogCtx(ctx).Debugf("[JournalEntryRepo:GetByIDForUpdate][id: %s]", id.String())
	defer tools.EndDBRequestTimer(time.Now())

	var row journalEntrySQL
	if err := r.db.WithContext(ctx).
		Where("id = ?", id.Uint64()).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Take(&row).Error; err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return domain.JournalEntry{}, fmt.Errorf("[JournalEntryRepo:GetByIDForUpdate][err: %w]", domain.ErrRecordNotFound)
		default:
			tools.TrackMetricDBErrorAdd()
			return domain.JournalEntry{}, fmt.Errorf("[JournalEntryRepo:GetByIDForUpdate][err: %w]", err)
		}
	}

	return row.decode(), nil
}

// UpdateIdempotencyKey sets a new external idempotency key for the journal entry.
func (r JournalEntryRepo) UpdateIdempotencyKey(ctx context.Context, id domain.ID, key string) error {
	logger.LogCtx(ctx).Debugf("[JournalEntryRepo:UpdateIdempotencyKey][id: %d][key: %s]", id, key)
	defer tools.EndDBRequestTimer(time.Now())

	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&journalEntrySQL{}).
		Where("id = ?", id.Uint64()).
		Updates(map[string]any{
			"ext_idempotency_key": key,
			"updated_at":          now,
		})
	if result.Error != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[JournalEntryRepo:UpdateIdempotencyKey][err: %w]", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("[JournalEntryRepo:UpdateIdempotencyKey][err: %w]", domain.ErrRecordNotFound)
	}

	return nil
}

// MarkReversed updates the status of a journal entry to 'reversed' and links it to the reversal entry.
func (r JournalEntryRepo) MarkReversed(ctx context.Context, id, relatedID domain.ID) error {
	logger.LogCtx(ctx).Debugf("[JournalEntryRepo:MarkReversed][id: %d][relatedID: %d]", id, relatedID)
	defer tools.EndDBRequestTimer(time.Now())

	result := r.db.WithContext(ctx).
		Model(&journalEntrySQL{}).
		Where("id = ?", id.Uint64()).
		Updates(map[string]any{
			"status":           domain.JournalStatusReversed.Uint8(),
			"related_entry_id": relatedID.Uint64(),
			"updated_at":       time.Now().UTC(),
		})
	if result.Error != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[JournalEntryRepo:MarkReversed][err: %w]", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("[JournalEntryRepo:MarkReversed][err: %w]", domain.ErrRecordNotFound)
	}

	return nil
}
