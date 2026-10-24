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

type depositRequestSQL struct {
	ID                 uint64          `gorm:"column:id"`
	UserID             uint64          `gorm:"column:user_id"`
	AssetCode          string          `gorm:"column:asset_code"`
	Network            uint8           `gorm:"column:network"`
	AddressAssigmentID *uint64         `gorm:"column:address_assignment_id"`
	BlockchainTxID     *uint64         `gorm:"column:blockchain_tx_id"`
	Amount             decimal.Decimal `gorm:"column:amount"`
	Fee                decimal.Decimal `gorm:"column:fee"`
	Status             uint8           `gorm:"column:status"`
	Description        *string         `gorm:"column:description"`
	CreatedAt          time.Time       `gorm:"column:created_at"`
	UpdatedAt          *time.Time      `gorm:"column:updated_at"`
	ConfirmedAt        *time.Time      `gorm:"column:confirmed_at"`
}

type depositRequestDetailsSQL struct {
	ID          uint64          `gorm:"column:id"`
	UserID      uint64          `gorm:"column:user_id"`
	AssetCode   string          `gorm:"column:asset_code"`
	Network     uint8           `gorm:"column:network"`
	Address     string          `gorm:"column:address"`
	Amount      decimal.Decimal `gorm:"column:amount"`
	Fee         decimal.Decimal `gorm:"column:fee"`
	Status      uint8           `gorm:"column:status"`
	Description *string         `gorm:"column:description"`
	CreatedAt   time.Time       `gorm:"column:created_at"`
}

// TableName sets the insert table name for this struct type
func (depositRequestSQL) TableName() string {
	return "deposit_requests"
}

// decode converts depositRequestSQL to domain.DepositRequest
func (w depositRequestSQL) decode() domain.DepositRequest {
	return domain.DepositRequest{
		ID:                 domain.NewIDFromUint64(w.ID),
		UserID:             domain.NewIDFromUint64(w.UserID),
		AssetCode:          currency.NewAssetCode(w.AssetCode),
		Network:            domain.NewChainNetwork(w.Network),
		AddressAssigmentID: domain.NewNullableIDFromUint64(w.AddressAssigmentID),
		BlockchainTxID:     domain.NewNullableIDFromUint64(w.BlockchainTxID),
		Amount:             w.Amount,
		Fee:                w.Fee,
		Status:             domain.NewDepositStatus(w.Status),
		Description:        w.Description,
		CreatedAt:          w.CreatedAt,
		UpdatedAt:          w.UpdatedAt,
		ConfirmedAt:        w.ConfirmedAt,
	}
}

type DepositRequestRepo struct {
	db *gorm.DB
}

// NewDepositRequestRepository creates a new deposit request repository
func NewDepositRequestRepository(db *gorm.DB) *DepositRequestRepo {
	return &DepositRequestRepo{db: db}
}

// WithTxFromContext returns a new WithdrawalRequestRepo with the transaction from the context
func (r DepositRequestRepo) WithTxFromContext(ctx context.Context) domain.DepositRequestRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// Create creates a new deposit request record in the database
func (r DepositRequestRepo) Create(ctx context.Context, request *domain.DepositRequest) error {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:Create][request: %+v]", request)
	defer tools.EndDBRequestTimer(time.Now())

	row := encodeDepositRequestForCreate(request)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("[DepositRequestRepo:Create][err:%w]", domain.ErrRecordAlreadyExists)
		}
		return fmt.Errorf("[DepositRequestRepo:Create] %w", err)
	}

	request.ID = domain.NewIDFromUint64(row.ID)
	request.CreatedAt = row.CreatedAt
	request.UpdatedAt = row.UpdatedAt
	request.ConfirmedAt = row.ConfirmedAt
	return nil
}

// encodeDepositRequestForCreate converts domain.DepositRequest to depositRequestSQL for insertion
func encodeDepositRequestForCreate(request *domain.DepositRequest) *depositRequestSQL {
	var addressAssignmentID *uint64
	if request.AddressAssigmentID != nil {
		id := request.AddressAssigmentID.Uint64()
		addressAssignmentID = &id
	}

	var blockchainTxID *uint64
	if request.BlockchainTxID != nil {
		id := request.BlockchainTxID.Uint64()
		blockchainTxID = &id
	}

	createdAt := request.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return &depositRequestSQL{
		UserID:             request.UserID.Uint64(),
		AssetCode:          request.AssetCode.String(),
		Network:            request.Network.Uint8(),
		AddressAssigmentID: addressAssignmentID,
		Amount:             request.Amount,
		Fee:                request.Fee,
		BlockchainTxID:     blockchainTxID,
		Status:             request.Status.Uint8(),
		CreatedAt:          createdAt,
		Description:        request.Description,
		ConfirmedAt:        request.ConfirmedAt,
	}
}

// GetByParams retrieves a deposit request by user ID, asset code, and network
func (r DepositRequestRepo) GetByParams(ctx context.Context, params domain.GetDepositRequestParams) (domain.DepositRequest, error) {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:GetByParams][params: %+v]", params)
	defer tools.EndDBRequestTimer(time.Now())

	var row depositRequestSQL
	query := r.db.WithContext(ctx).Model(&depositRequestSQL{}).Where("asset_code = ? AND network = ?",
		params.AssetCode.String(),
		params.Network.Uint8(),
	)

	if len(params.Statuses) > 0 {
		query = query.Where("status IN ?", params.Statuses)
	}

	if params.AddressAssignmentID != nil {
		query = query.Where("address_assignment_id = ?", params.AddressAssignmentID.Uint64())
	}

	if params.BlockchainTxID != nil {
		query = query.Where("blockchain_tx_id = ?", params.BlockchainTxID.Uint64())
	}

	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByParams][err:%w]", domain.ErrRecordNotFound)
		}
		tools.TrackMetricDBErrorAdd()
		return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByParams][err:%w]", err)
	}

	return row.decode(), nil
}

// GetByBlockchainID retrieves a deposit request by blockchain transaction ID
func (r DepositRequestRepo) GetByBlockchainID(ctx context.Context, blockchainTxID domain.ID) (domain.DepositRequest, error) {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:GetByBlockchainID][blockchainTxID: %d]", blockchainTxID)
	defer tools.EndDBRequestTimer(time.Now())

	var row depositRequestSQL
	if err := r.db.WithContext(ctx).
		Where("blockchain_tx_id = ?", blockchainTxID.Uint64()).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByBlockchainID][err:%w]", domain.ErrRecordNotFound)
		}

		tools.TrackMetricDBErrorAdd()
		return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByBlockchainID][err:%w]", err)
	}

	return row.decode(), nil
}

// UpdateStatus updates the status of a deposit request
func (r DepositRequestRepo) UpdateStatus(ctx context.Context, id domain.ID, params domain.UpdateDepositRequestStatusParams) error {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:UpdateStatus][id: %d][params: %+v]", id, params)
	defer tools.EndDBRequestTimer(time.Now())

	result := r.db.WithContext(ctx).
		Model(&depositRequestSQL{}).
		Where("id = ?", id.Uint64()).
		Updates(buildDepositStatusUpdateData(params))
	if result.Error != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[DepositRequestRepo:UpdateStatus][err:%w]", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("[DepositRequestRepo:UpdateStatus][err:%w]", domain.ErrRecordNotFound)
	}

	return nil
}

// buildDepositStatusUpdateData builds the update data for the deposit status update.
func buildDepositStatusUpdateData(params domain.UpdateDepositRequestStatusParams) map[string]interface{} {
	updateData := map[string]interface{}{
		"status":     params.Status.Uint8(),
		"updated_at": time.Now().UTC(),
	}
	if params.Description != nil {
		updateData["description"] = params.Description
	}
	if params.Status == domain.DepositStatusSuccess {
		confirmedAt := params.ConfirmedAt
		if confirmedAt == nil {
			now := time.Now().UTC()
			confirmedAt = &now
		}
		updateData["confirmed_at"] = *confirmedAt
	}
	return updateData
}

// GetByID retrieves a deposit request by its ID
func (r DepositRequestRepo) GetByID(ctx context.Context, id domain.ID) (domain.DepositRequest, error) {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:GetByID][id: %d]", id)
	defer tools.EndDBRequestTimer(time.Now())

	var row depositRequestSQL
	if err := r.db.WithContext(ctx).
		Where("id = ?", id.Uint64()).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByID][err:%w]", domain.ErrRecordNotFound)
		}

		tools.TrackMetricDBErrorAdd()
		return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByID][err:%w]", err)
	}

	return row.decode(), nil
}

// GetByIDForUpdate retrieves a deposit request by its ID with a row-level lock for update.
func (r DepositRequestRepo) GetByIDForUpdate(ctx context.Context, id domain.ID) (domain.DepositRequest, error) {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:GetByIDForUpdate][id: %d]", id)
	defer tools.EndDBRequestTimer(time.Now())

	var row depositRequestSQL
	if err := r.db.WithContext(ctx).
		Model(&depositRequestSQL{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id.Uint64()).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByIDForUpdate][err:%w]", domain.ErrRecordNotFound)
		}

		tools.TrackMetricDBErrorAdd()
		return domain.DepositRequest{}, fmt.Errorf("[DepositRequestRepo:GetByIDForUpdate][err:%w]", err)
	}

	return row.decode(), nil
}

// DepositList returns a filtered deposit list for the user.
func (r DepositRequestRepo) DepositList(ctx context.Context, userID domain.ID, filter domain.DepositFilter) ([]domain.DepositRequestDetails, int64, error) {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:DepositList][userID: %d][filter: %+v]", userID, filter)
	defer tools.EndDBRequestTimer(time.Now())
	var (
		rows  []depositRequestDetailsSQL
		total int64
	)

	query := r.db.WithContext(ctx).
		Distinct("d.id").
		Select("d.id", "d.user_id", "d.asset_code", "d.network", "d.amount", "d.status", "d.description", "d.created_at", "a.address").
		Table("deposit_requests AS d").
		Where("d.user_id = ?", userID.Uint64()).
		Joins("LEFT JOIN address_assignments AS a ON d.address_assignment_id = a.id").
		Order("d.id DESC")

	if filter.AssetCode != nil {
		query = query.Where("d.asset_code = ?", strings.ToUpper(*filter.AssetCode))
	}

	if filter.Status != nil {
		query = query.Where("d.status = ?", filter.Status.Uint8())
	}

	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("[DepositRequestRepo:DepositList][err: %w]", err)
	}

	query = query.
		Limit(filter.Limit).
		Offset(filter.Offset)

	if err := query.Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, 0, fmt.Errorf("[DepositRequestRepo:DepositList][err: %w]", err)
	}

	deposits := make([]domain.DepositRequestDetails, 0, len(rows))
	for _, row := range rows {
		deposits = append(deposits, row.decode())
	}

	return deposits, total, nil
}

// GetManualApprove returns a manual approve deposit list for the user.
func (r DepositRequestRepo) GetManualApprove(ctx context.Context, userID domain.ID) ([]domain.DepositRequest, error) {
	logger.LogCtx(ctx).Debugf("[DepositRequestRepo:GetManualApprove][userID: %d]", userID)
	defer tools.EndDBRequestTimer(time.Now())

	var rows []depositRequestSQL
	if err := r.db.WithContext(ctx).Where("user_id = ? AND status = ?", userID.Uint64(), domain.DepositStatusManualReview).Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[DepositRequestRepo:GetManualApprove][err: %w]", err)
	}

	depositRequests := make([]domain.DepositRequest, 0, len(rows))
	for _, row := range rows {
		depositRequests = append(depositRequests, row.decode())
	}

	return depositRequests, nil
}

// decode converts depositRequestDetailsSQL to domain.DepositRequestDetails
func (t depositRequestDetailsSQL) decode() domain.DepositRequestDetails {
	return domain.DepositRequestDetails{
		ID:          domain.NewIDFromUint64(t.ID),
		UserID:      domain.NewIDFromUint64(t.UserID),
		AssetCode:   currency.NewAssetCode(t.AssetCode),
		Amount:      t.Amount,
		Fee:         t.Fee,
		Network:     domain.NewChainNetwork(t.Network),
		Address:     t.Address,
		Status:      domain.NewDepositStatus(t.Status),
		Description: t.Description,
		CreatedAt:   t.CreatedAt,
	}
}
