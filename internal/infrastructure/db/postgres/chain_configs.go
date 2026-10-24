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

type chainConfigSQL struct {
	ID                        uint64          `gorm:"column:id"`
	AssetCode                 string          `gorm:"column:asset_code"`
	Network                   uint8           `gorm:"column:network"`
	Decimals                  uint8           `gorm:"column:decimals"`
	DustThreshold             decimal.Decimal `gorm:"column:dust_threshold"`
	MinConfirmations          uint8           `gorm:"column:min_confirmations"`
	ConfirmFinality           uint8           `gorm:"column:confirm_finality"`
	AssetName                 string          `gorm:"column:asset_name"`
	BlockUrl                  *string         `gorm:"column:block_url"`
	TxUrl                     *string         `gorm:"column:tx_url"`
	AddressUrl                *string         `gorm:"column:address_url"`
	ContractAddressUrl        *string         `gorm:"column:contract_address_url"`
	AddressRegex              *string         `gorm:"column:address_regex"`
	WithdrawFee               decimal.Decimal `gorm:"column:withdraw_fee"`
	DepositFee                decimal.Decimal `gorm:"column:deposit_fee"`
	MinAmountWithdrawalReview decimal.Decimal `gorm:"column:min_amount_withdrawal_review"`
	WithdrawFeeType           uint8           `gorm:"column:withdraw_fee_type"`
	DepositFeeType            uint8           `gorm:"column:deposit_fee_type"`
	EstimatedArrivalTime      int             `gorm:"column:estimated_arrival_time"`
	Status                    uint8           `gorm:"column:status"`
	CreatedAt                 time.Time       `gorm:"column:created_at"`
	UpdatedAt                 *time.Time      `gorm:"column:updated_at"`
}

// TableName sets the insert table name for this struct type
func (chainConfigSQL) TableName() string {
	return "chain_configs"
}

// decode converts chainConfigSQL to domain.ChainConfig
func (a chainConfigSQL) decode() domain.ChainConfig {
	return domain.ChainConfig{
		ID:                        domain.NewIDFromUint64(a.ID),
		AssetCode:                 currency.NewAssetCode(a.AssetCode),
		Network:                   domain.NewChainNetwork(a.Network),
		Decimals:                  a.Decimals,
		DustThreshold:             a.DustThreshold,
		MinConfirmations:          a.MinConfirmations,
		ConfirmFinality:           a.ConfirmFinality,
		AssetName:                 a.AssetName,
		BlockUrl:                  a.BlockUrl,
		TxUrl:                     a.TxUrl,
		AddressUrl:                a.AddressUrl,
		ContractAddressUrl:        a.ContractAddressUrl,
		AddressRegex:              a.AddressRegex,
		WithdrawFee:               a.WithdrawFee,
		DepositFee:                a.DepositFee,
		MinAmountWithdrawalReview: a.MinAmountWithdrawalReview,
		WithdrawFeeType:           domain.NewFeeType(a.WithdrawFeeType),
		DepositFeeType:            domain.NewFeeType(a.DepositFeeType),
		EstimatedArrivalTime:      a.EstimatedArrivalTime,
		Status:                    domain.NewChainConfigStatus(a.Status),
		CreatedAt:                 a.CreatedAt,
		UpdatedAt:                 a.UpdatedAt,
	}
}

type ChainConfigRepo struct {
	db *gorm.DB
}

// NewAChainConfigRepository creates a new address assignment repository
func NewAChainConfigRepository(db *gorm.DB) *ChainConfigRepo {
	return &ChainConfigRepo{db: db}
}

// WithTxFromContext returns a new CredentialRepo with the transaction from the context
func (r ChainConfigRepo) WithTxFromContext(ctx context.Context) domain.ChainConfigRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// Create creates a new address assignment record in the database
func (r ChainConfigRepo) Create(ctx context.Context, config *domain.ChainConfig) error {
	logger.LogCtx(ctx).Debugf("[ChainConfigRepo:Create][config: %+v]", config)
	defer tools.EndDBRequestTimer(time.Now())

	row := encodeChainConfigForCreate(config)
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[ChainConfigRepo:Create] %w", err)
	}

	config.ID = domain.NewIDFromUint64(row.ID)
	return nil
}

// encodeChainConfigForCreate encodes domain.ChainConfig to chainConfigSQL for creation
func encodeChainConfigForCreate(config *domain.ChainConfig) *chainConfigSQL {
	return &chainConfigSQL{
		AssetCode:                 config.AssetCode.String(),
		Network:                   config.Network.Uint8(),
		MinConfirmations:          config.MinConfirmations,
		Decimals:                  config.Decimals,
		DustThreshold:             config.DustThreshold,
		ConfirmFinality:           config.ConfirmFinality,
		AssetName:                 config.AssetName,
		BlockUrl:                  config.BlockUrl,
		TxUrl:                     config.TxUrl,
		AddressUrl:                config.AddressUrl,
		ContractAddressUrl:        config.ContractAddressUrl,
		AddressRegex:              config.AddressRegex,
		DepositFee:                config.DepositFee,
		WithdrawFee:               config.WithdrawFee,
		DepositFeeType:            config.DepositFeeType.Uint8(),
		WithdrawFeeType:           config.WithdrawFeeType.Uint8(),
		MinAmountWithdrawalReview: config.MinAmountWithdrawalReview,
		Status:                    config.Status.Uint8(),
		EstimatedArrivalTime:      config.EstimatedArrivalTime,
		CreatedAt:                 time.Now().UTC(),
	}
}

// GetAll retrieves all chain configurations from the database
func (r ChainConfigRepo) GetAll(ctx context.Context) (domain.ChainConfigs, error) {
	logger.LogCtx(ctx).Debugf("[ChainConfigRepo:GetAll]")
	defer tools.EndDBRequestTimer(time.Now())

	var rows []chainConfigSQL
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[ChainConfigRepo:GetAll] %w", err)
	}

	configs := make(domain.ChainConfigs)
	for _, row := range rows {
		config := row.decode()
		network := config.Network
		assetCode := config.AssetCode

		if _, ok := configs[assetCode]; !ok {
			configs[assetCode] = make(map[domain.ChainNetwork]domain.ChainConfig)
		}
		configs[assetCode][network] = config
	}

	return configs, nil
}
