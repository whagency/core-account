package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"account-service/internal/domain"
	"account-service/tools"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/db"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
	"gorm.io/gorm"
)

type blockchainTxSQL struct {
	ID                uint64          `gorm:"column:id"`
	ExternalRequestID string          `gorm:"column:external_request_id"`
	TxID              *string         `gorm:"column:tx_id"`
	AssetCode         string          `gorm:"column:asset_code"`
	Network           uint8           `gorm:"column:network"`
	Type              uint8           `gorm:"column:tx_type"`
	FromAddress       string          `gorm:"column:from_address"`
	ToAddress         string          `gorm:"column:to_address"`
	Amount            decimal.Decimal `gorm:"column:amount"`
	Confirmations     uint8           `gorm:"column:confirmations"`
	Status            uint8           `gorm:"column:status"`
	CreatedAt         time.Time       `gorm:"column:created_at"`
	ConfirmedAt       *time.Time      `gorm:"column:confirmed_at"`
	UpdatedAt         *time.Time      `gorm:"column:updated_at"`
}

// TableName sets the insert table name for this struct type
func (blockchainTxSQL) TableName() string {
	return "blockchain_txs"
}

// decode converts blockchainTxSQL to domain.BlockchainTx
func (txSQL blockchainTxSQL) decode() domain.BlockchainTx {
	return domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(txSQL.ID),
		ExternalRequestID: txSQL.ExternalRequestID,
		TxID:              txSQL.TxID,
		AssetCode:         currency.NewAssetCode(txSQL.AssetCode),
		Network:           domain.NewChainNetwork(txSQL.Network),
		Type:              domain.NewBlockchainTxType(txSQL.Type),
		FromAddress:       txSQL.FromAddress,
		ToAddress:         txSQL.ToAddress,
		Amount:            txSQL.Amount,
		Confirmations:     txSQL.Confirmations,
		Status:            domain.NewBlockchainTxStatus(txSQL.Status),
		CreatedAt:         txSQL.CreatedAt,
		ConfirmedAt:       txSQL.ConfirmedAt,
		UpdatedAt:         txSQL.UpdatedAt,
	}
}

type BlockchainTxRepo struct {
	db *gorm.DB
}

// NewBlockchainTxRepository creates a new blockchain transaction repository
func NewBlockchainTxRepository(db *gorm.DB) *BlockchainTxRepo {
	return &BlockchainTxRepo{db: db}
}

// WithTxFromContext returns a new BlockchainTxRepo with the transaction from the context
func (r BlockchainTxRepo) WithTxFromContext(ctx context.Context) domain.BlockchainTxRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// Create creates a new blockchain transaction record
func (r BlockchainTxRepo) Create(ctx context.Context, tx *domain.BlockchainTx) error {
	logger.LogCtx(ctx).Debugf("[BlockchainTxRepo:Create][tx: %+v]", tx)
	defer tools.EndDBRequestTimer(time.Now())

	row := encodeTxForCreate(tx)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		// external_request_id is unique. Callers that resend an idempotent
		// request need to tell that apart from a real write failure so they can
		// fall back to the existing row.
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("[BlockchainTxRepo:Create][err:%w]", domain.ErrRecordAlreadyExists)
		}
		return fmt.Errorf("[BlockchainTxRepo:Create] %w", err)
	}

	tx.ID = domain.NewIDFromUint64(row.ID)
	tx.CreatedAt = row.CreatedAt
	tx.ConfirmedAt = row.ConfirmedAt
	return nil
}

// encodeTxForCreate encodes a domain.BlockchainTx into blockchainTxSQL for creation
func encodeTxForCreate(tx *domain.BlockchainTx) *blockchainTxSQL {
	var (
		createdAt   = time.Now().UTC()
		confirmedAt *time.Time
	)

	if tx.Status == domain.BlockchainTxStatusConfirmed {
		confirmedAt = &createdAt
	}

	return &blockchainTxSQL{
		ExternalRequestID: tx.ExternalRequestID,
		TxID:              tx.TxID,
		AssetCode:         tx.AssetCode.String(),
		Network:           tx.Network.Uint8(),
		Type:              tx.Type.Uint8(),
		FromAddress:       tx.FromAddress,
		ToAddress:         tx.ToAddress,
		Amount:            tx.Amount,
		Confirmations:     tx.Confirmations,
		CreatedAt:         createdAt,
		ConfirmedAt:       confirmedAt,
		Status:            tx.Status.Uint8(),
	}
}

// GetByParams retrieves a blockchain transaction by params
func (r BlockchainTxRepo) GetByParams(ctx context.Context, params domain.GetBlockchainTxParams) (domain.BlockchainTx, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainTxRepo:GetByParams][params: %+v]", params)
	defer tools.EndDBRequestTimer(time.Now())

	var txSQL blockchainTxSQL
	if err := r.db.WithContext(ctx).
		Where("tx_id = ? AND asset_code = ? AND network = ?", params.TxID, params.AssetCode.String(), params.Network.Uint8()).
		First(&txSQL).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.BlockchainTx{}, fmt.Errorf("[BlockchainTxRepo:GetByParams][err:%w]", domain.ErrRecordNotFound)
		}

		tools.TrackMetricDBErrorAdd()
		return domain.BlockchainTx{}, fmt.Errorf("[BlockchainTxRepo:GetByParams][err:%w]", err)
	}

	return txSQL.decode(), nil
}

// GetByExternalRequestID retrieves a blockchain transaction by its ID
func (r BlockchainTxRepo) GetByExternalRequestID(ctx context.Context, externalRequestID string) (domain.BlockchainTx, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainTxRepo:GetByExternalRequestID][externalRequestID: %s]", externalRequestID)
	defer tools.EndDBRequestTimer(time.Now())

	var txSQL blockchainTxSQL
	if err := r.db.WithContext(ctx).
		Where("external_request_id = ?", externalRequestID).
		First(&txSQL).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.BlockchainTx{}, fmt.Errorf("[BlockchainTxRepo:GetByExternalRequestID][err:%w]", domain.ErrRecordNotFound)
		}

		tools.TrackMetricDBErrorAdd()
		return domain.BlockchainTx{}, fmt.Errorf("[BlockchainTxRepo:GetByExternalRequestID][err:%w]", err)
	}

	return txSQL.decode(), nil
}

// GetByID retrieves a blockchain transaction by its ID
func (r BlockchainTxRepo) GetByID(ctx context.Context, id domain.ID) (domain.BlockchainTx, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainTxRepo:GetByID][id: %d]", id)
	defer tools.EndDBRequestTimer(time.Now())

	var txSQL blockchainTxSQL
	if err := r.db.WithContext(ctx).
		Where("id = ?", id.Uint64()).
		First(&txSQL).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.BlockchainTx{}, fmt.Errorf("[BlockchainTxRepo:GetByID][err:%w]", domain.ErrRecordNotFound)
		}

		tools.TrackMetricDBErrorAdd()
		return domain.BlockchainTx{}, fmt.Errorf("[BlockchainTxRepo:GetByID][err:%w]", err)
	}

	return txSQL.decode(), nil
}

// FindStuckInboundForReconcile returns inbound BlockchainTx rows whose age
// exceeds olderThan and whose deposit side is still actionable. These are
// candidates for the deposit reconciliation cron — typically rows where the
// blockchain.transaction.event message died after the callback webhook
// already created the BlockchainTx.
//
// Age is measured against COALESCE(updated_at, created_at). Rows are actionable
// when either no deposit_request exists yet, or the existing deposit_request is
// still New/Pending. Terminal/operator states are excluded so old Failed,
// Blocked, ManualReview, or Success deposits cannot starve later candidates.
func (r BlockchainTxRepo) FindStuckInboundForReconcile(ctx context.Context, olderThan time.Duration, limit int) ([]domain.BlockchainTx, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainTxRepo:FindStuckInboundForReconcile][olderThan:%s][limit:%d]", olderThan, limit)
	defer tools.EndDBRequestTimer(time.Now())

	if limit <= 0 {
		return nil, nil
	}

	threshold := time.Now().UTC().Add(-olderThan)

	var rows []blockchainTxSQL
	if err := r.stuckInboundForReconcileQuery(ctx, threshold, limit).Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[BlockchainTxRepo:FindStuckInboundForReconcile][err:%w]", err)
	}

	out := make([]domain.BlockchainTx, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.decode())
	}
	return out, nil
}

// stuckInboundForReconcileQuery returns a query for finding stuck inbound blockchain transactions for reconciliation.
func (r BlockchainTxRepo) stuckInboundForReconcileQuery(ctx context.Context, threshold time.Time, limit int) *gorm.DB {
	return r.db.WithContext(ctx).
		Model(&blockchainTxSQL{}).
		Where("tx_type = ?", domain.BlockchainTxTypeInbound.Uint8()).
		Where("COALESCE(updated_at, created_at) < ?", threshold).
		Where(
			`(
				NOT EXISTS (
					SELECT 1
					FROM deposit_requests dr
					WHERE dr.blockchain_tx_id = blockchain_txs.id
				)
				OR EXISTS (
					SELECT 1
					FROM deposit_requests dr
					WHERE dr.blockchain_tx_id = blockchain_txs.id
					  AND dr.status IN (?, ?)
				)
			)`,
			domain.DepositStatusNew.Uint8(),
			domain.DepositStatusPending.Uint8(),
		).
		Order("id ASC").
		Limit(limit)
}

// UpdateStatus updates the status, confirmations, and optional tx hash of a blockchain transaction.
func (r BlockchainTxRepo) UpdateStatus(ctx context.Context, id domain.ID, status domain.BlockchainTxStatus, confirmations uint8, txID *string) error {
	logger.LogCtx(ctx).Debugf("[BlockchainTxRepo:UpdateStatus][id: %d][status: %d][confirmations: %d][txID:%v]", id, status, confirmations, txID)
	defer tools.EndDBRequestTimer(time.Now())

	updates := map[string]interface{}{
		"status":        status,
		"confirmations": confirmations,
		"updated_at":    time.Now().UTC(),
	}
	if txID != nil {
		updates["tx_id"] = *txID
	}
	if status == domain.BlockchainTxStatusConfirmed {
		updates["confirmed_at"] = time.Now().UTC()
	}

	if err := r.db.WithContext(ctx).
		Model(&blockchainTxSQL{}).
		Where("id = ?", id).
		Updates(updates).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[BlockchainTxRepo:UpdateStatus][err:%w]", err)
	}

	return nil
}
