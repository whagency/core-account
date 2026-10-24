package controllers

import (
	"context"
	stdErrors "errors"
	"fmt"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
)

type AccountCtrl struct {
	cacheCtrl domain.CacheController

	accounts         domain.AccountRepository
	balanceHistories domain.BalanceHistoryRepository

	dbTransactionManager domain.TransactionManager
	integrity            domain.AccountIntegrityService
}

func NewAccountController(
	cacheCtrl domain.CacheController,

	accounts domain.AccountRepository,
	balanceHistories domain.BalanceHistoryRepository,

	integrity domain.AccountIntegrityService,
	dbTransactionManager domain.TransactionManager,

) *AccountCtrl {
	return &AccountCtrl{
		cacheCtrl: cacheCtrl,

		accounts:         accounts,
		balanceHistories: balanceHistories,

		integrity:            integrity,
		dbTransactionManager: dbTransactionManager,
	}
}

// CreateDefaultAccountsForUser creates all default accounts for a new user
func (c AccountCtrl) CreateDefaultAccountsForUser(ctx context.Context, userID domain.ID) error {
	logger.LogCtx(ctx).Debugf("[AccountCtrl:CreateDefaultAccountsForUser][userID: %d]", userID)

	accounts := domain.NewDefaultAccountsForUser(userID)
	for i := range accounts {
		signature, errIntegrity := c.integrity.Compute(accounts[i], decimal.Zero)
		if errIntegrity != nil {
			return svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[AccountCtrl:CreateDefaultAccountsForUser]%w", errIntegrity)),
			)
		}
		accounts[i].IntegrityCheckValue = signature
	}

	var err error
	ctx = c.dbTransactionManager.Begin(ctx)

	defer func() {
		if err != nil {
			logger.LogCtx(ctx).Debugf("[AccountCtrl:CreateDefaultAccountsForUser][Rollback]%s", err)
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[AccountCtrl:CreateDefaultAccountsForUser][Rollback]%s", errRollback)
			}
		}
	}()

	// Create Accounts
	if err = c.accounts.WithTxFromContext(ctx).CreateBatch(ctx, accounts); err != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:CreateDefaultAccountsForUser]%w", err)),
		)
		return err
	}

	// Commit Transaction
	if err = c.dbTransactionManager.Commit(ctx); err != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:CreateDefaultAccountsForUser][Commit]%w", err)),
		)
		return err
	}

	return nil
}

// CreateDefaultSystemAccounts creates default system accounts
func (c AccountCtrl) CreateDefaultSystemAccounts(ctx context.Context) error {
	logger.LogCtx(ctx).Debugf("[AccountCtrl:CreateDefaultSystemAccounts]")

	accounts := domain.NewDefaultSystemAccounts()
	for i := range accounts {
		signature, errIntegrity := c.integrity.Compute(accounts[i], decimal.Zero)
		if errIntegrity != nil {
			return svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[AccountCtrl:CreateDefaultSystemAccounts]%w", errIntegrity)),
			)
		}
		accounts[i].IntegrityCheckValue = signature
	}

	var err error
	ctx = c.dbTransactionManager.Begin(ctx)

	defer func() {
		if err != nil {
			logger.LogCtx(ctx).Debugf("[AccountCtrl:CreateDefaultSystemAccounts][Rollback]%s", err)
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[AccountCtrl:CreateDefaultSystemAccounts][Rollback]%s", errRollback)
			}
		}
	}()

	// Create Accounts
	if err = c.accounts.WithTxFromContext(ctx).CreateBatch(ctx, accounts); err != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:CreateDefaultSystemAccounts]%w", err)),
		)
		return err
	}

	// Commit Transaction
	if err = c.dbTransactionManager.Commit(ctx); err != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:CreateDefaultSystemAccounts][Commit]%w", err)),
		)
		return err
	}

	return nil
}

// CreateAccount creates a single account for a user
func (c AccountCtrl) CreateAccount(ctx context.Context, userID *domain.ID, assetCode currency.AssetCode, accountType domain.AccountType) error {
	logger.LogCtx(ctx).Debugf("[AccountCtrl:CreateAccount][userID: %d][assetCode: %s][accountType: %d]", userID, assetCode.String(), accountType)

	// Create Account
	if _, errCreate := c.createAccount(ctx, domain.NewAccount(userID, assetCode, accountType)); errCreate != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:CreateAccount]%w", errCreate.Cause)),
		)
	}

	return nil
}

// GetBalancesByOwnerID retrieves all account balances for a given owner ID
func (c AccountCtrl) GetBalancesByOwnerID(ctx context.Context, ownerID *domain.ID) (domain.GetBalancesResult, error) {
	logger.LogCtx(ctx).Debugf("[AccountCtrl:GetBalancesByOwnerID][ownerID: %v]", ownerID)

	balances, err := c.accounts.GetBalancesByOwnerID(ctx, ownerID)
	if err != nil {
		if stdErrors.Is(err, domain.ErrRecordNotFound) {
			return nil, svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[AccountCtrl:GetBalancesByOwnerID]%w", err)),
			)
		}

		return nil, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:GetBalancesByOwnerID]%w", err)),
		)
	}

	return balances, nil
}

// GetBalance retrieves the balance for a specific account.
//
// The balance is returned quantized, exactly as stored: `accounts.balance` is
// NUMERIC(38,0) and holds base units scaled by currency.DefaultAssetScale.
// Callers compare it against a quantized amount — Account.GetBalanceInBaseUnits
// converts for display, and the HTTP handlers do that at the transport
// boundary, which is where every other unit conversion in this service lives.
//
// Nothing here converts, on purpose. Both operands of a balance check are
// decimal.Decimal, so nothing but the unit convention keeps them comparable,
// and a conversion in the wrong direction fails permissively: an inflated
// balance passes a "do you have enough" check instead of failing it.
func (c AccountCtrl) GetBalance(ctx context.Context, params domain.GetBalanceParams) (decimal.Decimal, error) {
	logger.LogCtx(ctx).Debugf("[AccountCtrl:GetBalance][params: %+v]", params)

	balance, err := c.accounts.GetBalance(ctx, params)
	if err != nil {
		if stdErrors.Is(err, domain.ErrRecordNotFound) {
			return decimal.Zero, svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[AccountCtrl:GetBalance]%w", err)),
			)
		}

		return decimal.Zero, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:GetBalance]%w", err)),
		)
	}

	return balance, nil
}

// GetPnlByOwnerID retrieves all account pnl for a given user ID
func (c AccountCtrl) GetPnlByOwnerID(ctx context.Context, userID *domain.ID) (domain.GetPnlResult, error) {
	logger.LogCtx(ctx).Debugf("[AccountCtrl:GetPnlByOwnerID][userID: %v]", userID)

	result := domain.GetPnlResult{
		PnlTotalAmount:  decimal.Zero,
		PnlTotalPercent: "0",
		PnlAssets:       make(map[currency.AssetCode]domain.AssetPnlResult),
	}

	today, todayError := c.balanceHistories.FindTodayStats(ctx, userID)
	if todayError != nil {
		return domain.GetPnlResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:GetPnlByOwnerID]%w", todayError)),
		)
	}

	yesterday, yesterdayError := c.balanceHistories.FindYesterdayStats(ctx, userID)
	if yesterdayError != nil {
		return domain.GetPnlResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:GetPnlByOwnerID]%w", yesterdayError)),
		)
	}

	yesterdayMap := make(map[currency.AssetCode]domain.BalanceHistory)
	for _, y := range yesterday {
		yesterdayMap[y.AssetCode] = y
	}

	totalYesterdayUSD := decimal.Zero
	totalPNLAmount := decimal.Zero

	for _, t := range today {
		todayUSD := t.Amount.Mul(t.Rate)

		yesterdayUSD := decimal.Zero
		if y, exists := yesterdayMap[t.AssetCode]; exists {
			yesterdayUSD = y.Amount.Mul(y.Rate)
		}

		// PNL Amount
		pnlAmount := todayUSD.Sub(yesterdayUSD)

		// PNL Percent
		pnlPercent := decimal.Zero
		if !yesterdayUSD.IsZero() {
			pnlPercent = pnlAmount.Div(yesterdayUSD).Mul(decimal.NewFromInt(100))
		}

		// Save Asset
		result.PnlAssets[t.AssetCode] = domain.AssetPnlResult{
			PnlAmount:  pnlAmount,
			PnlPercent: pnlPercent.StringFixed(2) + "%",
		}

		// Count Total
		totalPNLAmount = totalPNLAmount.Add(pnlAmount)
		totalYesterdayUSD = totalYesterdayUSD.Add(yesterdayUSD)
	}

	// Total PNL
	result.PnlTotalAmount = totalPNLAmount

	if !totalYesterdayUSD.IsZero() {
		totalPercent := totalPNLAmount.Div(totalYesterdayUSD).Mul(decimal.NewFromInt(100))
		result.PnlTotalPercent = totalPercent.StringFixed(2) + "%"
	} else {
		result.PnlTotalPercent = "0%"
	}

	return result, nil
}

// UpdatePnlStat updates pnl stat for all users with positive balance (owner_id IS NOT NULL) and writes it to balance history table
func (c AccountCtrl) UpdatePnlStat(ctx context.Context) error {
	logger.LogCtx(ctx).Debug("[AccountCtrl:UpdatePnlStat]")

	snapshots, err := c.accounts.GetAllPositiveUserBalanceSnapshots(ctx, []domain.AccountType{domain.AccountTypeLiability, domain.AccountTypeHold})
	if err != nil {
		return fmt.Errorf("[AccountCtrl:UpdatePnlStat]%w", err)
	}

	assetsData, errAsset := c.cacheCtrl.GetAllAssets(ctx)
	if errAsset != nil {
		if er, ok := appErrors.AsErrorResponse(errAsset); ok {
			er.SetCause(fmt.Errorf("[AccountCtrl:UpdatePnlStat]%w", er.Cause))
			return er
		}
		return svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:UpdatePnlStat]%w", errAsset)),
		)
	}

	type key struct {
		userID domain.ID
		asset  currency.AssetCode
	}

	sumByKey := make(map[key]decimal.Decimal, len(snapshots))
	skippedNoAsset := 0

	for _, s := range snapshots {
		if _, ok := assetsData[s.AssetCode]; !ok {
			skippedNoAsset++
			logger.LogCtx(ctx).Errorf("[AccountCtrl:UpdatePnlStat][err: asset %s not found]", s.AssetCode)
			continue
		}

		k := key{userID: s.OwnerID, asset: s.AssetCode}
		sumByKey[k] = sumByKey[k].Add(s.GetBalanceInBaseUnits())
	}

	histories := make([]domain.BalanceHistory, 0, len(sumByKey))
	for k, amount := range sumByKey {
		assetData, ok := assetsData[k.asset]
		if !ok {
			continue
		}

		histories = append(histories, domain.NewBalanceHistory(
			k.userID,
			k.asset,
			amount,
			assetData.USDPrice,
		))
	}

	if err = c.balanceHistories.UpsertTodayStats(ctx, histories); err != nil {
		return fmt.Errorf("[AccountCtrl:UpdatePnlStat]%w", err)
	}

	logger.LogCtx(ctx).Debugf(
		"[AccountCtrl:UpdatePnlStat][msg: done snapshots=%d, upserted=%d, skipped_no_asset=%d]",
		len(snapshots), len(histories), skippedNoAsset,
	)

	return nil
}

// createAccount creates a new account in the database
func (c AccountCtrl) createAccount(ctx context.Context, account domain.Account) (domain.Account, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[AccountCtrl:createAccount][account: %+v]", account)

	signature, errIntegrity := c.integrity.Compute(account, decimal.Zero)
	if errIntegrity != nil {
		return domain.Account{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[createAccount]%w", errIntegrity)),
		)
	}

	account.IntegrityCheckValue = signature
	if createErr := c.accounts.WithTxFromContext(ctx).CreateBatch(ctx, []domain.Account{account}); createErr != nil {
		return domain.Account{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[createAccount]%w", createErr)),
		)
	}

	return account, nil
}
