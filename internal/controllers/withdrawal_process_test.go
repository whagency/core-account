package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/cache"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/services/currency"
	restrictionsvc "github.com/whagency/core-common/services/restriction"
	usersvc "github.com/whagency/core-common/services/user"
	"github.com/whagency/core-common/testkit/dbtest"
	"github.com/whagency/core-common/testkit/errorxtest"
)

// ============================================================
// Mocks — WithdrawalRequestRepository
//
// mockBlockchainController, mockEventController,
// mockTransactionController, mockRestrictionService,
// mockUserService, mockCacheController
// are defined in deposit_process_test.go (same package).
// ============================================================

type mockWithdrawalRequestRepo struct {
	createFunc                     func(context.Context, *domain.WithdrawalRequest) error
	updateFunc                     func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error
	getByParamsFunc                func(context.Context, domain.GetWithdrawalRequestParams) (domain.WithdrawalRequest, error)
	getByIDFunc                    func(context.Context, domain.ID) (domain.WithdrawalRequest, error)
	getByIDForUpdateFunc           func(context.Context, domain.ID) (domain.WithdrawalRequest, error)
	getByBlockchainIDForUpdateFunc func(context.Context, domain.ID) (domain.WithdrawalRequest, error)
	withdrawalListFunc             func(context.Context, domain.ID, domain.WithdrawalFilter) ([]domain.WithdrawalRequest, int64, error)
	findStuckByStatusFunc          func(context.Context, domain.WithdrawalStatus, time.Duration, int) ([]domain.WithdrawalRequest, error)
	withTxFromCtxFunc              func(context.Context) domain.WithdrawalRequestRepository
}

func (m *mockWithdrawalRequestRepo) Create(ctx context.Context, r *domain.WithdrawalRequest) error {
	if m.createFunc == nil {
		panic("unexpected call: WithdrawalRequest.Create")
	}
	return m.createFunc(ctx, r)
}

func (m *mockWithdrawalRequestRepo) Update(ctx context.Context, id domain.ID, params domain.UpdateWithdrawalRequestParams) error {
	if m.updateFunc == nil {
		panic("unexpected call: WithdrawalRequest.Update")
	}
	return m.updateFunc(ctx, id, params)
}

func (m *mockWithdrawalRequestRepo) GetByParams(ctx context.Context, params domain.GetWithdrawalRequestParams) (domain.WithdrawalRequest, error) {
	if m.getByParamsFunc == nil {
		panic("unexpected call: WithdrawalRequest.GetByParams")
	}
	return m.getByParamsFunc(ctx, params)
}

func (m *mockWithdrawalRequestRepo) GetByID(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
	if m.getByIDFunc == nil {
		panic("unexpected call: WithdrawalRequest.GetByID")
	}
	return m.getByIDFunc(ctx, id)
}

func (m *mockWithdrawalRequestRepo) GetByIDForUpdate(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
	if m.getByIDForUpdateFunc == nil {
		panic("unexpected call: WithdrawalRequest.GetByIDForUpdate")
	}
	return m.getByIDForUpdateFunc(ctx, id)
}

func (m *mockWithdrawalRequestRepo) GetByBlockchainIDForUpdate(ctx context.Context, blockchainTxID domain.ID) (domain.WithdrawalRequest, error) {
	if m.getByBlockchainIDForUpdateFunc == nil {
		panic("unexpected call: WithdrawalRequest.GetByBlockchainIDForUpdate")
	}
	return m.getByBlockchainIDForUpdateFunc(ctx, blockchainTxID)
}

func (m *mockWithdrawalRequestRepo) WithdrawalList(ctx context.Context, userID domain.ID, filter domain.WithdrawalFilter) ([]domain.WithdrawalRequest, int64, error) {
	if m.withdrawalListFunc == nil {
		panic("unexpected call: WithdrawalRequest.WithdrawalList")
	}
	return m.withdrawalListFunc(ctx, userID, filter)
}

func (m *mockWithdrawalRequestRepo) FindStuckByStatus(ctx context.Context, status domain.WithdrawalStatus, olderThan time.Duration, limit int) ([]domain.WithdrawalRequest, error) {
	if m.findStuckByStatusFunc == nil {
		panic("unexpected call: WithdrawalRequest.FindStuckByStatus")
	}
	return m.findStuckByStatusFunc(ctx, status, olderThan, limit)
}

func (m *mockWithdrawalRequestRepo) WithTxFromContext(ctx context.Context) domain.WithdrawalRequestRepository {
	if m.withTxFromCtxFunc != nil {
		return m.withTxFromCtxFunc(ctx)
	}
	return m
}

// ============================================================
// Mocks — KYT service
// ============================================================

type mockKYTService struct {
	checkAddressFunc func(context.Context, string, string) (domain.KYTCheckAddressResult, error)
}

func (m *mockKYTService) CheckAddress(ctx context.Context, address, network string) (domain.KYTCheckAddressResult, error) {
	if m.checkAddressFunc == nil {
		panic("unexpected call: KYT.CheckAddress")
	}
	return m.checkAddressFunc(ctx, address, network)
}

// ============================================================
// Mocks — Locker
// ============================================================

type mockLocker struct {
	tryLockFunc func(context.Context, string) error
	unlockFunc  func(context.Context, string) error
	UnlockCalls int
}

func (m *mockLocker) TryLock(ctx context.Context, key string) error {
	if m.tryLockFunc == nil {
		return nil // default: lock succeeds
	}
	return m.tryLockFunc(ctx, key)
}

func (m *mockLocker) Unlock(ctx context.Context, key string) error {
	m.UnlockCalls++
	if m.unlockFunc == nil {
		return nil // default: no-op
	}
	return m.unlockFunc(ctx, key)
}

// ============================================================
// Mocks — AccountController
// ============================================================

type mockAccountController struct {
	getBalanceFunc func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error)
}

func (m *mockAccountController) CreateDefaultAccountsForUser(_ context.Context, _ domain.ID) error {
	panic("unexpected call: AccountController.CreateDefaultAccountsForUser")
}

func (m *mockAccountController) CreateDefaultSystemAccounts(_ context.Context) error {
	panic("unexpected call: AccountController.CreateDefaultSystemAccounts")
}

func (m *mockAccountController) CreateAccount(_ context.Context, _ *domain.ID, _ currency.AssetCode, _ domain.AccountType) error {
	panic("unexpected call: AccountController.CreateAccount")
}

func (m *mockAccountController) GetBalancesByOwnerID(_ context.Context, _ *domain.ID) (domain.GetBalancesResult, error) {
	panic("unexpected call: AccountController.GetBalancesByOwnerID")
}

func (m *mockAccountController) GetPnlByOwnerID(_ context.Context, _ *domain.ID) (domain.GetPnlResult, error) {
	panic("unexpected call: AccountController.GetPnlByOwnerID")
}

func (m *mockAccountController) UpdatePnlStat(_ context.Context) error {
	panic("unexpected call: AccountController.UpdatePnlStat")
}

func (m *mockAccountController) GetBalance(ctx context.Context, params domain.GetBalanceParams) (decimal.Decimal, error) {
	if m.getBalanceFunc == nil {
		panic("unexpected call: AccountController.GetBalance")
	}
	return m.getBalanceFunc(ctx, params)
}

// ============================================================
// Unified fixture
// ============================================================

type withdrawalFixture struct {
	ctrl             *WithdrawalCtrl
	blockchain       *mockBlockchainController
	events           *mockEventController
	transactions     *mockTransactionController
	withdrawals      *mockWithdrawalRequestRepo
	outbox           *mockOutboxRepo
	kyt              *mockKYTService
	user             *mockUserService
	restriction      *mockRestrictionService
	cache            *mockCacheController
	account          *mockAccountController
	locker           *mockLocker
	processingLocker *mockLocker
	txManager        *dbtest.ObservableTxManager
}

func newWithdrawalFixture() *withdrawalFixture {
	blockchain := &mockBlockchainController{}
	events := &mockEventController{}
	transactions := &mockTransactionController{}
	withdrawals := &mockWithdrawalRequestRepo{}
	outboxRepo := &mockOutboxRepo{}
	kyt := &mockKYTService{}
	user := &mockUserService{}
	restriction := &mockRestrictionService{}
	cache := &mockCacheController{}
	account := &mockAccountController{}
	locker := &mockLocker{}
	processingLocker := &mockLocker{}
	txManager := &dbtest.ObservableTxManager{}

	ctrl := &WithdrawalCtrl{
		accountCtrl:          account,
		blockchainCtrl:       blockchain,
		eventCtrl:            events,
		cacheCtrl:            cache,
		transactionCtrl:      transactions,
		withdrawalRequests:   withdrawals,
		outboxRepo:           outboxRepo,
		restriction:          restriction,
		user:                 user,
		kyt:                  kyt,
		withdrawalLocker:     locker,
		processingLocker:     processingLocker,
		dbTransactionManager: txManager,
	}

	return &withdrawalFixture{
		ctrl:             ctrl,
		blockchain:       blockchain,
		events:           events,
		transactions:     transactions,
		withdrawals:      withdrawals,
		outbox:           outboxRepo,
		kyt:              kyt,
		user:             user,
		restriction:      restriction,
		cache:            cache,
		account:          account,
		locker:           locker,
		processingLocker: processingLocker,
		txManager:        txManager,
	}
}

// ============================================================
// Builder helpers
// ============================================================

func testOutboundTx(status domain.BlockchainTxStatus) domain.BlockchainTx {
	return domain.BlockchainTx{
		ID:        domain.NewIDFromUint64(333),
		AssetCode: currency.NewAssetCode("USDT"),
		Network:   domain.ChainNetworkTRX,
		ToAddress: "TXXX000000000000000000000000000000000",
		Amount:    decimal.NewFromInt(10000000000), // 100 USDT quantized
		Status:    status,
		Type:      domain.BlockchainTxTypeOutbound,
	}
}

func testWithdrawalRequest(status domain.WithdrawalStatus, amount int64, blockchainTxID domain.ID) domain.WithdrawalRequest {
	return domain.WithdrawalRequest{
		ID:             domain.NewIDFromUint64(444),
		UserID:         domain.NewIDFromUint64(1001),
		AssetCode:      currency.NewAssetCode("USDT"),
		Network:        domain.ChainNetworkTRX,
		ToAddress:      "TXXX000000000000000000000000000000000",
		Amount:         decimal.NewFromInt(amount),
		Fee:            decimal.NewFromInt(100000000), // 1 USDT fee quantized
		FeeTier:        domain.FeeTierStandard,
		Status:         status,
		BlockchainTxID: &blockchainTxID,
	}
}

func testNewWithdrawalRequest() domain.WithdrawalRequest {
	return domain.WithdrawalRequest{
		ID:        domain.NewIDFromUint64(444),
		UserID:    domain.NewIDFromUint64(1001),
		AssetCode: currency.NewAssetCode("USDT"),
		Network:   domain.ChainNetworkTRX,
		ToAddress: "TXXX000000000000000000000000000000000",
		// Amount = 101 USDT quantized, Fee = 1 USDT quantized
		// Amount - Fee = 100 USDT, matches testOutboundTx amount=10000000000
		Amount:  decimal.NewFromInt(10100000000),
		Fee:     decimal.NewFromInt(100000000),
		FeeTier: domain.FeeTierStandard,
		Status:  domain.WithdrawalStatusNew,
	}
}

func testWithdrawalChainConfig() domain.ChainConfig {
	return domain.ChainConfig{
		AssetCode:                 currency.NewAssetCode("USDT"),
		Network:                   domain.ChainNetworkTRX,
		Decimals:                  6,
		WithdrawFeeType:           domain.FeeTypeFixAmount,
		WithdrawFee:               decimal.Zero,
		MinAmountWithdrawalReview: decimal.NewFromInt(5000),
	}
}

func zeroFeeResult() domain.GetBlockchainFeeResult {
	return domain.GetBlockchainFeeResult{
		AssetRate: decimal.NewFromInt(1),
		Standard:  domain.GetBlockchainFeeItem{EstimatedFee: decimal.Zero},
	}
}

func largeFeeResult() domain.GetBlockchainFeeResult {
	return domain.GetBlockchainFeeResult{
		AssetRate: decimal.NewFromInt(1),
		Standard:  domain.GetBlockchainFeeItem{EstimatedFee: decimal.NewFromInt(9999)},
	}
}

func allowedWithdrawalLimitResult() restrictionsvc.CheckLimitResult {
	return restrictionsvc.CheckLimitResult{
		OperationType: restrictionsvc.LimitOperationTypeWithdrawal,
		Decision:      restrictionsvc.DecisionTypeAllow,
	}
}

func blockedWithdrawalLimitResult() restrictionsvc.CheckLimitResult {
	return restrictionsvc.CheckLimitResult{
		OperationType: restrictionsvc.LimitOperationTypeWithdrawal,
		Decision:      restrictionsvc.DecisionTypeDenyAccount,
	}
}

func cleanKYT() domain.KYTCheckAddressResult {
	return domain.KYTCheckAddressResult{Status: domain.KYTStatusSuccess, IsClean: true}
}

func dirtyKYT() domain.KYTCheckAddressResult {
	return domain.KYTCheckAddressResult{Status: domain.KYTStatusSuccess, IsClean: false}
}

func testInitUser() usersvc.User {
	return usersvc.User{ID: 1001, Country: "UA"}
}

func testInitParams(amount decimal.Decimal) domain.InitWithdrawalParams {
	return domain.InitWithdrawalParams{
		User:      testInitUser(),
		AssetCode: currency.NewAssetCode("USDT"),
		Network:   domain.ChainNetworkTRX,
		ToAddress: "TXXX000000000000000000000000000000000",
		Amount:    amount,
		FeeTier:   domain.FeeTierStandard,
	}
}

// setupInitHappyPath wires every dependency for a successful Init.
// Individual tests override the one step they want to fail.
func setupInitHappyPath(f *withdrawalFixture) {
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testWithdrawalChainConfig(), nil
	}
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(1000), nil
	}
	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedWithdrawalLimitResult(), nil
	}
	f.blockchain.getFeesFunc = func(context.Context, domain.GetFeesParams) (domain.GetBlockchainFeeResult, error) {
		return zeroFeeResult(), nil
	}
	f.withdrawals.createFunc = func(_ context.Context, req *domain.WithdrawalRequest) error {
		req.ID = domain.NewIDFromUint64(1)
		return nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(77), Status: domain.JournalStatusCompleted}, nil
	}
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return nil
	}
	f.events.publishWithdrawalCreatedFunc = func(context.Context, domain.WithdrawalRequestCreatedEvent) error {
		return nil
	}
}

// setupSendHappyPath wires dependencies for a successful SendToBlockchain up to the
// blockchain call. Callers override blockchainCtrl.Withdrawal and downstream as needed.
//
// Flow: processingLocker.TryLock → GetByID (no TX) → kyt → blockchain → TX: GetByIDForUpdate → process → Commit
func setupSendHappyPath(f *withdrawalFixture, req domain.WithdrawalRequest) {
	f.processingLocker.tryLockFunc = func(context.Context, string) error { return nil }
	f.withdrawals.getByIDFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return cleanKYT(), nil
	}
	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
}

// ============================================================
// Process() tests
// ============================================================

func TestWithdrawalProcess_GetTransactionByID_Fails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, errors.New("db error")
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: domain.NewIDFromUint64(1)})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestWithdrawalProcess_InboundTx_ReturnsNilNoOp(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		tx := testOutboundTx(domain.BlockchainTxStatusPending)
		tx.Type = domain.BlockchainTxTypeInbound // override to inbound
		return tx, nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: domain.NewIDFromUint64(1)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithdrawalProcess_GetByBlockchainIDForUpdate_NotFound_ReturnsRecordNotFoundError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusPending)
	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return domain.WithdrawalRequest{}, domain.ErrRecordNotFound
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.RecordNotFoundError)
}

func TestWithdrawalProcess_GetByBlockchainIDForUpdate_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusPending)
	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return domain.WithdrawalRequest{}, errors.New("select failed")
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestWithdrawalProcess_WithdrawalNotPending_ReturnsNilNoOp(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusConfirmed)
	req := testWithdrawalRequest(domain.WithdrawalStatusSuccess, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		t.Fatal("Update must not be called when withdrawal is not Pending")
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithdrawalProcess_BlockchainPending_WithdrawalPending_NoOp(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusPending)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		t.Fatal("Update must not be called when status mapping produces no change")
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithdrawalProcess_BlockchainPendingSignatures_WithdrawalPending_NoOp(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	// PendingSignatures maps to WithdrawalStatusPending, same as current status → no-op
	tx := testOutboundTx(domain.BlockchainTxPendingSignatures)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		t.Fatal("Update must not be called for PendingSignatures → Pending no-op")
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithdrawalProcess_Confirmed_AmountsMatch_UpdatesSuccessAndSendsPush(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusConfirmed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}

	makeTxCalls := 0
	f.transactions.makeTransactionFunc = func(_ context.Context, p domain.MakeTxParams) (domain.MakeTxResult, error) {
		makeTxCalls++
		if p.Type != domain.JournalBizTypeWithdrawalComplete {
			t.Fatalf("unexpected tx type: got %d, want WithdrawalComplete", p.Type)
		}
		if p.Fee.Cmp(req.Fee) != 0 {
			t.Fatalf("unexpected fee: got %s, want %s", p.Fee.String(), req.Fee.String())
		}
		if p.IdempotencyKey == nil || *p.IdempotencyKey != req.ID.String() {
			t.Fatalf("unexpected idempotency key: %+v", p.IdempotencyKey)
		}
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(77), Status: domain.JournalStatusCompleted}, nil
	}

	updateCalls := 0
	f.withdrawals.updateFunc = func(_ context.Context, _ domain.ID, p domain.UpdateWithdrawalRequestParams) error {
		updateCalls++
		if p.Status != domain.WithdrawalStatusSuccess {
			t.Fatalf("unexpected status: got %s", p.Status.String())
		}
		if p.ConfirmedAt == nil || p.ConfirmedAt.IsZero() {
			t.Fatal("expected ConfirmedAt to be set on success")
		}
		if p.Description != nil {
			t.Fatalf("unexpected description for success: %v", *p.Description)
		}
		return nil
	}

	outboxCreateCalls := 0
	f.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}
	f.events.publishTransactionRollbackFunc = func(context.Context, domain.ID) error {
		t.Fatal("rollback must not be called on success")
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if makeTxCalls != 1 {
		t.Fatalf("expected 1 MakeTransaction call, got %d", makeTxCalls)
	}
	if updateCalls != 1 {
		t.Fatalf("expected 1 Update call, got %d", updateCalls)
	}
	if outboxCreateCalls != 1 {
		t.Fatalf("expected 1 outbox enqueue (push notification), got %d", outboxCreateCalls)
	}
}

func TestWithdrawalProcess_Confirmed_AmountMismatch_UpdatesSuccess_WithDescription(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusConfirmed)
	// amount - fee = 11100000000 - 100000000 = 11000000000 ≠ 10000000000 → mismatch
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 11100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(77), Status: domain.JournalStatusCompleted}, nil
	}

	var capturedParams domain.UpdateWithdrawalRequestParams
	f.withdrawals.updateFunc = func(_ context.Context, _ domain.ID, p domain.UpdateWithdrawalRequestParams) error {
		capturedParams = p
		return nil
	}
	f.events.publishNotificationPushFunc = func(context.Context, domain.PushMessage) error {
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Behavior change from old implementation: blockchain Confirmed + amount mismatch still
	// results in Success (trust the blockchain), with a description note for operators.
	if capturedParams.Status != domain.WithdrawalStatusSuccess {
		t.Fatalf("expected Success status (trust blockchain), got %s", capturedParams.Status.String())
	}
	if capturedParams.Description == nil || *capturedParams.Description != "Amount mismatch with blockchain transaction" {
		t.Fatalf("expected mismatch description, got %+v", capturedParams.Description)
	}
}

func TestWithdrawalProcess_Failed_UpdatesStatus(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusFailed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}

	updateCalls := 0
	f.withdrawals.updateFunc = func(_ context.Context, _ domain.ID, p domain.UpdateWithdrawalRequestParams) error {
		updateCalls++
		if p.Status != domain.WithdrawalStatusFailed {
			t.Fatalf("expected Failed status, got %s", p.Status.String())
		}
		return nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		t.Fatal("MakeTransaction must not be called for Failed status")
		return domain.MakeTxResult{}, nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("expected 1 Update call, got %d", updateCalls)
	}
}

func TestWithdrawalProcess_Failed_UpdateFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusFailed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return errors.New("update failed")
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestWithdrawalProcess_Ignored_ReturnsNilNoOp(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusIgnored)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		t.Fatal("Update must not be called for Ignored status")
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWithdrawalProcess_Confirmed_MakeTransactionFails_ReturnsError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusConfirmed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{}, svcErrors.New(appErrors.DatabaseError, appErrors.WithCause(errors.New("tx failed")))
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		t.Fatal("Update must not be called when MakeTransaction fails")
		return nil
	}
	f.events.publishNotificationPushFunc = func(context.Context, domain.PushMessage) error {
		t.Fatal("push must not be called when MakeTransaction fails")
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestWithdrawalProcess_Confirmed_UpdateFails_PublishesRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusConfirmed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)
	journalID := domain.NewIDFromUint64(909)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return errors.New("update failed")
	}

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected rollback journal id: got %v, want %v", id, journalID)
		}
		return nil
	}
	f.events.publishNotificationPushFunc = func(context.Context, domain.PushMessage) error {
		t.Fatal("push must not be called when update failed")
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if rollbackCalls != 0 {
		t.Fatalf("expected no rollback event, got %d", rollbackCalls)
	}
}

func TestWithdrawalProcess_CommitFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusFailed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.txManager.CommitErr = errors.New("commit failed")

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected rollback on commit failure, got %d rollback calls", f.txManager.RollbackCalls)
	}
}

// ============================================================
// SendToBlockchain() tests
// ============================================================

func TestWithdrawalSendToBlockchain_ProcessingLockerLocked_ReturnsWithdrawalRequestInProgressError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.processingLocker.tryLockFunc = func(context.Context, string) error {
		return cache.KeyAlreadyLockedErr
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: domain.NewIDFromUint64(1)})
	errorxtest.AssertErrorCode(t, err, svcErrors.WithdrawalRequestInProgressError)
}

func TestWithdrawalSendToBlockchain_NotFound_ReturnsRecordNotFoundError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.processingLocker.tryLockFunc = func(context.Context, string) error { return nil }
	f.withdrawals.getByIDFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return domain.WithdrawalRequest{}, domain.ErrRecordNotFound
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: domain.NewIDFromUint64(1)})
	errorxtest.AssertErrorCode(t, err, appErrors.RecordNotFoundError)
}

func TestWithdrawalSendToBlockchain_GetByIDDBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.processingLocker.tryLockFunc = func(context.Context, string) error { return nil }
	f.withdrawals.getByIDFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return domain.WithdrawalRequest{}, errors.New("db error")
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: domain.NewIDFromUint64(1)})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestWithdrawalSendToBlockchain_WrongStatus_ReturnsInvalidRequestError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil

	f.processingLocker.tryLockFunc = func(context.Context, string) error { return nil }
	f.withdrawals.getByIDFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.InvalidRequestError)
}

func TestWithdrawalSendToBlockchain_KYTServiceError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return domain.KYTCheckAddressResult{}, errors.New("kyt service unavailable")
	}
	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		t.Fatal("blockchain Withdrawal must not be called when KYT service errors")
		return domain.BlockchainTx{}, nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

// AMLBot answers HTTP 200 with result:false on its own internal errors, and
// reports a still-running check as pending. Both decode to a non-success
// status, and treating either as clean is what let an unchecked address be
// paid out.
func TestWithdrawalSendToBlockchain_KYTInconclusive_DoesNotSend(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		kytStatus domain.KYTStatus
	}{
		{name: "provider_error", kytStatus: domain.KYTStatusError},
		{name: "check_still_running", kytStatus: domain.KYTStatusPending},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWithdrawalFixture()
			req := testNewWithdrawalRequest()
			setupSendHappyPath(f, req)

			f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
				// IsClean is true on purpose: with no usable verdict the flag
				// carries no information and must not be acted on.
				return domain.KYTCheckAddressResult{Status: tc.kytStatus, IsClean: true}, nil
			}
			f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
				t.Fatal("Withdrawal must not be broadcast without a usable KYT verdict")
				return domain.BlockchainTx{}, nil
			}
			f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
				t.Fatal("the request must stay New so it can be retried, not be updated")
				return nil
			}
			f.user.blockUserFunc = func(context.Context, uint64) error {
				t.Fatal("an unavailable provider must not get the user blocked")
				return nil
			}

			err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
			errorxtest.AssertErrorCode(t, err, svcErrors.KYTVerdictUnavailableError)
		})
	}
}

func TestWithdrawalSendToBlockchain_KYTBlocksAddress_UpdatesBlockedAndBlocksUser(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return dirtyKYT(), nil
	}

	updateCalls := 0
	f.withdrawals.updateFunc = func(_ context.Context, _ domain.ID, p domain.UpdateWithdrawalRequestParams) error {
		updateCalls++
		if p.Status != domain.WithdrawalStatusBlocked {
			t.Fatalf("expected Blocked status, got %s", p.Status.String())
		}
		return nil
	}
	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		t.Fatal("Withdrawal must not be called when KYT blocks address")
		return domain.BlockchainTx{}, nil
	}

	blockUserCalls := 0
	f.user.blockUserFunc = func(_ context.Context, userID uint64) error {
		blockUserCalls++
		if userID != req.UserID.Uint64() {
			t.Fatalf("unexpected user blocked: %d", userID)
		}
		return nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("expected 1 Update call, got %d", updateCalls)
	}
	if blockUserCalls != 1 {
		t.Fatalf("expected 1 BlockUser call, got %d", blockUserCalls)
	}
	if f.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 commit, got %d", f.txManager.CommitCalls)
	}
}

func TestWithdrawalSendToBlockchain_KYTBlocked_CommitFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return dirtyKYT(), nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.txManager.CommitErr = errors.New("commit failed")

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestWithdrawalSendToBlockchain_KYTBlocked_BlockUserFails_ReturnsNilBestEffort(t *testing.T) {
	t.Parallel()

	// user.BlockUser is best-effort post-commit — failure must not propagate as an error.
	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return dirtyKYT(), nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	blockUserCalls := 0
	f.user.blockUserFunc = func(context.Context, uint64) error {
		blockUserCalls++
		return errors.New("user service unavailable")
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	if err != nil {
		t.Fatalf("expected nil (best-effort), got: %v", err)
	}
	if blockUserCalls != 1 {
		t.Fatalf("expected BlockUser to be attempted once, got %d", blockUserCalls)
	}
	if f.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 commit, got %d", f.txManager.CommitCalls)
	}
}

func TestWithdrawalSendToBlockchain_BlockchainFails_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	// When blockchain call fails, we return immediately — no DB write, no TX opened.
	// The queue will retry since ExternalServiceError is no longer in the non-retryable list (BUG-1 fix).
	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, svcErrors.New(appErrors.ExternalServiceError, appErrors.WithCause(errors.New("node down")))
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
	if f.txManager.CommitCalls != 0 {
		t.Fatalf("expected 0 commits when blockchain fails before TX, got %d", f.txManager.CommitCalls)
	}
}

func TestWithdrawalSendToBlockchain_ReCheckStatusChanged_ReturnsInvalidRequestError(t *testing.T) {
	t.Parallel()

	// Between initial GetByID (New) and GetByIDForUpdate inside TX the status was changed
	// by a concurrent operation — second attempt must be rejected without writing.
	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return testOutboundTx(domain.BlockchainTxStatusPending), nil
	}
	// GetByIDForUpdate inside TX returns non-New status (simulates concurrent change)
	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		changed := req
		changed.Status = domain.WithdrawalStatusPending
		return changed, nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.InvalidRequestError)
	if f.txManager.CommitCalls != 0 {
		t.Fatalf("expected 0 commits when status changed before re-check, got %d", f.txManager.CommitCalls)
	}
}

func TestWithdrawalSendToBlockchain_PendingSignatures_WithdrawalBecomesPending_NoPush(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return testOutboundTx(domain.BlockchainTxPendingSignatures), nil
	}

	updateCalls := 0
	f.withdrawals.updateFunc = func(_ context.Context, _ domain.ID, p domain.UpdateWithdrawalRequestParams) error {
		updateCalls++
		if p.Status != domain.WithdrawalStatusPending {
			t.Fatalf("expected Pending status for PendingSignatures, got %s", p.Status.String())
		}
		return nil
	}
	f.events.publishNotificationPushFunc = func(context.Context, domain.PushMessage) error {
		t.Fatal("push must not be sent for PendingSignatures status")
		return nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("expected 1 Update call, got %d", updateCalls)
	}
}

func TestWithdrawalSendToBlockchain_BlockchainNew_WithdrawalBecomesPending_NoPush(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return testOutboundTx(domain.BlockchainTxStatusNew), nil
	}

	updateCalls := 0
	f.withdrawals.updateFunc = func(_ context.Context, _ domain.ID, p domain.UpdateWithdrawalRequestParams) error {
		updateCalls++
		if p.Status != domain.WithdrawalStatusPending {
			t.Fatalf("expected Pending status for blockchain New, got %s", p.Status.String())
		}
		return nil
	}
	f.events.publishNotificationPushFunc = func(context.Context, domain.PushMessage) error {
		t.Fatal("push must not be sent for Pending status")
		return nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("expected 1 Update call, got %d", updateCalls)
	}
}

func TestWithdrawalSendToBlockchain_BlockchainConfirmed_WithdrawalBecomesSuccess_SendsPush(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return testOutboundTx(domain.BlockchainTxStatusConfirmed), nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(55), Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}

	outboxCreateCalls := 0
	f.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outboxCreateCalls != 1 {
		t.Fatalf("expected 1 outbox enqueue (push notification), got %d", outboxCreateCalls)
	}
}

func TestWithdrawalSendToBlockchain_ProcessFails_ReturnsError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return testOutboundTx(domain.BlockchainTxStatusConfirmed), nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{}, svcErrors.New(appErrors.DatabaseError, appErrors.WithCause(errors.New("journal failed")))
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		t.Fatal("Update must not be called when process fails")
		return nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected rollback on process failure, got %d", f.txManager.RollbackCalls)
	}
}

func TestWithdrawalSendToBlockchain_CommitFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return testOutboundTx(domain.BlockchainTxStatusPending), nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.txManager.CommitErr = errors.New("commit failed")

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected rollback on commit failure, got %d", f.txManager.RollbackCalls)
	}
}

// ============================================================
// Init() tests
// ============================================================

func TestWithdrawalInit_LockAlreadyHeld_ReturnsWithdrawalAlreadyInProgressError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.locker.tryLockFunc = func(context.Context, string) error {
		return cache.KeyAlreadyLockedErr
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, svcErrors.WithdrawalAlreadyInProgressError)
}

func TestWithdrawalInit_LockUnexpectedError_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.locker.tryLockFunc = func(context.Context, string) error {
		return errors.New("cache unavailable")
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

func TestWithdrawalInit_GetChainConfigFails_ReturnsError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return domain.ChainConfig{}, svcErrors.New(appErrors.RecordNotFoundError, appErrors.WithCause(errors.New("chain not found")))
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	if err == nil {
		t.Fatal("expected error when chain config not found")
	}
}

func TestWithdrawalInit_AmountPrecisionExceedsChainDecimals_ReturnsValidationError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		cfg := testWithdrawalChainConfig()
		cfg.Decimals = 6
		return cfg, nil
	}

	// 7 decimal places, but chain only supports 6
	amount := decimal.NewFromFloat(1.0000001)
	err := f.ctrl.Init(context.Background(), testInitParams(amount))
	errorxtest.AssertErrorCode(t, err, appErrors.ValidationError)
}

func TestWithdrawalInit_GetBalanceFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testWithdrawalChainConfig(), nil
	}
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return decimal.Zero, errors.New("db error")
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestWithdrawalInit_InsufficientBalance_ReturnsInsufficientFundsError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testWithdrawalChainConfig(), nil
	}
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(50), nil // balance < requested amount
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.InsufficientFundsError)
}

func TestWithdrawalInit_RestrictionServiceError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testWithdrawalChainConfig(), nil
	}
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(1000), nil
	}
	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{}, errors.New("restriction service down")
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestWithdrawalInit_RestrictionBlocked_ReturnsRestrictedOperationError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testWithdrawalChainConfig(), nil
	}
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(1000), nil
	}
	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return blockedWithdrawalLimitResult(), nil
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.RestrictedOperationError)
}

func TestWithdrawalInit_FeeExceedsAmount_ReturnsFeeMoreThanAmountError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testWithdrawalChainConfig(), nil
	}
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(1000), nil
	}
	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedWithdrawalLimitResult(), nil
	}
	f.blockchain.getFeesFunc = func(context.Context, domain.GetFeesParams) (domain.GetBlockchainFeeResult, error) {
		return largeFeeResult(), nil // standard fee = 9999
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(1))) // amount=1 < fee=9999
	errorxtest.AssertErrorCode(t, err, svcErrors.FeeMoreThanAmountError)
}

func TestWithdrawalInit_HighAmount_CreatesManualReview_NoEventPublished(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	setupInitHappyPath(f)
	// balance must cover the high amount used in this test
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(100000), nil
	}

	eventCalled := false
	f.events.publishWithdrawalCreatedFunc = func(context.Context, domain.WithdrawalRequestCreatedEvent) error {
		eventCalled = true
		return nil
	}

	// amount=10000 >= MinAmountWithdrawalReview=5000 → ManualReview
	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(10000)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventCalled {
		t.Fatal("WithdrawalCreated event must not be published for ManualReview status")
	}
}

func TestWithdrawalInit_NormalAmount_CreatesNew_PublishesWithdrawalCreatedEvent(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	setupInitHappyPath(f)

	eventCalls := 0
	f.events.publishWithdrawalCreatedFunc = func(context.Context, domain.WithdrawalRequestCreatedEvent) error {
		eventCalls++
		return nil
	}

	// amount=100 < MinAmountWithdrawalReview=5000 → New status
	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eventCalls != 1 {
		t.Fatalf("expected 1 WithdrawalCreated event, got %d", eventCalls)
	}
	if f.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 commit, got %d", f.txManager.CommitCalls)
	}
}

func TestWithdrawalInit_CreateFails_ReturnsDatabaseError_NoJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	setupInitHappyPath(f)

	f.withdrawals.createFunc = func(context.Context, *domain.WithdrawalRequest) error {
		return errors.New("insert failed")
	}
	f.events.publishTransactionRollbackFunc = func(context.Context, domain.ID) error {
		t.Fatal("journal rollback must not be published when Create fails (no journal was created)")
		return nil
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected rollback on Create failure, got %d", f.txManager.RollbackCalls)
	}
}

func TestWithdrawalInit_MakeTransactionFails_RollsBackAndPublishesNoJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	setupInitHappyPath(f)

	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{}, errors.New("journal service error")
	}
	f.events.publishTransactionRollbackFunc = func(context.Context, domain.ID) error {
		t.Fatal("journal rollback must not be published when MakeTransaction fails with zero JournalID")
		return nil
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	if err == nil {
		t.Fatal("expected error")
	}
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected rollback, got %d", f.txManager.RollbackCalls)
	}
}

// TestWithdrawalInit_Success_CounterEnqueuedViaOutbox verifies that a successful
// withdrawal Init enqueues the restriction counter update in the outbox instead of
// making a direct gRPC call. The actual delivery is handled asynchronously by the
// outbox worker, which means transient restriction-service failures no longer affect
// the Init result.
func TestWithdrawalInit_Success_CounterEnqueuedViaOutbox(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	setupInitHappyPath(f)

	journalID := domain.NewIDFromUint64(77)
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}

	outboxCreateCalls := 0
	f.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		return nil
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback event, got %d", rollbackCalls)
	}
	// Counter update must be enqueued in the outbox (inside the transaction).
	if outboxCreateCalls != 1 {
		t.Fatalf("expected exactly 1 outbox counter enqueue, got %d", outboxCreateCalls)
	}
}

func TestWithdrawalInit_CommitFails_DoesNotUpdateCountersOrPublishJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	setupInitHappyPath(f)

	journalID := domain.NewIDFromUint64(77)
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}

	countersCallCount := 0
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		countersCallCount++
		return nil
	}

	f.txManager.CommitErr = errors.New("commit failed")

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected journal id in rollback: got %v, want %v", id, journalID)
		}
		return nil
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)

	if countersCallCount != 0 {
		t.Fatalf("expected no CountersUpdate calls before failed commit, got %d", countersCallCount)
	}
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback event, got %d", rollbackCalls)
	}
}

// ============================================================
// Reject() tests
// ============================================================

func TestWithdrawalReject_GetByIDForUpdateFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return domain.WithdrawalRequest{}, errors.New("db error")
	}

	err := f.ctrl.Reject(context.Background(), domain.NewIDFromUint64(1))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestWithdrawalReject_StatusNotRejectable_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest() // status = New, CanBeRejected() = false

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

func TestWithdrawalReject_FailedStatus_CanBeRejected(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusFailed, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(1), Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("unexpected error for Failed→Rejected: %v", err)
	}
}

func TestWithdrawalReject_ManualReviewStatus_CanBeRejected(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusManualReview, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(1), Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("unexpected error for ManualReview→Rejected: %v", err)
	}
}

func TestWithdrawalReject_MakeTransactionFails_RollsBackAndPublishesNoJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusFailed, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{}, errors.New("journal failed")
	}
	f.events.publishTransactionRollbackFunc = func(context.Context, domain.ID) error {
		t.Fatal("journal rollback must not be published when MakeTransaction returns zero JournalID")
		return nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	if err == nil {
		t.Fatal("expected error")
	}
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestWithdrawalReject_UpdateFails_RollsBackWithoutJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusFailed, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil
	journalID := domain.NewIDFromUint64(33)

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return errors.New("update failed")
	}

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected journal id: got %v, want %v", id, journalID)
		}
		return nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback, got %d", rollbackCalls)
	}
}

func TestWithdrawalReject_CountersUpdateFails_IsLoggedOnly(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusFailed, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil
	journalID := domain.NewIDFromUint64(33)

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return errors.New("restriction service down")
	}

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		return nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("expected nil despite counter update failure, got %v", err)
	}
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback, got %d", rollbackCalls)
	}
}

func TestWithdrawalReject_Success_CommitsAndReturnsNil(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusFailed, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(1), Status: domain.JournalStatusCompleted}, nil
	}

	updateCalls := 0
	f.withdrawals.updateFunc = func(_ context.Context, _ domain.ID, p domain.UpdateWithdrawalRequestParams) error {
		updateCalls++
		if p.Status != domain.WithdrawalStatusRejected {
			t.Fatalf("expected Rejected status, got %s", p.Status.String())
		}
		return nil
	}
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return nil
	}
	f.events.publishTransactionRollbackFunc = func(context.Context, domain.ID) error {
		t.Fatal("rollback must not be published on success")
		return nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("expected 1 Update call, got %d", updateCalls)
	}
	if f.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 commit, got %d", f.txManager.CommitCalls)
	}
}

func TestWithdrawalReject_CommitFails_DoesNotUpdateCountersOrPublishJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testWithdrawalRequest(domain.WithdrawalStatusFailed, 10100000000, domain.NewIDFromUint64(0))
	req.BlockchainTxID = nil
	journalID := domain.NewIDFromUint64(33)

	f.withdrawals.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}

	countersCallCount := 0
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		countersCallCount++
		return nil
	}

	f.txManager.CommitErr = errors.New("commit failed")

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected journal id: got %v, want %v", id, journalID)
		}
		return nil
	}

	err := f.ctrl.Reject(context.Background(), req.ID)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)

	if countersCallCount != 0 {
		t.Fatalf("expected no CountersUpdate calls before failed commit, got %d", countersCallCount)
	}
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback, got %d", rollbackCalls)
	}
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected DB rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestWithdrawalProcess_Confirmed_CommitFails_DoesNotPublishJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusConfirmed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)
	journalID := domain.NewIDFromUint64(909)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.txManager.CommitErr = errors.New("commit failed")

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected journal id: got %v, want %v", id, journalID)
		}
		return nil
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback, got %d", rollbackCalls)
	}
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 DB rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestWithdrawalProcess_Confirmed_PushFails_StillReturnsNil(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	tx := testOutboundTx(domain.BlockchainTxStatusConfirmed)
	req := testWithdrawalRequest(domain.WithdrawalStatusPending, 10100000000, tx.ID)

	f.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(context.Context, domain.ID) (domain.WithdrawalRequest, error) {
		return req, nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(77), Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.events.publishNotificationPushFunc = func(context.Context, domain.PushMessage) error {
		return errors.New("queue down")
	}

	err := f.ctrl.Process(context.Background(), domain.ProcessWithdrawalParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("expected nil despite push failure, got: %v", err)
	}
}

func TestWithdrawalSendToBlockchain_Confirmed_CommitFails_DoesNotPublishJournalRollback(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)
	journalID := domain.NewIDFromUint64(55)

	f.blockchain.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
		return testOutboundTx(domain.BlockchainTxStatusConfirmed), nil
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return nil
	}
	f.txManager.CommitErr = errors.New("commit failed")

	rollbackCalls := 0
	f.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected journal id: got %v, want %v", id, journalID)
		}
		return nil
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback, got %d", rollbackCalls)
	}
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 DB rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestWithdrawalSendToBlockchain_KYTBlocked_UpdateFails_RollsBackDB(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	req := testNewWithdrawalRequest()
	setupSendHappyPath(f, req)

	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return dirtyKYT(), nil
	}
	f.withdrawals.updateFunc = func(context.Context, domain.ID, domain.UpdateWithdrawalRequestParams) error {
		return errors.New("update failed")
	}

	err := f.ctrl.SendToBlockchain(context.Background(), domain.SendWithdrawalToBlockchain{WithdrawalRequestID: req.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 DB rollback, got %d", f.txManager.RollbackCalls)
	}
}

// ============================================================
// Balance pre-check — units and short-circuit
// ============================================================

// TestWithdrawalInit_ShortBalance_RejectsBeforeAnyExternalCall is the
// regression for the scale defect.
//
// GetBalance used to return the stored balance shifted up by the asset scale
// while the check compared it against params.Amount in display units, so the
// balance read 10^(2*scale) times too large and every non-zero balance passed.
// The user still saw InsufficientFundsError — MakeTransaction rejected it under
// row locks — which is why nothing looked broken. What it cost was this: a
// hopeless request first burned a restriction gRPC call, a rate lookup, a node
// fee call and a DB transaction.
//
// Asserting the error alone would not have caught that, and did not: the
// existing insufficient-balance test passes either way. The claim worth pinning
// is that nothing downstream is reached.
func TestWithdrawalInit_ShortBalance_RejectsBeforeAnyExternalCall(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testWithdrawalChainConfig(), nil
	}
	// 99 USDT against a 100 USDT request — short by one unit, not by a factor.
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(99), nil
	}

	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		t.Error("restriction was called for a request the balance already rules out")
		return allowedWithdrawalLimitResult(), nil
	}
	f.blockchain.getFeesFunc = func(context.Context, domain.GetFeesParams) (domain.GetBlockchainFeeResult, error) {
		t.Error("node fee lookup was called for a request the balance already rules out")
		return zeroFeeResult(), nil
	}
	f.withdrawals.createFunc = func(_ context.Context, _ *domain.WithdrawalRequest) error {
		t.Error("a withdrawal row was created for a request the balance already rules out")
		return nil
	}

	err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.InsufficientFundsError)
}

// TestWithdrawalInit_ExactBalance_IsAccepted pins the comparison direction:
// a balance equal to the requested amount is enough, so the check is LessThan
// and not LessThanOrEqual.
func TestWithdrawalInit_ExactBalance_IsAccepted(t *testing.T) {
	t.Parallel()

	f := newWithdrawalFixture()
	setupInitHappyPath(f)
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(100), nil
	}

	if err := f.ctrl.Init(context.Background(), testInitParams(decimal.NewFromInt(100))); err != nil {
		t.Fatalf("exact balance was rejected: %v", err)
	}
}
