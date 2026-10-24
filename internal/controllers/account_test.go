package controllers

import (
	"context"
	"errors"
	"testing"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/testkit/dbtest"
	"github.com/whagency/core-common/testkit/errorxtest"
)

// ============================================================
// Tests — AccountCtrl.CreateDefaultAccountsForUser
// ============================================================

func TestAccountCreateDefaultAccountsForUser_Success_CommitsTransaction(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()

	if err := f.ctrl.CreateDefaultAccountsForUser(context.Background(), domain.NewIDFromUint64(1001)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.txManager.BeginCalls != 1 {
		t.Fatalf("expected 1 Begin, got %d", f.txManager.BeginCalls)
	}
	if f.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 Commit, got %d", f.txManager.CommitCalls)
	}
	if f.txManager.RollbackCalls != 0 {
		t.Fatalf("expected 0 Rollback, got %d", f.txManager.RollbackCalls)
	}
	if f.accounts.createBatchCalls < 1 {
		t.Fatal("expected CreateBatch to be called")
	}
}

func TestAccountCreateDefaultAccountsForUser_IntegrityFails_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.integrity.computeFunc = func(domain.Account, decimal.Decimal) (string, error) {
		return "", errors.New("integrity engine down")
	}

	err := f.ctrl.CreateDefaultAccountsForUser(context.Background(), domain.NewIDFromUint64(1001))
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
	if f.txManager.BeginCalls != 0 {
		t.Fatal("expected no Begin when integrity fails before transaction")
	}
}

func TestAccountCreateDefaultAccountsForUser_CreateBatchFails_ReturnsDatabaseErrorAndRollsBack(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.createBatchFunc = func(context.Context, []domain.Account) error {
		return errors.New("db insert failed")
	}

	err := f.ctrl.CreateDefaultAccountsForUser(context.Background(), domain.NewIDFromUint64(1001))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 Rollback, got %d", f.txManager.RollbackCalls)
	}
	if f.txManager.CommitCalls != 0 {
		t.Fatalf("expected 0 Commit, got %d", f.txManager.CommitCalls)
	}
}

func TestAccountCreateDefaultAccountsForUser_CommitFails_ReturnsDatabaseErrorAndRollsBack(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.txManager.CommitErr = errors.New("commit failed")

	err := f.ctrl.CreateDefaultAccountsForUser(context.Background(), domain.NewIDFromUint64(1001))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 Rollback, got %d", f.txManager.RollbackCalls)
	}
}

// ============================================================
// Tests — AccountCtrl.CreateDefaultSystemAccounts
// ============================================================

func TestAccountCreateDefaultSystemAccounts_Success_CommitsTransaction(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()

	if err := f.ctrl.CreateDefaultSystemAccounts(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 Commit, got %d", f.txManager.CommitCalls)
	}
	if f.txManager.RollbackCalls != 0 {
		t.Fatalf("expected 0 Rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestAccountCreateDefaultSystemAccounts_IntegrityFails_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.integrity.computeFunc = func(domain.Account, decimal.Decimal) (string, error) {
		return "", errors.New("integrity engine down")
	}

	err := f.ctrl.CreateDefaultSystemAccounts(context.Background())
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

func TestAccountCreateDefaultSystemAccounts_CreateBatchFails_ReturnsDatabaseErrorAndRollsBack(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.createBatchFunc = func(context.Context, []domain.Account) error {
		return errors.New("db insert failed")
	}

	err := f.ctrl.CreateDefaultSystemAccounts(context.Background())
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 Rollback, got %d", f.txManager.RollbackCalls)
	}
}

// ============================================================
// Tests — AccountCtrl.CreateAccount
// ============================================================

func TestAccountCreateAccount_Success(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	userID := domain.NewIDFromUint64(42)

	if err := f.ctrl.CreateAccount(context.Background(), &userID, currency.DefaultAccountAssetCode, domain.AccountTypeLiability); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.accounts.createBatchCalls != 1 {
		t.Fatalf("expected 1 CreateBatch call, got %d", f.accounts.createBatchCalls)
	}
}

func TestAccountCreateAccount_IntegrityFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.integrity.computeFunc = func(domain.Account, decimal.Decimal) (string, error) {
		return "", errors.New("integrity down")
	}
	userID := domain.NewIDFromUint64(42)

	err := f.ctrl.CreateAccount(context.Background(), &userID, currency.DefaultAccountAssetCode, domain.AccountTypeLiability)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestAccountCreateAccount_CreateBatchFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.createBatchFunc = func(context.Context, []domain.Account) error {
		return errors.New("db error")
	}
	userID := domain.NewIDFromUint64(42)

	err := f.ctrl.CreateAccount(context.Background(), &userID, currency.DefaultAccountAssetCode, domain.AccountTypeLiability)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// Tests — AccountCtrl.GetBalancesByOwnerID
// ============================================================

func TestAccountGetBalancesByOwnerID_Success_ReturnsBalances(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	want := domain.GetBalancesResult{
		currency.DefaultAccountAssetCode: {
			domain.AccountTypeLiability: decimal.NewFromInt(500),
		},
	}
	f.accounts.getBalancesByOwnerIDFunc = func(context.Context, *domain.ID) (domain.GetBalancesResult, error) {
		return want, nil
	}
	ownerID := domain.NewIDFromUint64(1)

	got, err := f.ctrl.GetBalancesByOwnerID(context.Background(), &ownerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[currency.DefaultAccountAssetCode][domain.AccountTypeLiability].Cmp(decimal.NewFromInt(500)) != 0 {
		t.Fatalf("unexpected balance: %s", got[currency.DefaultAccountAssetCode][domain.AccountTypeLiability])
	}
}

func TestAccountGetBalancesByOwnerID_NotFound_ReturnsRecordNotFoundError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.getBalancesByOwnerIDFunc = func(context.Context, *domain.ID) (domain.GetBalancesResult, error) {
		return nil, domain.ErrRecordNotFound
	}
	ownerID := domain.NewIDFromUint64(1)

	_, err := f.ctrl.GetBalancesByOwnerID(context.Background(), &ownerID)
	errorxtest.AssertErrorCode(t, err, appErrors.RecordNotFoundError)
}

func TestAccountGetBalancesByOwnerID_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.getBalancesByOwnerIDFunc = func(context.Context, *domain.ID) (domain.GetBalancesResult, error) {
		return nil, errors.New("connection lost")
	}
	ownerID := domain.NewIDFromUint64(1)

	_, err := f.ctrl.GetBalancesByOwnerID(context.Background(), &ownerID)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// Tests — AccountCtrl.GetBalance
// ============================================================

// TestAccountGetBalance_ReturnsStoredValueUnchanged pins the unit contract.
//
// GetBalance must hand back exactly what the repository read. It used to shift
// the value up by the asset scale, which inflated every balance by 10^scale and
// made the callers' "do you have enough" checks pass for any non-zero balance.
// Converting here is wrong in either direction: the stored value is already the
// unit every caller compares against, and display conversion belongs at the
// transport boundary.
// quantizedBalance builds a balance in the unit AccountCtrl.GetBalance returns:
// accounts.balance is NUMERIC(38,0) holding base units scaled by
// currency.DefaultAssetScale, so a stub that returns a bare integer is stating
// a balance of that many *quants*, not that many USDT.
func quantizedBalance(baseUnits int64) decimal.Decimal {
	return decimal.NewFromInt(baseUnits).Shift(currency.DefaultAssetScale)
}

func TestAccountGetBalance_ReturnsStoredValueUnchanged(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	// What the column holds for 100 USDT at scale 8.
	stored := quantizedBalance(100)
	f.accounts.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return stored, nil
	}
	ownerID := domain.NewIDFromUint64(1)

	got, err := f.ctrl.GetBalance(context.Background(), domain.GetBalanceParams{
		OwnerID:     &ownerID,
		AssetCode:   currency.DefaultAccountAssetCode,
		AccountType: domain.AccountTypeLiability,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Cmp(stored) != 0 {
		t.Fatalf("GetBalance converted the balance: got %s, want the stored %s", got, stored)
	}
}

func TestAccountGetBalance_NotFound_ReturnsRecordNotFoundError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return decimal.Zero, domain.ErrRecordNotFound
	}
	ownerID := domain.NewIDFromUint64(1)

	_, err := f.ctrl.GetBalance(context.Background(), domain.GetBalanceParams{OwnerID: &ownerID})
	errorxtest.AssertErrorCode(t, err, appErrors.RecordNotFoundError)
}

func TestAccountGetBalance_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return decimal.Zero, errors.New("db timeout")
	}
	ownerID := domain.NewIDFromUint64(1)

	_, err := f.ctrl.GetBalance(context.Background(), domain.GetBalanceParams{OwnerID: &ownerID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// Tests — AccountCtrl.GetPnlByOwnerID
// ============================================================

func TestAccountGetPnlByOwnerID_Success_ComputesAmountsAndPercents(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	ownerID := domain.NewIDFromUint64(1)
	assetCode := currency.NewAssetCode("USDT")

	// today: 200 USDT @ $1 = $200
	// yesterday: 100 USDT @ $1 = $100
	// PnL amount = +$100, percent = +100%
	f.balanceHistories.findTodayStatsFunc = func(context.Context, *domain.ID) ([]domain.BalanceHistory, error) {
		return []domain.BalanceHistory{
			{AssetCode: assetCode, Amount: decimal.NewFromInt(200), Rate: decimal.NewFromInt(1)},
		}, nil
	}
	f.balanceHistories.findYesterdayStatsFunc = func(context.Context, *domain.ID) ([]domain.BalanceHistory, error) {
		return []domain.BalanceHistory{
			{AssetCode: assetCode, Amount: decimal.NewFromInt(100), Rate: decimal.NewFromInt(1)},
		}, nil
	}

	result, err := f.ctrl.GetPnlByOwnerID(context.Background(), &ownerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PnlTotalAmount.Cmp(decimal.NewFromInt(100)) != 0 {
		t.Fatalf("unexpected total PnL amount: %s", result.PnlTotalAmount)
	}
	if result.PnlTotalPercent != "100.00%" {
		t.Fatalf("unexpected total PnL percent: %s", result.PnlTotalPercent)
	}
	assetResult, ok := result.PnlAssets[assetCode]
	if !ok {
		t.Fatal("expected asset in PnlAssets")
	}
	if assetResult.PnlAmount.Cmp(decimal.NewFromInt(100)) != 0 {
		t.Fatalf("unexpected asset PnL amount: %s", assetResult.PnlAmount)
	}
}

func TestAccountGetPnlByOwnerID_NoYesterdayData_PnlPercentIsZero(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	ownerID := domain.NewIDFromUint64(1)

	f.balanceHistories.findTodayStatsFunc = func(context.Context, *domain.ID) ([]domain.BalanceHistory, error) {
		return []domain.BalanceHistory{
			{AssetCode: currency.NewAssetCode("USDT"), Amount: decimal.NewFromInt(100), Rate: decimal.NewFromInt(1)},
		}, nil
	}
	f.balanceHistories.findYesterdayStatsFunc = func(context.Context, *domain.ID) ([]domain.BalanceHistory, error) {
		return []domain.BalanceHistory{}, nil
	}

	result, err := f.ctrl.GetPnlByOwnerID(context.Background(), &ownerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PnlTotalPercent != "0%" {
		t.Fatalf("expected 0%% when no yesterday data, got %s", result.PnlTotalPercent)
	}
}

func TestAccountGetPnlByOwnerID_FindTodayFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	ownerID := domain.NewIDFromUint64(1)
	f.balanceHistories.findTodayStatsFunc = func(context.Context, *domain.ID) ([]domain.BalanceHistory, error) {
		return nil, errors.New("db error")
	}

	_, err := f.ctrl.GetPnlByOwnerID(context.Background(), &ownerID)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestAccountGetPnlByOwnerID_FindYesterdayFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	ownerID := domain.NewIDFromUint64(1)
	f.balanceHistories.findTodayStatsFunc = func(context.Context, *domain.ID) ([]domain.BalanceHistory, error) {
		return []domain.BalanceHistory{}, nil
	}
	f.balanceHistories.findYesterdayStatsFunc = func(context.Context, *domain.ID) ([]domain.BalanceHistory, error) {
		return nil, errors.New("db error")
	}

	_, err := f.ctrl.GetPnlByOwnerID(context.Background(), &ownerID)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// Tests — AccountCtrl.UpdatePnlStat
// ============================================================

func TestAccountUpdatePnlStat_Success_AggregatesAndUpserts(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	userID := domain.NewIDFromUint64(1)
	assetCode := currency.NewAssetCode("USDT")

	// Two snapshots for the same user+asset — must be summed
	f.accounts.getAllPositiveSnapshotsFunc = func(context.Context, []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
		return []domain.PositiveBalanceSnapshot{
			{OwnerID: userID, AssetCode: assetCode, Balance: decimal.NewFromInt(100_000_000)},
			{OwnerID: userID, AssetCode: assetCode, Balance: decimal.NewFromInt(50_000_000)},
		}, nil
	}
	f.cache.getAllAssetsFunc = func(context.Context) (currency.AssetMap, error) {
		return currency.AssetMap{
			assetCode: currency.Asset{Code: assetCode, USDPrice: decimal.NewFromInt(1)},
		}, nil
	}

	var upserted []domain.BalanceHistory
	f.balanceHistories.upsertTodayStatsFunc = func(_ context.Context, h []domain.BalanceHistory) error {
		upserted = h
		return nil
	}

	if err := f.ctrl.UpdatePnlStat(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(upserted) != 1 {
		t.Fatalf("expected 1 history record (aggregated), got %d", len(upserted))
	}
	// 100_000_000 + 50_000_000 shifted by DefaultAssetScale
	expectedAmount := decimal.NewFromInt(150_000_000).Shift(-currency.DefaultAssetScale)
	if upserted[0].Amount.Cmp(expectedAmount) != 0 {
		t.Fatalf("unexpected aggregated amount: got %s, want %s", upserted[0].Amount, expectedAmount)
	}
}

func TestAccountUpdatePnlStat_AssetNotInCache_SkipsAndContinues(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	knownAsset := currency.NewAssetCode("USDT")
	unknownAsset := currency.NewAssetCode("UNKNOWN")
	userID := domain.NewIDFromUint64(1)

	f.accounts.getAllPositiveSnapshotsFunc = func(context.Context, []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
		return []domain.PositiveBalanceSnapshot{
			{OwnerID: userID, AssetCode: unknownAsset, Balance: decimal.NewFromInt(100_000_000)},
			{OwnerID: userID, AssetCode: knownAsset, Balance: decimal.NewFromInt(200_000_000)},
		}, nil
	}
	f.cache.getAllAssetsFunc = func(context.Context) (currency.AssetMap, error) {
		return currency.AssetMap{
			knownAsset: currency.Asset{Code: knownAsset, USDPrice: decimal.NewFromInt(1)},
		}, nil
	}

	var upserted []domain.BalanceHistory
	f.balanceHistories.upsertTodayStatsFunc = func(_ context.Context, h []domain.BalanceHistory) error {
		upserted = h
		return nil
	}

	if err := f.ctrl.UpdatePnlStat(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(upserted) != 1 {
		t.Fatalf("expected 1 history record (unknown asset skipped), got %d", len(upserted))
	}
	if upserted[0].AssetCode != knownAsset {
		t.Fatalf("expected known asset, got %s", upserted[0].AssetCode)
	}
}

func TestAccountUpdatePnlStat_GetSnapshotsFails_ReturnsError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.getAllPositiveSnapshotsFunc = func(context.Context, []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
		return nil, errors.New("db error")
	}

	if err := f.ctrl.UpdatePnlStat(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAccountUpdatePnlStat_GetAllAssetsFails_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.getAllPositiveSnapshotsFunc = func(context.Context, []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
		return []domain.PositiveBalanceSnapshot{}, nil
	}
	f.cache.getAllAssetsFunc = func(context.Context) (currency.AssetMap, error) {
		return nil, errors.New("cache unavailable")
	}

	err := f.ctrl.UpdatePnlStat(context.Background())
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

func TestAccountUpdatePnlStat_UpsertFails_ReturnsError(t *testing.T) {
	t.Parallel()

	f := newAccountFixture()
	f.accounts.getAllPositiveSnapshotsFunc = func(context.Context, []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
		return []domain.PositiveBalanceSnapshot{}, nil
	}
	f.cache.getAllAssetsFunc = func(context.Context) (currency.AssetMap, error) {
		return currency.AssetMap{}, nil
	}
	f.balanceHistories.upsertTodayStatsFunc = func(context.Context, []domain.BalanceHistory) error {
		return errors.New("upsert failed")
	}

	if err := f.ctrl.UpdatePnlStat(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ============================================================
// Fixture
// ============================================================

type accountFixture struct {
	ctrl             *AccountCtrl
	accounts         *mockAccountRepo
	balanceHistories *mockBalanceHistoryRepo
	integrity        *mockIntegrityService
	cache            *mockCacheController
	txManager        *dbtest.ObservableTxManager
}

func newAccountFixture() *accountFixture {
	accounts := &mockAccountRepo{}
	balanceHistories := &mockBalanceHistoryRepo{}
	integrity := &mockIntegrityService{}
	cache := &mockCacheController{}
	txManager := &dbtest.ObservableTxManager{}

	// Happy-path defaults
	accounts.createBatchFunc = func(_ context.Context, batch []domain.Account) error {
		accounts.createBatchCalls++
		return nil
	}
	accounts.withTxFromContextFunc = func(_ context.Context) domain.AccountRepository {
		return accounts
	}
	integrity.computeFunc = func(account domain.Account, _ decimal.Decimal) (string, error) {
		return "hash-" + account.AssetCode.String(), nil
	}

	ctrl := NewAccountController(cache, accounts, balanceHistories, integrity, txManager)

	return &accountFixture{
		ctrl:             ctrl,
		accounts:         accounts,
		balanceHistories: balanceHistories,
		integrity:        integrity,
		cache:            cache,
		txManager:        txManager,
	}
}

// ============================================================
// Mocks
// ============================================================

type mockAccountRepo struct {
	createBatchCalls int

	createBatchFunc             func(context.Context, []domain.Account) error
	getBalancesByOwnerIDFunc    func(context.Context, *domain.ID) (domain.GetBalancesResult, error)
	getBalanceFunc              func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error)
	getAllPositiveSnapshotsFunc func(context.Context, []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error)
	getByIDsFunc                func(context.Context, []domain.ID) (map[domain.ID]domain.Account, error)
	withTxFromContextFunc       func(context.Context) domain.AccountRepository
}

func (m *mockAccountRepo) CreateBatch(ctx context.Context, accounts []domain.Account) error {
	if m.createBatchFunc == nil {
		panic("unexpected call: AccountRepo.CreateBatch")
	}
	return m.createBatchFunc(ctx, accounts)
}

func (m *mockAccountRepo) GetBalancesByOwnerID(ctx context.Context, ownerID *domain.ID) (domain.GetBalancesResult, error) {
	if m.getBalancesByOwnerIDFunc == nil {
		panic("unexpected call: AccountRepo.GetBalancesByOwnerID")
	}
	return m.getBalancesByOwnerIDFunc(ctx, ownerID)
}

func (m *mockAccountRepo) GetBalance(ctx context.Context, params domain.GetBalanceParams) (decimal.Decimal, error) {
	if m.getBalanceFunc == nil {
		panic("unexpected call: AccountRepo.GetBalance")
	}
	return m.getBalanceFunc(ctx, params)
}

func (m *mockAccountRepo) GetAllPositiveUserBalanceSnapshots(ctx context.Context, types []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
	if m.getAllPositiveSnapshotsFunc == nil {
		panic("unexpected call: AccountRepo.GetAllPositiveUserBalanceSnapshots")
	}
	return m.getAllPositiveSnapshotsFunc(ctx, types)
}

func (m *mockAccountRepo) WithTxFromContext(ctx context.Context) domain.AccountRepository {
	if m.withTxFromContextFunc == nil {
		return m
	}
	return m.withTxFromContextFunc(ctx)
}

func (m *mockAccountRepo) GetForUpdate(_ context.Context, _ domain.GetForUpdateParams) (domain.Account, error) {
	panic("unexpected call: AccountRepo.GetForUpdate")
}

func (m *mockAccountRepo) UpdateBalance(_ context.Context, _ domain.UpdateBalanceParams) error {
	panic("unexpected call: AccountRepo.UpdateBalance")
}

func (m *mockAccountRepo) GetByIDsForUpdate(_ context.Context, _ []domain.ID) (map[domain.ID]domain.Account, error) {
	panic("unexpected call: AccountRepo.GetByIDsForUpdate")
}

func (m *mockAccountRepo) GetByIDs(ctx context.Context, ids []domain.ID) (map[domain.ID]domain.Account, error) {
	if m.getByIDsFunc != nil {
		return m.getByIDsFunc(ctx, ids)
	}
	panic("unexpected call: AccountRepo.GetByIDs")
}

type mockBalanceHistoryRepo struct {
	findTodayStatsFunc     func(context.Context, *domain.ID) ([]domain.BalanceHistory, error)
	findYesterdayStatsFunc func(context.Context, *domain.ID) ([]domain.BalanceHistory, error)
	upsertTodayStatsFunc   func(context.Context, []domain.BalanceHistory) error
}

func (m *mockBalanceHistoryRepo) FindTodayStats(ctx context.Context, userID *domain.ID) ([]domain.BalanceHistory, error) {
	if m.findTodayStatsFunc == nil {
		panic("unexpected call: BalanceHistoryRepo.FindTodayStats")
	}
	return m.findTodayStatsFunc(ctx, userID)
}

func (m *mockBalanceHistoryRepo) FindYesterdayStats(ctx context.Context, userID *domain.ID) ([]domain.BalanceHistory, error) {
	if m.findYesterdayStatsFunc == nil {
		panic("unexpected call: BalanceHistoryRepo.FindYesterdayStats")
	}
	return m.findYesterdayStatsFunc(ctx, userID)
}

func (m *mockBalanceHistoryRepo) UpsertTodayStats(ctx context.Context, histories []domain.BalanceHistory) error {
	if m.upsertTodayStatsFunc == nil {
		panic("unexpected call: BalanceHistoryRepo.UpsertTodayStats")
	}
	return m.upsertTodayStatsFunc(ctx, histories)
}

func (m *mockBalanceHistoryRepo) WithTxFromContext(_ context.Context) domain.BalanceHistoryRepository {
	return m
}

type mockIntegrityService struct {
	computeFunc func(domain.Account, decimal.Decimal) (string, error)
}

func (m *mockIntegrityService) Compute(account domain.Account, balance decimal.Decimal) (string, error) {
	if m.computeFunc == nil {
		panic("unexpected call: IntegrityService.Compute")
	}
	return m.computeFunc(account, balance)
}

func (m *mockIntegrityService) Verify(_ domain.Account) error {
	panic("unexpected call: IntegrityService.Verify")
}

// svcErrors is used to satisfy the import; keep the alias used in account.go consistent.
var _ = svcErrors.New
