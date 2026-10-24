package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

type accountSQL struct {
	ID           uint64          `gorm:"column:id"`
	OwnerID      *uint64         `gorm:"column:owner_id"`
	AssetCode    string          `gorm:"column:asset_code"`
	Type         uint8           `gorm:"column:acct_type"`
	Balance      decimal.Decimal `gorm:"column:balance"`
	SecurityHash string          `gorm:"column:security_hash"`
	CreatedAt    time.Time       `gorm:"column:created_at"`
	UpdatedAt    *time.Time      `gorm:"column:updated_at"`
}

// TableName sets the insert table name for this struct type
func (accountSQL) TableName() string {
	return "accounts"
}

// decode converts eventSQL to domain.Account
func (u accountSQL) decode() domain.Account {
	return domain.Account{
		ID:                  domain.NewIDFromUint64(u.ID),
		OwnerID:             domain.NewNullableIDFromUint64(u.OwnerID),
		AssetCode:           currency.NewAssetCode(u.AssetCode),
		Type:                domain.NewAccountType(u.Type),
		Balance:             u.Balance,
		IntegrityCheckValue: u.SecurityHash,
		UpdatedAt:           u.UpdatedAt,
		CreatedAt:           u.CreatedAt,
	}
}

type AccountRepo struct {
	db *gorm.DB
}

// NewAccountRepository creates a new instance of AccountRepo
func NewAccountRepository(db *gorm.DB) *AccountRepo {
	return &AccountRepo{db: db}
}

// WithTxFromContext returns a new AccountRepo with the transaction from the context
func (r AccountRepo) WithTxFromContext(ctx context.Context) domain.AccountRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// GetForUpdate returns a single account matching the owner, account_type and purpose, locking the row for update.
func (r AccountRepo) GetForUpdate(ctx context.Context, params domain.GetForUpdateParams) (domain.Account, error) {
	logger.LogCtx(ctx).Debugf("[AccountRepo:GetForUpdate][params: %+v]", params)
	defer tools.EndDBRequestTimer(time.Now())

	query := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("asset_code = ? AND acct_type = ?", params.Asset.String(), params.Type.Uint8())

	if params.OwnerID == 0 {
		query = query.Where("owner_id IS NULL")
	} else {
		query = query.Where("owner_id = ?", params.OwnerID.Uint64())
	}

	var row accountSQL
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Account{}, fmt.Errorf("[AccountRepo:GetByOwnerPurposeForUpdate][err: %w]", domain.ErrRecordNotFound)
		}
		tools.TrackMetricDBErrorAdd()
		return domain.Account{}, fmt.Errorf("[AccountRepo:GetByOwnerPurposeForUpdate][err: %w]", err)
	}

	return row.decode(), nil
}

// CreateBatch creates multiple accounts in a single batch operation if account exists it won't be created again
func (r AccountRepo) CreateBatch(ctx context.Context, accounts []domain.Account) error {
	logger.LogCtx(ctx).Debugf("[AccountRepo:CreateBatch][accounts: %+v]", accounts)
	defer tools.EndDBRequestTimer(time.Now())

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(encodeAccountsForCreateBatch(accounts)).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[AccountRepo:CreateBatch][err: %w]", err)
	}

	return nil
}

// encodeAccountsForCreateBatch encodes a slice of domain.Account to a slice of accountSQL for batch creation
func encodeAccountsForCreateBatch(accounts []domain.Account) []accountSQL {
	rows := make([]accountSQL, len(accounts))
	for i, acct := range accounts {
		var ownerID *uint64
		if acct.OwnerID != nil {
			oid := acct.OwnerID.Uint64()
			ownerID = &oid
		}

		rows[i] = accountSQL{
			OwnerID:      ownerID,
			AssetCode:    acct.AssetCode.String(),
			Type:         acct.Type.Uint8(),
			Balance:      acct.Balance,
			SecurityHash: acct.IntegrityCheckValue,
			CreatedAt:    time.Now().UTC(),
		}
	}
	return rows
}

// UpdateBalance updates the account balance and security hash if the expected hash matches the current one.
func (r AccountRepo) UpdateBalance(ctx context.Context, params domain.UpdateBalanceParams) error {
	logger.LogCtx(ctx).Debugf("[AccountRepo:UpdateBalance][accountID: %d][newBalance: %s]", params.AccountID, params.NewBalance.String())
	defer tools.EndDBRequestTimer(time.Now())

	result := r.db.WithContext(ctx).
		Model(&accountSQL{}).
		Where("id = ? AND security_hash = ?", params.AccountID.Uint64(), params.ExpectedHash).
		Updates(map[string]any{
			"balance":       params.NewBalance,
			"security_hash": params.NewHash,
			"updated_at":    time.Now().UTC(),
		})
	if result.Error != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[AccountRepo:AddToBalance][err: %w]", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("[AccountRepo:AddToBalance][err: %w]", domain.ErrRecordNotFound)
	}

	return nil
}

// GetBalancesByOwnerID retrieves all account balances for a given owner ID.
func (r AccountRepo) GetBalancesByOwnerID(ctx context.Context, ownerID *domain.ID) (domain.GetBalancesResult, error) {
	logger.LogCtx(ctx).Debugf("[AccountRepo:GetBalancesByOwnerID][ownerID: %+v]", ownerID)
	defer tools.EndDBRequestTimer(time.Now())

	query := r.db.WithContext(ctx).Model(&accountSQL{})
	if ownerID == nil {
		query = query.Where("owner_id IS NULL")
	} else {
		query = query.Where("owner_id = ?", ownerID.Uint64())
	}

	var rows []accountSQL
	if err := query.Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[AccountRepo:GetBalancesByOwnerID][err: %w]", err)
	}

	result := make(domain.GetBalancesResult)
	for _, row := range rows {
		assetCode := currency.NewAssetCode(row.AssetCode)
		accountType := domain.NewAccountType(row.Type)

		if _, exists := result[assetCode]; !exists {
			result[assetCode] = make(map[domain.AccountType]decimal.Decimal)
		}
		result[assetCode][accountType] = row.Balance
	}

	return result, nil
}

type positiveBalanceRow struct {
	OwnerID   uint64          `gorm:"column:owner_id"`
	AssetCode string          `gorm:"column:asset_code"`
	Type      uint8           `gorm:"column:acct_type"`
	Balance   decimal.Decimal `gorm:"column:balance"`
}

// GetAllPositiveUserBalanceSnapshots retrieves a list of all positive user account balances with owner ID, asset code, account type and balance
func (r AccountRepo) GetAllPositiveUserBalanceSnapshots(ctx context.Context, accTypes []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
	logger.LogCtx(ctx).Debug("[AccountRepo:GetAllPositiveUserBalanceSnapshots]")
	defer tools.EndDBRequestTimer(time.Now())

	var rows []positiveBalanceRow
	if err := r.db.WithContext(ctx).
		Model(&accountSQL{}).
		Select("owner_id, asset_code, acct_type, balance").
		Where(
			"owner_id IS NOT NULL AND balance > 0 AND acct_type IN ?",
			accTypes,
		).
		Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[AccountRepo:GetAllPositiveUserBalanceSnapshots][err: %w]", err)
	}

	out := make([]domain.PositiveBalanceSnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.PositiveBalanceSnapshot{
			OwnerID:     domain.NewIDFromUint64(row.OwnerID),
			AssetCode:   currency.NewAssetCode(row.AssetCode),
			AccountType: domain.NewAccountType(row.Type),
			Balance:     row.Balance,
		})
	}
	return out, nil
}

// GetBalance retrieves the balance for a specific account based on the provided parameters.
//
// A missing account reads as a zero balance, and that is deliberate. Accounts
// are created on demand — by the user.created consumer, and lazily by
// TransactionCtrl.lockOrCreateAndLockAccount — so a user whose row does not
// exist yet has no funds, which is exactly what a zero answers. Both callers
// use this to decide whether a withdrawal or transfer can be afforded, and both
// map any error to DatabaseError, so reporting "not found" here would turn a
// legitimate state into a 500 on two money paths.
//
// Scan is what produces that zero: unlike First, it does not report
// gorm.ErrRecordNotFound for an empty result. Do not switch it to First, and do
// not add not-found handling back, without changing the callers first.
func (r AccountRepo) GetBalance(ctx context.Context, params domain.GetBalanceParams) (decimal.Decimal, error) {
	logger.LogCtx(ctx).Debugf("[AccountRepo:GetBalance][params: %+v]", params)
	defer tools.EndDBRequestTimer(time.Now())

	query := r.db.WithContext(ctx).
		Model(&accountSQL{}).
		Select("balance").
		Where("asset_code = ? AND acct_type = ?", params.AssetCode.String(), params.AccountType.Uint8())

	if params.OwnerID == nil {
		query = query.Where("owner_id IS NULL")
	} else {
		query = query.Where("owner_id = ?", params.OwnerID.Uint64())
	}

	var balance decimal.Decimal
	if err := query.Scan(&balance).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return decimal.Zero, fmt.Errorf("[AccountRepo:GetBalance][err: %w]", err)
	}

	return balance, nil
}

// GetByIDsForUpdate retrieves accounts by IDs and locks them for update.
//
// Callers must reach this only through a path that already fixed the lock
// order — locking by id here is a different order from the one
// controllers.sortAccountLookups establishes, and mixing the two deadlocks.
func (r AccountRepo) GetByIDsForUpdate(ctx context.Context, ids []domain.ID) (map[domain.ID]domain.Account, error) {
	logger.LogCtx(ctx).Debugf("[AccountRepo:GetByIDsForUpdate][ids: %+v]", ids)
	defer tools.EndDBRequestTimer(time.Now())

	return r.getByIDs(ctx, ids, true, "GetByIDsForUpdate")
}

// GetByIDs retrieves accounts by IDs without locking them.
//
// It exists so a caller holding only account ids can learn their
// (owner, asset, type) keys and then take the locks in the service-wide order.
// Reading unlocked is safe for that purpose: accounts are never deleted, so a
// row found here still exists when the lock is taken, and every value the
// caller goes on to use must come from the locked read rather than from here.
func (r AccountRepo) GetByIDs(ctx context.Context, ids []domain.ID) (map[domain.ID]domain.Account, error) {
	logger.LogCtx(ctx).Debugf("[AccountRepo:GetByIDs][ids: %+v]", ids)
	defer tools.EndDBRequestTimer(time.Now())

	return r.getByIDs(ctx, ids, false, "GetByIDs")
}

// getByIDs backs both the locking and non-locking lookups so they cannot drift
// in how they validate a partial result.
func (r AccountRepo) getByIDs(ctx context.Context, ids []domain.ID, forUpdate bool, scope string) (map[domain.ID]domain.Account, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("[AccountRepo:%s][err: no IDs provided]", scope)
	}

	idsUint := make([]uint64, 0, len(ids))
	for i := range ids {
		idsUint = append(idsUint, ids[i].Uint64())
	}
	sort.Slice(idsUint, func(i, j int) bool { return idsUint[i] < idsUint[j] })

	query := r.db.WithContext(ctx)
	if forUpdate {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}

	var rows []accountSQL
	if err := query.
		Where("id IN ?", idsUint).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[AccountRepo:%s][err: %w]", scope, err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("[AccountRepo:%s][err: no accounts found]", scope)
	}

	if len(rows) != len(idsUint) {
		return nil, fmt.Errorf("[AccountRepo:%s][err: some accounts not found]", scope)
	}

	accounts := make(map[domain.ID]domain.Account, len(rows))
	for _, row := range rows {
		account := row.decode()
		accounts[account.ID] = account
	}

	return accounts, nil
}
