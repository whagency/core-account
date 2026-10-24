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
	"google.golang.org/grpc"
)

// ============================================================
// Tests — TransferCtrl.Init
// ============================================================

func TestTransferInit_Success_CommitsAndEnqueuesOutboxMessages(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	outboxCreateCount := 0
	f.outbox.createFunc = func(_ context.Context, _ outbox.CreateParams) error {
		outboxCreateCount++
		return nil
	}

	result, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JournalID == 0 {
		t.Fatal("expected non-zero journal ID")
	}
	if result.Status != domain.JournalStatusCompleted {
		t.Fatalf("unexpected status: got %v, want Completed", result.Status)
	}
	if f.txManager.BeginCalls != 1 {
		t.Fatalf("unexpected begin calls: got %d, want 1", f.txManager.BeginCalls)
	}
	if f.txManager.CommitCalls != 1 {
		t.Fatalf("unexpected commit calls: got %d, want 1", f.txManager.CommitCalls)
	}
	if f.txManager.RollbackCalls != 0 {
		t.Fatalf("unexpected rollback calls: got %d, want 0", f.txManager.RollbackCalls)
	}
	// 3 outbox rows: 1 restriction counter update + 2 push notifications (outgoing + incoming)
	if outboxCreateCount != 3 {
		t.Fatalf("expected 3 outbox rows, got %d", outboxCreateCount)
	}
	if f.locker.UnlockCalls != 1 {
		t.Fatalf("expected Unlock to be called once, got %d", f.locker.UnlockCalls)
	}
}

func TestTransferInit_LockAlreadyHeld_ReturnsTransferAlreadyInProgressError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.locker.tryLockFunc = func(context.Context, string) error {
		return cache.KeyAlreadyLockedErr
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, svcErrors.TransferAlreadyInProgressError)
}

func TestTransferInit_LockUnexpectedError_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.locker.tryLockFunc = func(context.Context, string) error {
		return errors.New("hazelcast connection lost")
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

func TestTransferInit_GetBalanceFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return decimal.Zero, errors.New("db error")
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestTransferInit_InsufficientBalance_ReturnsInsufficientFundsError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(50), nil // less than amount=100
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.InsufficientFundsError)
}

func TestTransferInit_RestrictionCheckFails_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{}, errors.New("restriction service down")
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestTransferInit_RestrictionBlocked_ReturnsRestrictedOperationError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{
			Decision: restrictionsvc.DecisionTypeDenyAccount,
		}, nil
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.RestrictedOperationError)
}

func TestTransferInit_GetUserByNumberFails_ReturnsUserNumberNotFoundError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.userGRPC.getUserByNumberFunc = func(context.Context, uint64) (usersvc.User, error) {
		return usersvc.User{}, errors.New("user not found")
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, svcErrors.UserNumberNotFoundError)
}

func TestTransferInit_SelfTransfer_ReturnsSelfPaymentError(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	// receiver is same as sender
	f.userGRPC.getUserByNumberFunc = func(context.Context, uint64) (usersvc.User, error) {
		return usersvc.User{ID: f.senderID}, nil
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, svcErrors.SelfPaymentError)
}

func TestTransferInit_CreateRequestFails_ReturnsDatabaseError_NoJournalRollback(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.transfers.createFunc = func(context.Context, *domain.TransferRequest) error {
		return errors.New("db insert failed")
	}
	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected DB rollback, got %d calls", f.txManager.RollbackCalls)
	}
}

func TestTransferInit_MakeTransactionFails_RollsBackDB_NoJournalRollback(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{}, svcErrors.New(appErrors.DatabaseError, appErrors.WithCause(errors.New("journal failed")))
	}
	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 DB rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestTransferInit_CommitFails_ReturnsDatabaseError_NoJournalRollback(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.txManager.CommitErr = errors.New("commit failed")
	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 DB rollback, got %d", f.txManager.RollbackCalls)
	}
}

func TestTransferInit_CountersUpdateFails_StillReturnsSuccess(t *testing.T) {
	t.Parallel()

	// CountersUpdate is now async (outbox worker). A restriction service error does not
	// affect Init; the outbox row is enqueued and the worker retries independently.
	f := newTransferFixture()
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return errors.New("counters update failed")
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	if err != nil {
		t.Fatalf("expected success despite counters error, got: %v", err)
	}
}

func TestTransferInit_NotificationOutboxFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	// Push notification rows are now enqueued inside the transaction.
	// An outbox Create failure rolls back the whole transaction and returns DatabaseError.
	f := newTransferFixture()
	outboxCallCount := 0
	f.outbox.createFunc = func(_ context.Context, _ outbox.CreateParams) error {
		outboxCallCount++
		// Fail on the second create (push notification rows start at index 2)
		if outboxCallCount >= 2 {
			return errors.New("db insert failed")
		}
		return nil
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if f.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 DB rollback, got %d", f.txManager.RollbackCalls)
	}
}

// ============================================================
// Fixture and helpers
// ============================================================

type transferFixture struct {
	ctrl         *TransferCtrl
	account      *mockAccountController
	transactions *mockTransactionController
	transfers    *mockTransferRequestRepo
	outbox       *mockOutboxRepo
	restriction  *mockRestrictionService
	userGRPC     *mockUserGRPCService
	locker       *mockLocker
	txManager    *dbtest.ObservableTxManager

	senderID     uint64
	receiverID   uint64
	toUserNumber uint64
}

func newTransferFixture() *transferFixture {
	senderID := uint64(1001)
	receiverID := uint64(2002)
	toUserNumber := uint64(999)

	f := &transferFixture{
		account:      &mockAccountController{},
		transactions: &mockTransactionController{},
		transfers:    &mockTransferRequestRepo{},
		outbox:       &mockOutboxRepo{},
		restriction:  &mockRestrictionService{},
		userGRPC:     &mockUserGRPCService{},
		locker:       &mockLocker{},
		txManager:    &dbtest.ObservableTxManager{},
		senderID:     senderID,
		receiverID:   receiverID,
		toUserNumber: toUserNumber,
	}

	// Happy-path defaults
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(10_000), nil // enough for any test amount
	}
	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{Decision: restrictionsvc.DecisionTypeAllow}, nil
	}
	f.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return nil
	}
	f.userGRPC.getUserByNumberFunc = func(_ context.Context, number uint64) (usersvc.User, error) {
		return usersvc.User{ID: receiverID, Number: number}, nil
	}
	f.transfers.createFunc = func(_ context.Context, r *domain.TransferRequest) error {
		r.ID = domain.NewIDFromUint64(500)
		r.CreatedAt = time.Now().UTC()
		return nil
	}
	f.transfers.withTxFromContextFunc = func(_ context.Context) domain.TransferRequestRepository {
		return f.transfers
	}
	f.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{
			JournalID: domain.NewIDFromUint64(77),
			Status:    domain.JournalStatusCompleted,
		}, nil
	}
	f.ctrl = NewTransferController(
		f.account,
		f.transactions,
		f.transfers,
		f.outbox,
		f.restriction,
		f.userGRPC,
		f.locker,
		f.txManager,
	)

	return f
}

func testTransferParams(senderID uint64, toUserNumber uint64, amount decimal.Decimal) domain.TransferParams {
	return domain.TransferParams{
		User:         usersvc.User{ID: senderID, Number: 100, Country: "UA"},
		ToUserNumber: domain.NewIDFromUint64(toUserNumber),
		AssetCode:    currency.DefaultAccountAssetCode,
		Amount:       amount,
	}
}

// ============================================================
// New mocks — UserGRPCService, TransferRequestRepository
// ============================================================

type mockUserGRPCService struct {
	validateSessionFunc func(context.Context, string, bool) (usersvc.Session, error)
	getUserByNumberFunc func(context.Context, uint64) (usersvc.User, error)
}

func (m *mockUserGRPCService) ValidateSession(ctx context.Context, token string, updateActivity bool) (usersvc.Session, error) {
	if m.validateSessionFunc != nil {
		return m.validateSessionFunc(ctx, token, updateActivity)
	}
	panic("unexpected call: UserGRPCService.ValidateSession")
}

func (m *mockUserGRPCService) GetUserByNumber(ctx context.Context, number uint64) (usersvc.User, error) {
	if m.getUserByNumberFunc != nil {
		return m.getUserByNumberFunc(ctx, number)
	}
	panic("unexpected call: UserGRPCService.GetUserByNumber")
}

func (m *mockUserGRPCService) GetConnection() (*grpc.ClientConn, error) {
	panic("unexpected call: UserGRPCService.GetConnection")
}

func (m *mockUserGRPCService) Close() error {
	return nil
}

func (m *mockUserGRPCService) Reset() error {
	return nil
}

type mockTransferRequestRepo struct {
	createFunc            func(context.Context, *domain.TransferRequest) error
	transferListFunc      func(context.Context, domain.ID, domain.TransferFilter) ([]domain.TransferRequest, int64, error)
	withTxFromContextFunc func(context.Context) domain.TransferRequestRepository
}

func (m *mockTransferRequestRepo) Create(ctx context.Context, r *domain.TransferRequest) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, r)
	}
	panic("unexpected call: TransferRequestRepo.Create")
}

func (m *mockTransferRequestRepo) TransferList(ctx context.Context, id domain.ID, f domain.TransferFilter) ([]domain.TransferRequest, int64, error) {
	if m.transferListFunc != nil {
		return m.transferListFunc(ctx, id, f)
	}
	panic("unexpected call: TransferRequestRepo.TransferList")
}

func (m *mockTransferRequestRepo) WithTxFromContext(ctx context.Context) domain.TransferRequestRepository {
	if m.withTxFromContextFunc != nil {
		return m.withTxFromContextFunc(ctx)
	}
	return m
}

// ============================================================
// Balance pre-check — units and short-circuit
// ============================================================

// TestTransferInit_ShortBalance_RejectsBeforeAnyExternalCall is the transfer
// side of the scale defect. GetBalance used to return the stored balance
// shifted up by the asset scale while this check compared it against
// params.Amount in display units, so any non-zero balance passed.
//
// The reason to assert on the calls rather than only the error: reaching
// GetUserByNumber means an unfunded caller can resolve other people's account
// numbers and tell an existing one from a missing one by the error code, before
// anything checks that they can afford the transfer.
func TestTransferInit_ShortBalance_RejectsBeforeAnyExternalCall(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	// 99 USDT against a 100 USDT transfer — short by one unit, not by a factor.
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(99), nil
	}

	f.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		t.Error("restriction was called for a transfer the balance already rules out")
		return restrictionsvc.CheckLimitResult{Decision: restrictionsvc.DecisionTypeAllow}, nil
	}
	f.userGRPC.getUserByNumberFunc = func(_ context.Context, number uint64) (usersvc.User, error) {
		t.Error("recipient lookup ran for a transfer the balance already rules out")
		return usersvc.User{ID: f.receiverID, Number: number}, nil
	}
	f.transfers.createFunc = func(_ context.Context, _ *domain.TransferRequest) error {
		t.Error("a transfer row was created for a transfer the balance already rules out")
		return nil
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	errorxtest.AssertErrorCode(t, err, appErrors.InsufficientFundsError)
}

// TestTransferInit_ExactBalance_IsAccepted pins the comparison direction: a
// balance equal to the amount is enough, so the check is LessThan and not
// LessThanOrEqual.
func TestTransferInit_ExactBalance_IsAccepted(t *testing.T) {
	t.Parallel()

	f := newTransferFixture()
	f.account.getBalanceFunc = func(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
		return quantizedBalance(100), nil
	}

	_, err := f.ctrl.Init(context.Background(), testTransferParams(f.senderID, f.toUserNumber, decimal.NewFromInt(100)))
	if err != nil {
		t.Fatalf("exact balance was rejected: %v", err)
	}
}
