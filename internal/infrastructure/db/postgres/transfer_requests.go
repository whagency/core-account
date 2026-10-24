package postgres

import (
	"context"
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
)

type transferRequestsSQL struct {
	ID         uint64          `gorm:"column:id"`
	UserFromID uint64          `gorm:"column:user_from_id"`
	UserToID   uint64          `gorm:"column:user_to_id"`
	AssetCode  string          `gorm:"column:asset_code"`
	Amount     decimal.Decimal `gorm:"column:amount"`
	Fee        decimal.Decimal `gorm:"column:fee"`
	CreatedAt  time.Time       `gorm:"column:created_at"`
}

// TableName sets the insert table name for this struct type
func (transferRequestsSQL) TableName() string {
	return "transfer_requests"
}

type TransferRequestsRepo struct {
	db *gorm.DB
}

// NewTransferRequestsRepository creates a new transfer requests repository
func NewTransferRequestsRepository(db *gorm.DB) *TransferRequestsRepo {
	return &TransferRequestsRepo{db: db}
}

// WithTxFromContext returns a new TransferRequestsRepo with the transaction from the context
func (r TransferRequestsRepo) WithTxFromContext(ctx context.Context) domain.TransferRequestRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// Create creates a new transfer request record in the database
func (r TransferRequestsRepo) Create(ctx context.Context, request *domain.TransferRequest) error {
	logger.LogCtx(ctx).Debugf("[TransferRepo:Create][request: %+v]", request)
	defer tools.EndDBRequestTimer(time.Now())

	row := encodeTransferForCreate(request)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[TransferRepo:Create] %w", err)
	}

	request.ID = domain.NewIDFromUint64(row.ID)
	request.CreatedAt = row.CreatedAt
	return nil
}

// TransferList returns a filtered transfer list for the user.
func (r TransferRequestsRepo) TransferList(ctx context.Context, userID domain.ID, filter domain.TransferFilter) ([]domain.TransferRequest, int64, error) {
	logger.LogCtx(ctx).Debugf("[TransferRepo:TransferList][userID: %d][filter: %+v]", userID, filter)
	defer tools.EndDBRequestTimer(time.Now())
	var (
		rows  []transferRequestsSQL
		total int64
	)

	query := r.db.WithContext(ctx).
		Select("t.id", "t.user_from_id", "t.user_to_id", "t.asset_code", "t.amount", "t.fee", "t.created_at").
		Table("transfer_requests AS t").
		Where("t.user_from_id = ? OR t.user_to_id = ?", userID.Uint64(), userID.Uint64()).
		Order("t.id DESC")

	if filter.AssetCode != nil {
		query = query.Where("t.asset_code = ?", strings.ToUpper(*filter.AssetCode))
	}

	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("[TransferRepo:TransferList][err: %w]", err)
	}

	query = query.
		Limit(filter.Limit).
		Offset(filter.Offset)

	if err := query.Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, 0, fmt.Errorf("[TransferRepo:TransferList][err: %w]", err)
	}

	transfers := make([]domain.TransferRequest, 0, len(rows))
	for _, row := range rows {
		transfers = append(transfers, row.decode())
	}

	return transfers, total, nil
}

// encodeTransferRequestForCreate converts domain.TransferRequest to transferRequestsSQL for insertion
func encodeTransferForCreate(request *domain.TransferRequest) *transferRequestsSQL {
	createdAt := request.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return &transferRequestsSQL{
		UserFromID: request.UserFromID.Uint64(),
		UserToID:   request.UserToID.Uint64(),
		AssetCode:  request.AssetCode.String(),
		Amount:     request.Amount,
		CreatedAt:  createdAt,
	}
}

// decode converts transferRequestsSQL to domain.TransferRequest
func (t transferRequestsSQL) decode() domain.TransferRequest {
	return domain.TransferRequest{
		ID:         domain.NewIDFromUint64(t.ID),
		UserFromID: domain.NewIDFromUint64(t.UserFromID),
		UserToID:   domain.NewIDFromUint64(t.UserToID),
		AssetCode:  currency.NewAssetCode(t.AssetCode),
		Amount:     t.Amount,
		Fee:        t.Fee,
		CreatedAt:  t.CreatedAt,
	}
}
