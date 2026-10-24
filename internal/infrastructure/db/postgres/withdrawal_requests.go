package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

type withdrawalRequestSQL struct {
	ID             uint64          `gorm:"column:id"`
	UserID         uint64          `gorm:"column:user_id"`
	AssetCode      string          `gorm:"column:asset_code"`
	Network        uint8           `gorm:"column:network"`
	ToAddress      string          `gorm:"column:to_address"`
	Amount         decimal.Decimal `gorm:"column:amount"`
	Fee            decimal.Decimal `gorm:"column:fee"`
	FeeTier        uint8           `gorm:"column:fee_tier"`
	Description    *string         `gorm:"column:description"`
	Status         uint8           `gorm:"column:status"`
	BlockchainTxID *uint64         `gorm:"column:blockchain_tx_id"`
	CreatedAt      time.Time       `gorm:"column:created_at"`
	UpdatedAt      *time.Time      `gorm:"column:updated_at"`
	ConfirmedAt    *time.Time      `gorm:"column:confirmed_at"`
}

// TableName sets the insert table name for this struct type
func (withdrawalRequestSQL) TableName() string {
	return "withdrawal_requests"
}

// decode converts withdrawalRequestSQL to domain.WithdrawalRequest
func (w withdrawalRequestSQL) decode() domain.WithdrawalRequest {
	var blockchainTxID *domain.ID
	if w.BlockchainTxID != nil {
		id := domain.ID(*w.BlockchainTxID)
		blockchainTxID = &id
	}

	return domain.WithdrawalRequest{
		ID:             domain.NewIDFromUint64(w.ID),
		UserID:         domain.NewIDFromUint64(w.UserID),
		AssetCode:      currency.NewAssetCode(w.AssetCode),
		Network:        domain.NewChainNetwork(w.Network),
		ToAddress:      w.ToAddress,
		Amount:         w.Amount,
		Fee:            w.Fee,
		FeeTier:        domain.NewFeeTier(w.FeeTier),
		Status:         domain.NewWithdrawalStatus(w.Status),
		Description:    w.Description,
		BlockchainTxID: blockchainTxID,
		CreatedAt:      w.CreatedAt,
		UpdatedAt:      w.UpdatedAt,
		ConfirmedAt:    w.ConfirmedAt,
	}
}

type WithdrawalRequestRepo struct {
	db *gorm.DB
}

// NewWithdrawalRequestRepository creates a new withdrawal request repository
func NewWithdrawalRequestRepository(db *gorm.DB) *WithdrawalRequestRepo {
	return &WithdrawalRequestRepo{db: db}
}

// WithTxFromContext returns a new WithdrawalRequestRepo with the transaction from the context
func (r WithdrawalRequestRepo) WithTxFromContext(ctx context.Context) domain.WithdrawalRequestRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// Create creates a new withdrawal request record in the database
func (r WithdrawalRequestRepo) Create(ctx context.Context, request *domain.WithdrawalRequest) error {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestRepo:Create][request: %+v]", request)
	defer tools.EndDBRequestTimer(time.Now())

	row := encodeWithdrawalRequestForCreate(request)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("[WithdrawalRequestRepo:Create][err:%w]", domain.ErrRecordAlreadyExists)
		}
		return fmt.Errorf("[WithdrawalRequestRepo:Create] %w", err)
	}

	request.ID = domain.NewIDFromUint64(row.ID)
	request.CreatedAt = row.CreatedAt
	return nil
}

// encodeWithdrawalRequestForCreate converts domain.WithdrawalRequest to withdrawalRequestSQL for insertion
func encodeWithdrawalRequestForCreate(request *domain.WithdrawalRequest) *withdrawalRequestSQL {
	var blockchainTxID *uint64
	if request.BlockchainTxID != nil {
		id := request.BlockchainTxID.Uint64()
		blockchainTxID = &id
	}

	createdAt := request.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return &withdrawalRequestSQL{
		UserID:         request.UserID.Uint64(),
		AssetCode:      request.AssetCode.String(),
		Network:        request.Network.Uint8(),
		ToAddress:      request.ToAddress,
		Amount:         request.Amount,
		Fee:            request.Fee,
		BlockchainTxID: blockchainTxID,
		FeeTier:        request.FeeTier.Uint8(),
		Status:         request.Status.Uint8(),
		CreatedAt:      createdAt,
	}
}

// Update updates the status of a withdrawal request
func (r WithdrawalRequestRepo) Update(ctx context.Context, id domain.ID, params domain.UpdateWithdrawalRequestParams) error {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestRepo:Update][id: %d][params: %+v]", id, params)
	defer tools.EndDBRequestTimer(time.Now())

	updateData := map[string]interface{}{
		"status":     params.Status.Uint8(),
		"updated_at": time.Now().UTC(),
	}
	if params.Description != nil {
		updateData["description"] = params.Description
	}
	if params.ConfirmedAt != nil {
		updateData["confirmed_at"] = *params.ConfirmedAt
	}
	if params.BlockchainTxID != nil {
		updateData["blockchain_tx_id"] = params.BlockchainTxID.Uint64()
	}

	// Model takes the SQL row type, like every other method here. Passing the
	// domain struct resolved to the same table only through GORM's naming
	// strategy, which is not something this repository should depend on.
	result := r.db.WithContext(ctx).
		Model(&withdrawalRequestSQL{}).
		Where("id = ?", id.Uint64()).
		Updates(updateData)
	if result.Error != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[WithdrawalRequestRepo:Update][err:%w]", result.Error)
	}

	// Every caller loads the row under GetByIDForUpdate first, so no match means
	// the row vanished mid-transaction. Reporting it matters because these
	// updates carry the status the ledger has already moved to: a silently lost
	// write leaves a withdrawal describing money that has been settled.
	if result.RowsAffected == 0 {
		return fmt.Errorf("[WithdrawalRequestRepo:Update][err:%w]", domain.ErrRecordNotFound)
	}

	return nil
}

// GetByID retrieves a withdrawal request by its ID
func (r WithdrawalRequestRepo) GetByID(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestRepo:GetByID][id: %d]", id)
	defer tools.EndDBRequestTimer(time.Now())

	var row withdrawalRequestSQL
	if err := r.db.WithContext(ctx).
		Model(&withdrawalRequestSQL{}).
		Where("id = ?", id).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.WithdrawalRequest{}, fmt.Errorf("[WithdrawalRequestRepo:GetByID][err:%w]", domain.ErrRecordNotFound)
		}
		tools.TrackMetricDBErrorAdd()
		return domain.WithdrawalRequest{}, fmt.Errorf("[WithdrawalRequestRepo:GetByID][err:%w]", err)
	}

	return row.decode(), nil
}

// GetByIDForUpdate retrieves a withdrawal request by its ID with a row-level lock for update.
func (r WithdrawalRequestRepo) GetByIDForUpdate(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestRepo:GetByIDForUpdate][id: %d]", id)
	defer tools.EndDBRequestTimer(time.Now())

	var row withdrawalRequestSQL
	if err := r.db.WithContext(ctx).
		Model(&withdrawalRequestSQL{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id.Uint64()).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.WithdrawalRequest{}, fmt.Errorf("[WithdrawalRequestRepo:GetByIDForUpdate][err:%w]", domain.ErrRecordNotFound)
		}
		tools.TrackMetricDBErrorAdd()
		return domain.WithdrawalRequest{}, fmt.Errorf("[WithdrawalRequestRepo:GetByIDForUpdate][err:%w]", err)
	}

	return row.decode(), nil
}

// GetByBlockchainIDForUpdate retrieves a withdrawal request by its blockchain transaction ID with a row-level lock for update.
func (r WithdrawalRequestRepo) GetByBlockchainIDForUpdate(ctx context.Context, blockchainTxID domain.ID) (domain.WithdrawalRequest, error) {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestRepo:GetByBlockchainIDForUpdate][blockchainTxID: %d]", blockchainTxID)
	defer tools.EndDBRequestTimer(time.Now())

	var row withdrawalRequestSQL
	if err := r.db.WithContext(ctx).
		Model(&withdrawalRequestSQL{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("blockchain_tx_id = ?", blockchainTxID.Uint64()).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.WithdrawalRequest{}, fmt.Errorf("[WithdrawalRequestRepo:GetByBlockchainIDForUpdate][err:%w]", domain.ErrRecordNotFound)
		}
		tools.TrackMetricDBErrorAdd()
		return domain.WithdrawalRequest{}, fmt.Errorf("[WithdrawalRequestRepo:GetByBlockchainIDForUpdate][err:%w]", err)
	}

	return row.decode(), nil
}

// FindStuckByStatus returns up to limit withdrawal requests stuck in the given
// status for longer than olderThan. New withdrawals are filtered by created_at
// (no update happens before the queue consumer attempts SendToBlockchain),
// while Pending withdrawals are filtered by updated_at (status moved into
// Pending after a successful node call). Other statuses are not supported by
// the reconciliation pass and yield an empty result without a query.
//
// No composite (status, created_at) index backs this, deliberately. Measured on
// 200k rows: with stuck rows rare — which they are by definition — the planner
// picks the existing single-column status index and filters the handful of
// matches; with them dense it walks the primary key instead, because
// ORDER BY id LIMIT n lets it stop early. Neither plan used a composite index,
// so adding one would only cost write amplification.
func (r WithdrawalRequestRepo) FindStuckByStatus(ctx context.Context, status domain.WithdrawalStatus, olderThan time.Duration, limit int) ([]domain.WithdrawalRequest, error) {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestRepo:FindStuckByStatus][status:%d][olderThan:%s][limit:%d]", status, olderThan, limit)
	defer tools.EndDBRequestTimer(time.Now())

	if limit <= 0 {
		return nil, nil
	}

	threshold := time.Now().UTC().Add(-olderThan)

	query := r.db.WithContext(ctx).
		Model(&withdrawalRequestSQL{}).
		Where("status = ?", status.Uint8()).
		Order("id ASC").
		Limit(limit)

	switch status {
	case domain.WithdrawalStatusNew:
		query = query.Where("created_at < ?", threshold)
	case domain.WithdrawalStatusPending:
		// updated_at is set whenever Update() runs; for rows that have only
		// just transitioned to Pending it tracks the SendToBlockchain commit.
		// Fallback to created_at when updated_at is NULL keeps the filter
		// working on the rare row that was inserted directly into Pending.
		query = query.Where("COALESCE(updated_at, created_at) < ?", threshold)
	default:
		return nil, nil
	}

	var rows []withdrawalRequestSQL
	if err := query.Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[WithdrawalRequestRepo:FindStuckByStatus][err:%w]", err)
	}

	out := make([]domain.WithdrawalRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.decode())
	}
	return out, nil
}

// WithdrawalList returns a filtered withdrawal list for the user.
func (r WithdrawalRequestRepo) WithdrawalList(ctx context.Context, userID domain.ID, filter domain.WithdrawalFilter) ([]domain.WithdrawalRequest, int64, error) {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestRepo:WithdrawalList][userID: %d][filter: %+v]", userID, filter)
	defer tools.EndDBRequestTimer(time.Now())
	var (
		rows  []withdrawalRequestSQL
		total int64
	)

	query := r.db.WithContext(ctx).
		Select("w.id", "w.user_id", "w.asset_code", "w.network", "w.to_address", "w.amount", "w.status", "w.description", "w.created_at").
		Table("withdrawal_requests AS w").
		Where("w.user_id = ?", userID.Uint64()).
		Order("w.id DESC")

	if filter.AssetCode != nil {
		query = query.Where("w.asset_code = ?", strings.ToUpper(*filter.AssetCode))
	}

	if filter.Status != nil {
		query = query.Where("w.status = ?", filter.Status.Uint8())
	}

	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("[WithdrawalRequestRepo:WithdrawalList][err: %w]", err)
	}

	query = query.
		Limit(filter.Limit).
		Offset(filter.Offset)

	if err := query.Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, 0, fmt.Errorf("[WithdrawalRequestRepo:WithdrawalList][err: %w]", err)
	}

	deposits := make([]domain.WithdrawalRequest, 0, len(rows))
	for _, row := range rows {
		deposits = append(deposits, row.decode())
	}

	return deposits, total, nil
}
