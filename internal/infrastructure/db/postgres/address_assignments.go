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
	"github.com/whagency/core-common/services/currency"
	"gorm.io/gorm"
)

type addressAssignmentSQL struct {
	ID        uint64     `gorm:"column:id"`
	OwnerID   *uint64    `gorm:"column:owner_id"`
	AssetCode string     `gorm:"column:asset_code"`
	Network   uint8      `gorm:"column:network"`
	Purpose   uint8      `gorm:"column:purpose"`
	Address   string     `gorm:"column:address"`
	Status    uint8      `gorm:"column:status"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt *time.Time `gorm:"column:updated_at"`
}

// TableName sets the insert table name for this struct type
func (addressAssignmentSQL) TableName() string {
	return "address_assignments"
}

// decode converts addressAssignmentSQL to domain.AddressAssignment
func (a addressAssignmentSQL) decode() domain.AddressAssignment {
	var ownerID *domain.ID
	if a.OwnerID != nil {
		oid := domain.NewIDFromUint64(*a.OwnerID)
		ownerID = &oid
	}

	return domain.AddressAssignment{
		ID:        domain.NewIDFromUint64(a.ID),
		OwnerID:   ownerID,
		AssetCode: currency.NewAssetCode(a.AssetCode),
		Network:   domain.NewChainNetwork(a.Network),
		Purpose:   domain.NewPurposeAddress(a.Purpose),
		Address:   a.Address,
		Status:    domain.NewAddressStatus(a.Status),
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

type AddressAssignmentRepo struct {
	db *gorm.DB
}

// NewAddressAssignmentRepository creates a new address assignment repository
func NewAddressAssignmentRepository(db *gorm.DB) *AddressAssignmentRepo {
	return &AddressAssignmentRepo{db: db}
}

// WithTxFromContext returns a new CredentialRepo with the transaction from the context
func (r AddressAssignmentRepo) WithTxFromContext(ctx context.Context) domain.AddressAssignmentRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// Create creates a new address assignment record in the database
func (r AddressAssignmentRepo) Create(ctx context.Context, assignment *domain.AddressAssignment) error {
	logger.LogCtx(ctx).Debugf("[CredentialRepo:Create][assignment: %+v]", assignment)
	defer tools.EndDBRequestTimer(time.Now())

	row := encodeAddressAssignmentForCreate(assignment)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[AddressAssignmentRepo:Create] %w", err)
	}

	assignment.ID = domain.NewIDFromUint64(row.ID)
	return nil
}

// encodeAddressAssignmentForCreate converts domain.AddressAssignment to addressAssignmentSQL for insertion
func encodeAddressAssignmentForCreate(assignment *domain.AddressAssignment) *addressAssignmentSQL {
	var ownerID *uint64
	if assignment.OwnerID != nil {
		oid := assignment.OwnerID.Uint64()
		ownerID = &oid
	}

	return &addressAssignmentSQL{
		OwnerID:   ownerID,
		AssetCode: assignment.AssetCode.String(),
		Network:   assignment.Network.Uint8(),
		Purpose:   assignment.Purpose.Uint8(),
		Address:   assignment.Address,
		Status:    assignment.Status.Uint8(),
		CreatedAt: time.Now().UTC(),
	}
}

// GetActive retrieves an active address assignment based on the provided parameters
func (r AddressAssignmentRepo) GetActive(ctx context.Context, params domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
	logger.LogCtx(ctx).Debugf("[AddressAssignmentRepo:GetActive][params: %+v]", params)
	defer tools.EndDBRequestTimer(time.Now())

	var assignmentSQL addressAssignmentSQL
	query := r.db.WithContext(ctx).Model(&addressAssignmentSQL{}).Where("asset_code = ? AND network = ? AND purpose = ? AND (status = ? OR status = ?)",
		params.AssetCode.String(),
		params.Network.Uint8(),
		params.Purpose.Uint8(),
		domain.AddressStatusAllocated.Uint8(),
		domain.AddressStatusInUse.Uint8(),
	)

	if params.OwnerID != nil {
		ownerID := params.OwnerID.Uint64()
		query = query.Where("owner_id = ?", ownerID)
	} else if params.IsSystem {
		query = query.Where("owner_id IS NULL")
	}

	if params.Address != nil {
		query = query.Where("address = ?", *params.Address)
	}

	if err := query.First(&assignmentSQL).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.AddressAssignment{}, fmt.Errorf("[AddressAssignmentRepo:GetActive][err:%w]", domain.ErrRecordNotFound)
		}
		tools.TrackMetricDBErrorAdd()
		return domain.AddressAssignment{}, fmt.Errorf("[AddressAssignmentRepo:GetActive][err:%w]", err)
	}

	return assignmentSQL.decode(), nil
}

// UpdateStatus updates the status of an address assignment
func (r AddressAssignmentRepo) UpdateStatus(ctx context.Context, id domain.ID, status domain.AddressStatus) error {
	logger.LogCtx(ctx).Debugf("[AddressAssignmentRepo:UpdateStatus][id: %d][status: %s]", id, status.String())
	defer tools.EndDBRequestTimer(time.Now())

	updatedData := map[string]interface{}{
		"status":     status.Uint8(),
		"updated_at": time.Now().UTC(),
	}
	if err := r.db.WithContext(ctx).Model(&addressAssignmentSQL{}).
		Where("id = ?", id.Uint64()).
		Updates(updatedData).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[AddressAssignmentRepo:UpdateStatus][err:%w]", err)
	}

	return nil
}
