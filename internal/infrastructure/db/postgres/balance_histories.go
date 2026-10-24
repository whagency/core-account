package postgres

import (
	"context"
	"fmt"
	"time"

	"account-service/internal/domain"
	"account-service/tools"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/db"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const _defaultUpsertBatchSize = 1000

type balanceHistorySQL struct {
	UserID    uint64          `gorm:"column:user_id"`
	AssetCode string          `gorm:"column:asset_code"`
	Amount    decimal.Decimal `gorm:"column:amount"`
	Rate      decimal.Decimal `gorm:"column:rate"`
	StatDate  time.Time       `gorm:"column:stat_date"`
	CreatedAt time.Time       `gorm:"column:created_at"`
	UpdatedAt *time.Time      `gorm:"column:updated_at"`
}

// TableName sets the insert table name for this struct type
func (balanceHistorySQL) TableName() string {
	return "balance_histories"
}

// decode converts eventSQL to domain.BalanceHistory
func (u balanceHistorySQL) decode() domain.BalanceHistory {
	return domain.BalanceHistory{
		UserID:    domain.NewIDFromUint64(u.UserID),
		AssetCode: currency.NewAssetCode(u.AssetCode),
		Amount:    u.Amount,
		Rate:      u.Rate,
		StatDate:  u.StatDate,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

type BalanceHistoryRepo struct {
	db *gorm.DB
}

// NewBalanceHistoryRepository creates a new instance of BalanceHistoryRepo
func NewBalanceHistoryRepository(db *gorm.DB) *BalanceHistoryRepo {
	return &BalanceHistoryRepo{db: db}
}

// WithTxFromContext checks for an active transaction in the context and returns a new repository instance using that transaction if it exists.
func (r BalanceHistoryRepo) WithTxFromContext(ctx context.Context) domain.BalanceHistoryRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// UpsertTodayStats inserts/updates today's stats in batches.
// Uses UNIQUE(user_id, asset_code, stat_date) as conflict target.
func (r BalanceHistoryRepo) UpsertTodayStats(ctx context.Context, histories []domain.BalanceHistory) error {
	logger.LogCtx(ctx).Debugf("[BalanceHistoryRepo:UpsertTodayStats][count:%d]", len(histories))
	defer tools.EndDBRequestTimer(time.Now())

	if len(histories) == 0 {
		return nil
	}

	rows := make([]balanceHistorySQL, 0, len(histories))
	now := time.Now().UTC()
	for i := range histories {
		rows = append(rows, encodeBalanceHistoryForUpsert(histories[i], now))
	}

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "asset_code"},
				{Name: "stat_date"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"amount":     gorm.Expr("EXCLUDED.amount"),
				"rate":       gorm.Expr("EXCLUDED.rate"),
				"updated_at": gorm.Expr("EXCLUDED.updated_at"),
			}),
		}).
		CreateInBatches(rows, _defaultUpsertBatchSize).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[BalanceHistoryRepo:UpsertTodayStats][err: %w]", err)
	}

	return nil
}

// FindTodayStats retrieves all balance history stats for today for a given user ID.
func (r BalanceHistoryRepo) FindTodayStats(ctx context.Context, userID *domain.ID) ([]domain.BalanceHistory, error) {
	defer tools.EndDBRequestTimer(time.Now())

	var rows []balanceHistorySQL
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND stat_date = CURRENT_DATE", userID.Uint64()).
		Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[BalanceHistoryRepo:FindTodayStats][err: %w]", err)
	}

	balanceHistories := make([]domain.BalanceHistory, 0, len(rows))
	for _, row := range rows {
		balanceHistories = append(balanceHistories, row.decode())
	}

	return balanceHistories, nil
}

// FindYesterdayStats retrieves all balance history stats for yesterday for a given user ID.
func (r BalanceHistoryRepo) FindYesterdayStats(ctx context.Context, userID *domain.ID) ([]domain.BalanceHistory, error) {
	defer tools.EndDBRequestTimer(time.Now())

	var rows []balanceHistorySQL
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND stat_date = CURRENT_DATE - 1", userID.Uint64()).
		Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[BalanceHistoryRepo:FindYesterdayStats][err: %w]", err)
	}

	balanceHistories := make([]domain.BalanceHistory, 0, len(rows))
	for _, row := range rows {
		balanceHistories = append(balanceHistories, row.decode())
	}

	return balanceHistories, nil
}

// encodeBalanceHistoryForUpsert returns a balanceHistorySQL struct for creating a new stat
func encodeBalanceHistoryForUpsert(history domain.BalanceHistory, now time.Time) balanceHistorySQL {
	return balanceHistorySQL{
		UserID:    history.UserID.Uint64(),
		AssetCode: history.AssetCode.String(),
		Amount:    history.Amount,
		Rate:      history.Rate,
		StatDate:  history.StatDate,
		CreatedAt: now,
		UpdatedAt: &now,
	}
}
