package controllers

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/cache"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/services/currency"
	restrictionsvc "github.com/whagency/core-common/services/restriction"
	usersvc "github.com/whagency/core-common/services/user"
	"github.com/whagency/core-common/testkit/dbtest"
	"github.com/whagency/core-common/testkit/errorxtest"
	"google.golang.org/grpc"
)

func TestMain(m *testing.M) {
	_ = logger.Init(logger.Config{
		AppName:      "account-service-tests",
		Environment:  "test",
		Version:      "test",
		Level:        "error",
		StdoutFormat: logger.StdoutFormatConsole,
	})
	os.Exit(m.Run())
}

// ============================================================
// Tests — DepositCtrl.Process (create path)
// ============================================================

func TestDepositProcess_CreateSuccess_RunLedgerAndPostOps(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		req.ID = domain.NewIDFromUint64(555)
		return nil
	}

	makeTxCalls := 0
	fixture.transactions.makeTransactionFunc = func(_ context.Context, params domain.MakeTxParams) (domain.MakeTxResult, error) {
		makeTxCalls++
		if params.IdempotencyKey == nil || *params.IdempotencyKey != "555" {
			t.Fatalf("unexpected idempotency key: %+v", params.IdempotencyKey)
		}
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(1), Status: domain.JournalStatusCompleted}, nil
	}

	// Counter update and push notification are both enqueued in the outbox.
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	fixture.grpcRestriction.setRequiredVerificationFunc = func(context.Context, uint64, bool) error {
		t.Fatal("SetRequiredVerification must not be called in success flow")
		return nil
	}
	fixture.users.blockUserFunc = func(context.Context, uint64) error {
		t.Fatal("BlockUser must not be called in success flow")
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if makeTxCalls != 1 {
		t.Fatalf("unexpected makeTransaction calls: got %d, want 1", makeTxCalls)
	}
	// 2 outbox rows: restriction counter update + push notification
	if outboxCreateCalls != 2 {
		t.Fatalf("unexpected outbox enqueue calls: got %d, want 2", outboxCreateCalls)
	}
	if fixture.txManager.CommitCalls != 1 {
		t.Fatalf("unexpected commit calls: got %d, want 1", fixture.txManager.CommitCalls)
	}
}

// TestDepositProcess_CreateSuccess_CounterEnqueuedViaOutbox verifies that a successful
// deposit process enqueues the restriction counter update in the outbox rather than
// making a direct gRPC call. Transient restriction-service errors are handled by the
// outbox worker with retry, so they no longer affect the controller result.
func TestDepositProcess_CreateSuccess_CounterEnqueuedViaOutbox(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		req.ID = domain.NewIDFromUint64(555)
		return nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(77), Status: domain.JournalStatusCompleted}, nil
	}
	// Counter and push notification are both enqueued in outbox.
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	rollbackCalls := 0
	fixture.events.publishTransactionRollbackFunc = func(context.Context, domain.ID) error {
		rollbackCalls++
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 outbox rows: restriction counter update + push notification
	if outboxCreateCalls != 2 {
		t.Fatalf("expected 2 outbox enqueues, got %d", outboxCreateCalls)
	}
	if rollbackCalls != 0 {
		t.Fatalf("expected no ledger rollback event, got %d", rollbackCalls)
	}
	if fixture.txManager.RollbackCalls != 0 {
		t.Fatalf("expected no db rollback, got %d", fixture.txManager.RollbackCalls)
	}
}

// ============================================================
// Tests — DepositCtrl.Process (existing deposit path)
// ============================================================

func TestDepositProcess_ExistingSuccess_SameStatus_ReturnsNil(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)
	existing := testDepositRequest(44, domain.DepositStatusSuccess, tx.ID)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return existing, nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		t.Fatal("MakeTransaction must not be called when status is already equal to target")
		return domain.MakeTxResult{}, nil
	}
	fixture.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		t.Fatal("CountersUpdate must not be called when status is already equal to target")
		return nil
	}
	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestDepositProcess_ExistingPendingToSuccess_UpdatesStatusAndRunsPostOps(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)
	existing := testDepositRequest(42, domain.DepositStatusPending, tx.ID)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return existing, nil
	}

	updateCalls := 0
	fixture.deposits.updateStatusFunc = func(_ context.Context, id domain.ID, params domain.UpdateDepositRequestStatusParams) error {
		updateCalls++
		if id != existing.ID {
			t.Fatalf("unexpected id in update: got %d, want %d", id, existing.ID)
		}
		if params.Status != domain.DepositStatusSuccess {
			t.Fatalf("unexpected target status: %s", params.Status.String())
		}
		if params.ConfirmedAt == nil || params.ConfirmedAt.IsZero() {
			t.Fatal("expected confirmedAt for success transition")
		}
		return nil
	}

	makeTxCalls := 0
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		makeTxCalls++
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(3), Status: domain.JournalStatusCompleted}, nil
	}
	// Counter and push notification are both enqueued in the outbox.
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("unexpected UpdateStatus calls: got %d, want 1", updateCalls)
	}
	if makeTxCalls != 1 {
		t.Fatalf("unexpected makeTransaction calls: got %d, want 1", makeTxCalls)
	}
	// 2 outbox rows: restriction counter update + push notification
	if outboxCreateCalls != 2 {
		t.Fatalf("unexpected outbox enqueue calls: got %d, want 2", outboxCreateCalls)
	}
	if fixture.txManager.CommitCalls != 1 {
		t.Fatalf("unexpected commit calls: got %d, want 1", fixture.txManager.CommitCalls)
	}
}

func TestDepositProcess_ExistingPendingToSuccess_LedgerFails_RollsBackTransaction(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)
	existing := testDepositRequest(46, domain.DepositStatusPending, tx.ID)
	journalID := domain.NewIDFromUint64(88)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return existing, nil
	}
	fixture.deposits.updateStatusFunc = func(context.Context, domain.ID, domain.UpdateDepositRequestStatusParams) error {
		return nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID}, svcErrors.New(appErrors.DatabaseError, appErrors.WithCause(errors.New("tx failed")))
	}
	fixture.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		t.Fatal("CountersUpdate must not be called when ledger fails")
		return nil
	}

	rollbackCalls := 0
	fixture.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected journal id: got %v, want %v", id, journalID)
		}
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if fixture.txManager.RollbackCalls != 1 {
		t.Fatalf("expected 1 db rollback, got %d", fixture.txManager.RollbackCalls)
	}
	if rollbackCalls != 0 {
		t.Fatalf("expected no ledger rollback event, got %d", rollbackCalls)
	}
}

// ============================================================
// Tests — DepositCtrl.Process (manual review / restriction)
// ============================================================

func TestDepositProcess_ManualReview_RunVerificationAndPush(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{
			OperationType: restrictionsvc.LimitOperationTypeDeposit,
			Decision:      restrictionsvc.DecisionTypeDenyLimitExceeded,
		}, nil
	}
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		req.ID = domain.NewIDFromUint64(556)
		return nil
	}

	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		t.Fatal("MakeTransaction must not be called for manual review")
		return domain.MakeTxResult{}, nil
	}
	fixture.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		t.Fatal("CountersUpdate must not be called for manual review")
		return nil
	}

	// Push notification is now enqueued via outbox (inside transaction); no counter update for manual review.
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}
	verificationCalls := 0
	fixture.grpcRestriction.setRequiredVerificationFunc = func(_ context.Context, userID uint64, required bool) error {
		verificationCalls++
		if userID != testUserID().Uint64() || !required {
			t.Fatalf("unexpected verification args: user=%d required=%v", userID, required)
		}
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verificationCalls != 1 {
		t.Fatalf("unexpected SetRequiredVerification calls: got %d, want 1", verificationCalls)
	}
	// 1 outbox row: push notification (no counter for manual review)
	if outboxCreateCalls != 1 {
		t.Fatalf("unexpected outbox enqueue calls: got %d, want 1", outboxCreateCalls)
	}
}

func TestDepositProcess_ManualReview_NonLimitDecision_RunVerificationAndPush(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{
			OperationType: restrictionsvc.LimitOperationTypeDeposit,
			Decision:      restrictionsvc.DecisionTypeDenyAccount,
		}, nil
	}
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		req.ID = domain.NewIDFromUint64(559)
		if req.Description == nil {
			t.Fatal("expected description for manual review")
		}
		return nil
	}

	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		t.Fatal("MakeTransaction must not be called for manual review")
		return domain.MakeTxResult{}, nil
	}
	fixture.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		t.Fatal("CountersUpdate must not be called for manual review")
		return nil
	}

	// Push notification is now enqueued via outbox (inside transaction); no counter update for manual review.
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, _ outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}
	verificationCalls := 0
	fixture.grpcRestriction.setRequiredVerificationFunc = func(_ context.Context, userID uint64, required bool) error {
		verificationCalls++
		if userID != testUserID().Uint64() || !required {
			t.Fatalf("unexpected verification args: user=%d required=%v", userID, required)
		}
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verificationCalls != 1 {
		t.Fatalf("unexpected SetRequiredVerification calls: got %d, want 1", verificationCalls)
	}
	// 1 outbox row: push notification (no counter for manual review)
	if outboxCreateCalls != 1 {
		t.Fatalf("unexpected outbox enqueue calls: got %d, want 1", outboxCreateCalls)
	}
}

func TestDepositProcess_RestrictionError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	// Restriction service unavailable → retry in queue; deposit must NOT be created.
	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{}, errors.New("restriction unavailable")
	}
	fixture.deposits.createFunc = func(context.Context, *domain.DepositRequest) error {
		t.Fatal("Create must not be called when restriction returns an error")
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestDepositProcess_SetRequiredVerificationFails_ReturnsNilBestEffort(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{Decision: restrictionsvc.DecisionTypeDenyLimitExceeded}, nil
	}
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		req.ID = domain.NewIDFromUint64(560)
		return nil
	}
	fixture.grpcRestriction.setRequiredVerificationFunc = func(context.Context, uint64, bool) error {
		return errors.New("restriction service down")
	}
	// Push notification is enqueued in the outbox (inside the transaction); verification failure is post-commit best-effort.

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("expected nil (best-effort), got: %v", err)
	}
	if fixture.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 commit before post-commit side effects, got %d", fixture.txManager.CommitCalls)
	}
	if fixture.txManager.RollbackCalls != 0 {
		t.Fatalf("expected no db rollback for post-commit side-effect failure, got %d", fixture.txManager.RollbackCalls)
	}
}

// ============================================================
// Tests — DepositCtrl.Process (blocked path)
// ============================================================

func TestDepositProcess_Blocked_RunBlockUser(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusBlocked)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		t.Fatal("CheckLimit must not be called for blocked deposits")
		return restrictionsvc.CheckLimitResult{}, nil
	}
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		req.ID = domain.NewIDFromUint64(557)
		if req.Status != domain.DepositStatusBlocked {
			t.Fatalf("unexpected status on create: %s", req.Status.String())
		}
		return nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		t.Fatal("MakeTransaction must not be called for blocked deposits")
		return domain.MakeTxResult{}, nil
	}

	blockCalls := 0
	fixture.users.blockUserFunc = func(context.Context, uint64) error {
		blockCalls++
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blockCalls != 1 {
		t.Fatalf("unexpected BlockUser calls: got %d, want 1", blockCalls)
	}
}

func TestDepositProcess_BlockUserFails_ReturnsNilBestEffort(t *testing.T) {
	t.Parallel()

	// BlockUser runs after commit as a best-effort side effect; failure is logged and not returned.
	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusBlocked)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return testAddressAssignment(), nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	createCalls := 0
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		createCalls++
		req.ID = domain.NewIDFromUint64(558)
		if req.Status != domain.DepositStatusBlocked {
			t.Fatalf("unexpected status on create: %s", req.Status.String())
		}
		return nil
	}
	blockUserCalls := 0
	fixture.users.blockUserFunc = func(context.Context, uint64) error {
		blockUserCalls++
		return errors.New("user service unavailable")
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("expected nil (best-effort), got: %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("expected blocked deposit to be created once, got %d", createCalls)
	}
	if blockUserCalls != 1 {
		t.Fatalf("expected BlockUser to be attempted once, got %d", blockUserCalls)
	}
	if fixture.txManager.CommitCalls != 1 {
		t.Fatalf("expected 1 commit before post-commit side effects, got %d", fixture.txManager.CommitCalls)
	}
}

// ============================================================
// Tests — DepositCtrl.Process (skip / error cases)
// ============================================================

func TestDepositProcess_CreateSkippedWhenAddressNotFound(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return domain.AddressAssignment{}, domain.ErrRecordNotFound
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		t.Fatal("GetChainConfig must not be called when address assignment is missing")
		return domain.ChainConfig{}, nil
	}
	fixture.deposits.createFunc = func(context.Context, *domain.DepositRequest) error {
		t.Fatal("Create must not be called when address assignment is missing")
		return nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		t.Fatal("MakeTransaction must not be called when deposit request is not created")
		return domain.MakeTxResult{}, nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDepositProcess_ReturnsDatabaseErrorOnGetTransactionFailure(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, errors.New("cannot read tx")
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: domain.NewIDFromUint64(1)})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestDepositProcess_ReturnsDatabaseErrorOnGetByBlockchainIDFailure(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, errors.New("select failed")
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestDepositProcess_OutboundTx_ReturnsNil(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)
	tx.Type = domain.BlockchainTxTypeOutbound

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("expected nil for outbound tx, got: %v", err)
	}
}

func TestDepositProcess_AddressAllocated_UpdatesStatusToInUse(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	ownerID := testUserID()
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return domain.AddressAssignment{
			ID:        domain.NewIDFromUint64(501),
			OwnerID:   &ownerID,
			AssetCode: tx.AssetCode,
			Network:   tx.Network,
			Purpose:   domain.PurposeAddressDeposit,
			Address:   tx.ToAddress,
			Status:    domain.AddressStatusAllocated,
		}, nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	fixture.deposits.createFunc = func(_ context.Context, req *domain.DepositRequest) error {
		req.ID = domain.NewIDFromUint64(600)
		return nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(1), Status: domain.JournalStatusCompleted}, nil
	}
	fixture.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error { return nil }

	updateStatusCalls := 0
	var capturedStatus domain.AddressStatus
	fixture.addresses.updateStatusFunc = func(_ context.Context, _ domain.ID, status domain.AddressStatus) error {
		updateStatusCalls++
		capturedStatus = status
		return nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateStatusCalls != 1 {
		t.Fatalf("expected UpdateStatus called once for Allocated address, got %d", updateStatusCalls)
	}
	if capturedStatus != domain.AddressStatusInUse {
		t.Fatalf("expected status InUse, got %v", capturedStatus)
	}
}

func TestDepositProcess_AddressUpdateStatusFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	tx := testBlockchainTx(domain.BlockchainTxStatusConfirmed)

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return tx, nil
	}
	fixture.deposits.getByBlockchainIDFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, domain.ErrRecordNotFound
	}
	ownerID := testUserID()
	fixture.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return domain.AddressAssignment{
			ID:      domain.NewIDFromUint64(501),
			OwnerID: &ownerID,
			Status:  domain.AddressStatusAllocated,
		}, nil
	}
	fixture.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	fixture.restriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	fixture.addresses.updateStatusFunc = func(context.Context, domain.ID, domain.AddressStatus) error {
		return errors.New("db connection lost")
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: tx.ID})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// Tests — DepositCtrl.Process (processing locker)
// ============================================================

func TestDepositProcess_ProcessingLockerAlreadyHeld_ReturnsConcurrencyConflictError(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	fixture.processingLocker.tryLockFunc = func(context.Context, string) error {
		return cache.KeyAlreadyLockedErr
	}

	fixture.blockchain.getTransactionByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		t.Fatal("GetTransactionByID must not be called when lock is already held")
		return domain.BlockchainTx{}, nil
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: domain.NewIDFromUint64(42)})
	errorxtest.AssertErrorCode(t, err, svcErrors.DepositRequestInProgressError)
}

func TestDepositProcess_ProcessingLockerUnexpectedError_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	fixture.processingLocker.tryLockFunc = func(context.Context, string) error {
		return errors.New("hazelcast is down")
	}

	err := fixture.ctrl.Process(context.Background(), domain.ProcessDepositParams{BlockchainTxID: domain.NewIDFromUint64(42)})
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

// ============================================================
// Approve
// ============================================================

func TestDepositApprove_Success_RunsLedgerAndPostOps(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	req := testDepositRequest(100, domain.DepositStatusManualReview, domain.NewIDFromUint64(42))

	fixture.deposits.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return req, nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(10), Status: domain.JournalStatusCompleted}, nil
	}
	fixture.deposits.updateStatusFunc = func(context.Context, domain.ID, domain.UpdateDepositRequestStatusParams) error {
		return nil
	}
	// Counter update and push notification are both enqueued in the outbox.
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	err := fixture.ctrl.Approve(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 outbox rows: restriction counter update + push notification
	if outboxCreateCalls != 2 {
		t.Fatalf("expected 2 outbox enqueues, got %d", outboxCreateCalls)
	}
}

func TestDepositApprove_GetByIDForUpdateFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	fixture.deposits.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{}, errors.New("db error")
	}

	err := fixture.ctrl.Approve(context.Background(), domain.NewIDFromUint64(1))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestDepositApprove_NotInManualReview_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	req := testDepositRequest(101, domain.DepositStatusSuccess, domain.NewIDFromUint64(42))
	fixture.deposits.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return req, nil
	}

	err := fixture.ctrl.Approve(context.Background(), req.ID)
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

func TestDepositApprove_StatusChangedBeforeCommit_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	// Simulate concurrent approval: GetByIDForUpdate sees status already changed to Success
	req := testDepositRequest(103, domain.DepositStatusSuccess, domain.NewIDFromUint64(42))
	fixture.deposits.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return req, nil
	}

	err := fixture.ctrl.Approve(context.Background(), req.ID)
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
	if fixture.txManager.CommitCalls != 0 {
		t.Fatalf("expected 0 commits when status already changed, got %d", fixture.txManager.CommitCalls)
	}
}

func TestDepositApprove_CommitFails_DoesNotPublishJournalRollback(t *testing.T) {
	t.Parallel()

	// MakeTransaction now joins the outer DB transaction, so a failed outer
	// commit rolls back the deposit status update and journal together.
	fixture := newDepositProcessFixture()
	req := testDepositRequest(102, domain.DepositStatusManualReview, domain.NewIDFromUint64(42))
	journalID := domain.NewIDFromUint64(20)

	fixture.deposits.getByIDForUpdateFunc = func(context.Context, domain.ID) (domain.DepositRequest, error) {
		return req, nil
	}
	fixture.deposits.updateStatusFunc = func(context.Context, domain.ID, domain.UpdateDepositRequestStatusParams) error {
		return nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: journalID, Status: domain.JournalStatusCompleted}, nil
	}
	fixture.restriction.countersUpdateFunc = func(context.Context, restrictionsvc.CounterUpdateParams) error {
		return nil
	}
	fixture.txManager.CommitErr = errors.New("commit failed")

	rollbackCalls := 0
	fixture.events.publishTransactionRollbackFunc = func(_ context.Context, id domain.ID) error {
		rollbackCalls++
		if id != journalID {
			t.Fatalf("unexpected journal id: got %v, want %v", id, journalID)
		}
		return nil
	}

	err := fixture.ctrl.Approve(context.Background(), req.ID)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
	if rollbackCalls != 0 {
		t.Fatalf("expected no journal rollback on commit failure, got %d", rollbackCalls)
	}
}

// ============================================================
// AutomaticManualApprove
// ============================================================

func TestDepositAutoApprove_Success_ApprovesAll(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	userID := testUserID()
	req1 := testDepositRequest(200, domain.DepositStatusManualReview, domain.NewIDFromUint64(42))
	req2 := testDepositRequest(201, domain.DepositStatusManualReview, domain.NewIDFromUint64(43))

	fixture.deposits.getManualApproveFunc = func(context.Context, domain.ID) ([]domain.DepositRequest, error) {
		return []domain.DepositRequest{req1, req2}, nil
	}
	fixture.deposits.getByIDForUpdateFunc = func(_ context.Context, id domain.ID) (domain.DepositRequest, error) {
		if id == req1.ID {
			return req1, nil
		}
		return req2, nil
	}
	fixture.transactions.makeTransactionFunc = func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(30), Status: domain.JournalStatusCompleted}, nil
	}
	fixture.deposits.updateStatusFunc = func(context.Context, domain.ID, domain.UpdateDepositRequestStatusParams) error {
		return nil
	}
	// Counter update and push notification are both enqueued in the outbox (2 deposits × 2 rows = 4 total).
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	err := fixture.ctrl.AutomaticManualApprove(context.Background(), userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 4 outbox rows: 2 restriction counter updates + 2 push notifications
	if outboxCreateCalls != 4 {
		t.Fatalf("expected 4 outbox enqueues, got %d", outboxCreateCalls)
	}
}

func TestDepositAutoApprove_GetManualApproveFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	fixture.deposits.getManualApproveFunc = func(context.Context, domain.ID) ([]domain.DepositRequest, error) {
		return nil, errors.New("db error")
	}

	err := fixture.ctrl.AutomaticManualApprove(context.Background(), testUserID())
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestDepositAutoApprove_OneItemFails_ContinuesRest(t *testing.T) {
	t.Parallel()

	fixture := newDepositProcessFixture()
	userID := testUserID()
	req1 := testDepositRequest(202, domain.DepositStatusManualReview, domain.NewIDFromUint64(42))
	req2 := testDepositRequest(203, domain.DepositStatusManualReview, domain.NewIDFromUint64(43))

	fixture.deposits.getManualApproveFunc = func(context.Context, domain.ID) ([]domain.DepositRequest, error) {
		return []domain.DepositRequest{req1, req2}, nil
	}
	fixture.deposits.getByIDForUpdateFunc = func(_ context.Context, id domain.ID) (domain.DepositRequest, error) {
		if id == req1.ID {
			return req1, nil
		}
		return req2, nil
	}
	txCalls := 0
	fixture.transactions.makeTransactionFunc = func(_ context.Context, p domain.MakeTxParams) (domain.MakeTxResult, error) {
		txCalls++
		if p.IdempotencyKey != nil && *p.IdempotencyKey == req1.ID.String() {
			return domain.MakeTxResult{}, svcErrors.New(appErrors.DatabaseError, appErrors.WithCause(errors.New("ledger fail")))
		}
		return domain.MakeTxResult{JournalID: domain.NewIDFromUint64(31), Status: domain.JournalStatusCompleted}, nil
	}
	fixture.deposits.updateStatusFunc = func(context.Context, domain.ID, domain.UpdateDepositRequestStatusParams) error {
		return nil
	}
	// req2 succeeds: 1 counter + 1 push notification = 2 outbox rows.
	outboxCreateCalls := 0
	fixture.outbox.createFunc = func(_ context.Context, p outbox.CreateParams) error {
		outboxCreateCalls++
		return nil
	}

	err := fixture.ctrl.AutomaticManualApprove(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if txCalls != 2 {
		t.Fatalf("expected MakeTransaction called for both items, got %d", txCalls)
	}
	if outboxCreateCalls != 2 {
		t.Fatalf("expected 2 outbox enqueues (counter + push for req2 only), got %d", outboxCreateCalls)
	}
}

// ============================================================
// Tests — DepositCtrl.Init
// ============================================================

func TestDepositInit_Success_ExistingAddress_ReturnsAssignmentAndRefreshes(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	assignment := testAddressAssignment()
	chainCfg := testChainConfig()

	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return chainCfg, nil
	}
	f.grpcRestriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	f.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return assignment, nil
	}
	refreshCalls := 0
	f.blockchain.refreshAddressFunc = func(context.Context, domain.RefreshAddressParams) error {
		refreshCalls++
		return nil
	}

	result, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AddressAssignment.ID != assignment.ID {
		t.Fatalf("expected assignment ID %v, got %v", assignment.ID, result.AddressAssignment.ID)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected 1 refresh call, got %d", refreshCalls)
	}
	if f.locker.UnlockCalls != 1 {
		t.Fatalf("expected Unlock called once, got %d", f.locker.UnlockCalls)
	}
}

func TestDepositInit_Success_NewAddress_AllocatesAndReturns(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	chainCfg := testChainConfig()
	newAssignment := testAddressAssignment()
	newAssignment.ID = domain.NewIDFromUint64(999)

	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return chainCfg, nil
	}
	f.grpcRestriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	f.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return domain.AddressAssignment{}, domain.ErrRecordNotFound
	}
	f.blockchain.allocateAddressFunc = func(context.Context, domain.AllocateAddressParams) (domain.AllocateAddressResult, error) {
		return domain.AllocateAddressResult{AddressAssignment: newAssignment}, nil
	}

	result, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AddressAssignment.ID != newAssignment.ID {
		t.Fatalf("expected new assignment ID %v, got %v", newAssignment.ID, result.AddressAssignment.ID)
	}
}

func TestDepositInit_LockAlreadyHeld_ReturnsDepositAlreadyInProgressError(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	f.locker.tryLockFunc = func(context.Context, string) error {
		return cache.KeyAlreadyLockedErr
	}

	_, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	errorxtest.AssertErrorCode(t, err, svcErrors.DepositAlreadyInProgressError)
}

func TestDepositInit_LockUnexpectedError_ReturnsInternalServerError(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	f.locker.tryLockFunc = func(context.Context, string) error {
		return errors.New("hazelcast down")
	}

	_, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
}

func TestDepositInit_GetChainConfigFails_ReturnsError(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return domain.ChainConfig{}, svcErrors.New(appErrors.RecordNotFoundError, appErrors.WithCause(errors.New("not found")))
	}

	_, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	errorxtest.AssertErrorCode(t, err, appErrors.RecordNotFoundError)
}

func TestDepositInit_RestrictionCheckFails_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	f.grpcRestriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{}, errors.New("restriction service down")
	}

	_, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestDepositInit_RestrictionBlocked_ReturnsRestrictedOperationError(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	f.grpcRestriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return restrictionsvc.CheckLimitResult{Decision: restrictionsvc.DecisionTypeDenyAccount}, nil
	}

	_, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	errorxtest.AssertErrorCode(t, err, appErrors.RestrictedOperationError)
}

func TestDepositInit_GetActiveFails_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	f.grpcRestriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	f.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return domain.AddressAssignment{}, errors.New("db error")
	}

	_, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestDepositInit_AllocateAddressFails_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	f.grpcRestriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	f.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return domain.AddressAssignment{}, domain.ErrRecordNotFound
	}
	f.blockchain.allocateAddressFunc = func(context.Context, domain.AllocateAddressParams) (domain.AllocateAddressResult, error) {
		return domain.AllocateAddressResult{}, errors.New("node unavailable")
	}

	_, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestDepositInit_RefreshFails_StillReturnsExistingAddress(t *testing.T) {
	t.Parallel()

	f := newDepositProcessFixture()
	assignment := testAddressAssignment()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testChainConfig(), nil
	}
	f.grpcRestriction.checkLimitFunc = func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
		return allowedLimitResult(), nil
	}
	f.addresses.getActiveFunc = func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
		return assignment, nil
	}
	f.blockchain.refreshAddressFunc = func(context.Context, domain.RefreshAddressParams) error {
		return errors.New("node timeout")
	}

	result, err := f.ctrl.Init(context.Background(), testInitDepositParams())
	if err != nil {
		t.Fatalf("refresh failure must not propagate: %v", err)
	}
	if result.AddressAssignment.ID != assignment.ID {
		t.Fatalf("expected existing assignment, got %v", result.AddressAssignment.ID)
	}
}

// ============================================================
// Helpers — Init
// ============================================================

func testInitDepositParams() domain.InitDepositParams {
	return domain.InitDepositParams{
		User:      usersvc.User{ID: 1001, Country: "UA"},
		AssetCode: currency.NewAssetCode("USDT"),
		Network:   domain.ChainNetworkTRX,
	}
}

// ============================================================
// Fixture
// ============================================================

type depositProcessFixture struct {
	ctrl             *DepositCtrl
	blockchain       *mockBlockchainController
	events           *mockEventController
	cache            *mockCacheController
	transactions     *mockTransactionController
	addresses        *mockAddressAssignmentRepo
	deposits         *mockDepositRequestRepo
	outbox           *mockOutboxRepo
	restriction      *mockRestrictionService
	grpcRestriction  *mockRestrictionGRPCService
	users            *mockUserService
	locker           *mockLocker
	processingLocker *mockLocker
	txManager        *dbtest.ObservableTxManager
}

func newDepositProcessFixture() *depositProcessFixture {
	blockchain := &mockBlockchainController{}
	events := &mockEventController{}
	cache := &mockCacheController{}
	transactions := &mockTransactionController{}
	addresses := &mockAddressAssignmentRepo{}
	deposits := &mockDepositRequestRepo{}
	outboxRepo := &mockOutboxRepo{}
	restriction := &mockRestrictionService{}
	grpcRestriction := &mockRestrictionGRPCService{}
	grpcRestriction.checkLimitFunc = restriction.CheckLimit
	grpcRestriction.countersUpdateFunc = restriction.CountersUpdate
	users := &mockUserService{}
	locker := &mockLocker{}
	processingLocker := &mockLocker{}
	txManager := &dbtest.ObservableTxManager{}

	ctrl := &DepositCtrl{
		blockchainCtrl:       blockchain,
		eventCtrl:            events,
		cacheCtrl:            cache,
		transactionCtrl:      transactions,
		addressAssignments:   addresses,
		depositRequests:      deposits,
		outboxRepo:           outboxRepo,
		restriction:          grpcRestriction,
		user:                 users,
		depositLocker:        locker,
		processingLocker:     processingLocker,
		dbTransactionManager: txManager,
	}

	return &depositProcessFixture{
		ctrl:             ctrl,
		blockchain:       blockchain,
		events:           events,
		cache:            cache,
		transactions:     transactions,
		addresses:        addresses,
		deposits:         deposits,
		outbox:           outboxRepo,
		restriction:      restriction,
		grpcRestriction:  grpcRestriction,
		users:            users,
		locker:           locker,
		processingLocker: processingLocker,
		txManager:        txManager,
	}
}

// ============================================================
// Helpers — domain builders
// ============================================================

func testUserID() domain.ID {
	return domain.NewIDFromUint64(1001)
}

func testBlockchainTx(status domain.BlockchainTxStatus) domain.BlockchainTx {
	return domain.BlockchainTx{
		ID:        domain.NewIDFromUint64(42),
		AssetCode: currency.NewAssetCode("USDT"),
		Network:   domain.ChainNetworkTRX,
		ToAddress: "TTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTT",
		Amount:    decimal.NewFromInt(100000000),
		Status:    status,
		Type:      domain.BlockchainTxTypeInbound,
	}
}

func testAddressAssignment() domain.AddressAssignment {
	ownerID := testUserID()
	return domain.AddressAssignment{
		ID:        domain.NewIDFromUint64(501),
		OwnerID:   &ownerID,
		AssetCode: currency.NewAssetCode("USDT"),
		Network:   domain.ChainNetworkTRX,
		Purpose:   domain.PurposeAddressDeposit,
		Address:   "TTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTT",
		Status:    domain.AddressStatusInUse,
	}
}

func testChainConfig() domain.ChainConfig {
	return domain.ChainConfig{
		AssetCode:      currency.NewAssetCode("USDT"),
		Network:        domain.ChainNetworkTRX,
		DepositFeeType: domain.FeeTypeFixAmount,
		DepositFee:     decimal.Zero,
	}
}

func allowedLimitResult() restrictionsvc.CheckLimitResult {
	return restrictionsvc.CheckLimitResult{
		OperationType: restrictionsvc.LimitOperationTypeDeposit,
		Decision:      restrictionsvc.DecisionTypeAllow,
	}
}

func testDepositRequest(id uint64, status domain.DepositStatus, txID domain.ID) domain.DepositRequest {
	return domain.DepositRequest{
		ID:             domain.NewIDFromUint64(id),
		UserID:         testUserID(),
		AssetCode:      currency.NewAssetCode("USDT"),
		Network:        domain.ChainNetworkTRX,
		BlockchainTxID: &txID,
		Amount:         decimal.NewFromInt(100000000),
		Fee:            decimal.Zero,
		Status:         status,
	}
}

// ============================================================
// Mocks — BlockchainController
// ============================================================

type mockBlockchainController struct {
	getChainConfigsFunc              func(context.Context) (domain.ChainConfigs, error)
	allocateAddressFunc              func(context.Context, domain.AllocateAddressParams) (domain.AllocateAddressResult, error)
	refreshAddressFunc               func(context.Context, domain.RefreshAddressParams) error
	processTransactionFunc           func(context.Context, domain.ProcessBlockchainTxParams) error
	withdrawalFunc                   func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error)
	getTransactionByIDFunc           func(context.Context, domain.ID) (domain.BlockchainTx, error)
	createDefaultChainConfigsFun     func(context.Context) error
	getFeesFunc                      func(context.Context, domain.GetFeesParams) (domain.GetBlockchainFeeResult, error)
	pollTransactionStatusFunc        func(context.Context, domain.ID) (domain.TransactionStatusResult, error)
	findStuckInboundForReconcileFunc func(context.Context, time.Duration, int) ([]domain.BlockchainTx, error)
}

func (m *mockBlockchainController) GetChainConfigs(ctx context.Context) (domain.ChainConfigs, error) {
	if m.getChainConfigsFunc == nil {
		panic("unexpected call: Blockchain.GetChainConfigs")
	}
	return m.getChainConfigsFunc(ctx)
}

func (m *mockBlockchainController) AllocateAddress(ctx context.Context, params domain.AllocateAddressParams) (domain.AllocateAddressResult, error) {
	if m.allocateAddressFunc == nil {
		panic("unexpected call: Blockchain.AllocateAddress")
	}
	return m.allocateAddressFunc(ctx, params)
}

func (m *mockBlockchainController) RefreshAddress(ctx context.Context, params domain.RefreshAddressParams) error {
	if m.refreshAddressFunc == nil {
		panic("unexpected call: Blockchain.RefreshAddress")
	}
	return m.refreshAddressFunc(ctx, params)
}

func (m *mockBlockchainController) ProcessTransaction(ctx context.Context, params domain.ProcessBlockchainTxParams) error {
	if m.processTransactionFunc == nil {
		panic("unexpected call: Blockchain.ProcessTransaction")
	}
	return m.processTransactionFunc(ctx, params)
}

func (m *mockBlockchainController) Withdrawal(ctx context.Context, params domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
	if m.withdrawalFunc == nil {
		panic("unexpected call: Blockchain.Withdrawal")
	}
	return m.withdrawalFunc(ctx, params)
}

func (m *mockBlockchainController) GetTransactionByID(ctx context.Context, id domain.ID) (domain.BlockchainTx, error) {
	if m.getTransactionByIDFunc == nil {
		panic("unexpected call: Blockchain.GetTransactionByID")
	}
	return m.getTransactionByIDFunc(ctx, id)
}

func (m *mockBlockchainController) CreateDefaultChainConfigs(ctx context.Context) error {
	if m.createDefaultChainConfigsFun == nil {
		panic("unexpected call: Blockchain.CreateDefaultChainConfigs")
	}
	return m.createDefaultChainConfigsFun(ctx)
}

func (m *mockBlockchainController) GetFees(ctx context.Context, params domain.GetFeesParams) (domain.GetBlockchainFeeResult, error) {
	if m.getFeesFunc == nil {
		panic("unexpected call: Blockchain.GetFees")
	}
	return m.getFeesFunc(ctx, params)
}

func (m *mockBlockchainController) PollTransactionStatus(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
	if m.pollTransactionStatusFunc == nil {
		panic("unexpected call: Blockchain.PollTransactionStatus")
	}
	return m.pollTransactionStatusFunc(ctx, id)
}

func (m *mockBlockchainController) FindStuckInboundForReconcile(ctx context.Context, olderThan time.Duration, limit int) ([]domain.BlockchainTx, error) {
	if m.findStuckInboundForReconcileFunc == nil {
		panic("unexpected call: Blockchain.FindStuckInboundForReconcile")
	}
	return m.findStuckInboundForReconcileFunc(ctx, olderThan, limit)
}

// ============================================================
// Mocks — EventController
// ============================================================

type mockEventController struct {
	publishNotificationPushFunc          func(context.Context, domain.PushMessage) error
	publishTransactionRollbackFunc       func(context.Context, domain.ID) error
	publishBlockchainTransactionUpdateFn func(context.Context, domain.BlockchainTransactionUpdateEvent) error
	publishWithdrawalCreatedFunc         func(context.Context, domain.WithdrawalRequestCreatedEvent) error
}

func (m *mockEventController) PublishNotificationPush(ctx context.Context, msg domain.PushMessage) error {
	if m.publishNotificationPushFunc == nil {
		return nil
	}
	return m.publishNotificationPushFunc(ctx, msg)
}

func (m *mockEventController) PublishTransactionRollback(ctx context.Context, journalID domain.ID) error {
	if m.publishTransactionRollbackFunc == nil {
		panic("unexpected call: Event.PublishTransactionRollback")
	}
	return m.publishTransactionRollbackFunc(ctx, journalID)
}

func (m *mockEventController) PublishBlockchainTransactionUpdate(ctx context.Context, ev domain.BlockchainTransactionUpdateEvent) error {
	if m.publishBlockchainTransactionUpdateFn == nil {
		return nil
	}
	return m.publishBlockchainTransactionUpdateFn(ctx, ev)
}

func (m *mockEventController) PublishWithdrawalCreated(ctx context.Context, ev domain.WithdrawalRequestCreatedEvent) error {
	if m.publishWithdrawalCreatedFunc == nil {
		return nil
	}
	return m.publishWithdrawalCreatedFunc(ctx, ev)
}

// ============================================================
// Mocks — CacheController
// ============================================================

type mockCacheController struct {
	getAssetFunc       func(context.Context, currency.AssetCode) (currency.Asset, error)
	getAllAssetsFunc   func(context.Context) (currency.AssetMap, error)
	getChainConfigFunc func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error)
}

func (m *mockCacheController) GetAsset(ctx context.Context, code currency.AssetCode) (currency.Asset, error) {
	if m.getAssetFunc == nil {
		panic("unexpected call: Cache.GetAsset")
	}
	return m.getAssetFunc(ctx, code)
}

func (m *mockCacheController) GetAllAssets(ctx context.Context) (currency.AssetMap, error) {
	if m.getAllAssetsFunc == nil {
		panic("unexpected call: Cache.GetAllAssets")
	}
	return m.getAllAssetsFunc(ctx)
}

func (m *mockCacheController) GetChainConfig(ctx context.Context, code currency.AssetCode, network domain.ChainNetwork) (domain.ChainConfig, error) {
	if m.getChainConfigFunc == nil {
		panic("unexpected call: Cache.GetChainConfig")
	}
	return m.getChainConfigFunc(ctx, code, network)
}

// ============================================================
// Mocks — TransactionController
// ============================================================

type mockTransactionController struct {
	makeTransactionFunc func(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error)
}

func (m *mockTransactionController) MakeTransaction(ctx context.Context, params domain.MakeTxParams) (domain.MakeTxResult, error) {
	if m.makeTransactionFunc == nil {
		panic("unexpected call: Transaction.MakeTransaction")
	}
	return m.makeTransactionFunc(ctx, params)
}

func (m *mockTransactionController) RollbackTransaction(_ context.Context, _ domain.ID) error {
	return nil
}

func (m *mockTransactionController) FindJournalByIdempotencyKey(
	_ context.Context,
	_ domain.FindJournalParams,
) (domain.FindJournalResult, error) {
	return domain.FindJournalResult{}, nil
}

// ============================================================
// Mocks — AddressAssignmentRepository
// ============================================================

type mockAddressAssignmentRepo struct {
	createFunc       func(context.Context, *domain.AddressAssignment) error
	getActiveFunc    func(context.Context, domain.GetActiveAddressParams) (domain.AddressAssignment, error)
	updateStatusFunc func(context.Context, domain.ID, domain.AddressStatus) error
}

func (m *mockAddressAssignmentRepo) Create(ctx context.Context, a *domain.AddressAssignment) error {
	if m.createFunc == nil {
		panic("unexpected call: AddressAssignment.Create")
	}
	return m.createFunc(ctx, a)
}

func (m *mockAddressAssignmentRepo) GetActive(ctx context.Context, params domain.GetActiveAddressParams) (domain.AddressAssignment, error) {
	if m.getActiveFunc == nil {
		panic("unexpected call: AddressAssignment.GetActive")
	}
	return m.getActiveFunc(ctx, params)
}

func (m *mockAddressAssignmentRepo) UpdateStatus(ctx context.Context, id domain.ID, status domain.AddressStatus) error {
	if m.updateStatusFunc == nil {
		panic("unexpected call: AddressAssignment.UpdateStatus")
	}
	return m.updateStatusFunc(ctx, id, status)
}

func (m *mockAddressAssignmentRepo) WithTxFromContext(_ context.Context) domain.AddressAssignmentRepository {
	return m
}

// ============================================================
// Mocks — DepositRequestRepository
// ============================================================

type mockDepositRequestRepo struct {
	createFunc            func(context.Context, *domain.DepositRequest) error
	getByParamsFunc       func(context.Context, domain.GetDepositRequestParams) (domain.DepositRequest, error)
	getByBlockchainIDFunc func(context.Context, domain.ID) (domain.DepositRequest, error)
	updateStatusFunc      func(context.Context, domain.ID, domain.UpdateDepositRequestStatusParams) error
	getByIDFunc           func(context.Context, domain.ID) (domain.DepositRequest, error)
	getByIDForUpdateFunc  func(context.Context, domain.ID) (domain.DepositRequest, error)
	getManualApproveFunc  func(context.Context, domain.ID) ([]domain.DepositRequest, error)
	depositListFunc       func(context.Context, domain.ID, domain.DepositFilter) ([]domain.DepositRequestDetails, int64, error)
	withTxFromContextFunc func(context.Context) domain.DepositRequestRepository
}

func (m *mockDepositRequestRepo) Create(ctx context.Context, request *domain.DepositRequest) error {
	if m.createFunc == nil {
		panic("unexpected call: DepositRequest.Create")
	}
	return m.createFunc(ctx, request)
}

func (m *mockDepositRequestRepo) GetByParams(ctx context.Context, params domain.GetDepositRequestParams) (domain.DepositRequest, error) {
	if m.getByParamsFunc == nil {
		panic("unexpected call: DepositRequest.GetByParams")
	}
	return m.getByParamsFunc(ctx, params)
}

func (m *mockDepositRequestRepo) GetByBlockchainID(ctx context.Context, id domain.ID) (domain.DepositRequest, error) {
	if m.getByBlockchainIDFunc == nil {
		panic("unexpected call: DepositRequest.GetByBlockchainID")
	}
	return m.getByBlockchainIDFunc(ctx, id)
}

func (m *mockDepositRequestRepo) UpdateStatus(ctx context.Context, id domain.ID, params domain.UpdateDepositRequestStatusParams) error {
	if m.updateStatusFunc == nil {
		panic("unexpected call: DepositRequest.UpdateStatus")
	}
	return m.updateStatusFunc(ctx, id, params)
}

func (m *mockDepositRequestRepo) GetByID(ctx context.Context, id domain.ID) (domain.DepositRequest, error) {
	if m.getByIDFunc == nil {
		panic("unexpected call: DepositRequest.GetByID")
	}
	return m.getByIDFunc(ctx, id)
}

func (m *mockDepositRequestRepo) GetByIDForUpdate(ctx context.Context, id domain.ID) (domain.DepositRequest, error) {
	if m.getByIDForUpdateFunc == nil {
		panic("unexpected call: DepositRequest.GetByIDForUpdate")
	}
	return m.getByIDForUpdateFunc(ctx, id)
}

func (m *mockDepositRequestRepo) GetManualApprove(ctx context.Context, id domain.ID) ([]domain.DepositRequest, error) {
	if m.getManualApproveFunc == nil {
		panic("unexpected call: DepositRequest.GetManualApprove")
	}
	return m.getManualApproveFunc(ctx, id)
}

func (m *mockDepositRequestRepo) DepositList(ctx context.Context, userID domain.ID, filter domain.DepositFilter) ([]domain.DepositRequestDetails, int64, error) {
	if m.depositListFunc == nil {
		panic("unexpected call: DepositRequest.DepositList")
	}
	return m.depositListFunc(ctx, userID, filter)
}

func (m *mockDepositRequestRepo) WithTxFromContext(ctx context.Context) domain.DepositRequestRepository {
	if m.withTxFromContextFunc != nil {
		return m.withTxFromContextFunc(ctx)
	}
	return m // default: return self so Create/UpdateStatus work as usual
}

// ============================================================
// Mocks — RestrictionService (plain, used for delegation)
// ============================================================

type mockRestrictionService struct {
	checkLimitFunc              func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error)
	countersUpdateFunc          func(context.Context, restrictionsvc.CounterUpdateParams) error
	setRequiredVerificationFunc func(context.Context, uint64, bool) error
}

func (m *mockRestrictionService) CheckLimit(ctx context.Context, params restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
	if m.checkLimitFunc == nil {
		panic("unexpected call: Restriction.CheckLimit")
	}
	return m.checkLimitFunc(ctx, params)
}

func (m *mockRestrictionService) CountersUpdate(ctx context.Context, params restrictionsvc.CounterUpdateParams) error {
	if m.countersUpdateFunc == nil {
		panic("unexpected call: Restriction.CountersUpdate")
	}
	return m.countersUpdateFunc(ctx, params)
}

func (m *mockRestrictionService) SetRequiredVerification(ctx context.Context, userID uint64, required bool) error {
	if m.setRequiredVerificationFunc == nil {
		panic("unexpected call: Restriction.SetRequiredVerification")
	}
	return m.setRequiredVerificationFunc(ctx, userID, required)
}

// ============================================================
// Mocks — RestrictionGRPCService
// ============================================================

type mockRestrictionGRPCService struct {
	setRequiredVerificationFunc func(context.Context, uint64, bool) error
	checkLimitFunc              func(context.Context, restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error)
	countersUpdateFunc          func(context.Context, restrictionsvc.CounterUpdateParams) error
	getConnectionFunc           func() (*grpc.ClientConn, error)
	closeFunc                   func() error
	resetFunc                   func() error
}

func (m *mockRestrictionGRPCService) CheckLimit(ctx context.Context, params restrictionsvc.CheckLimitParams) (restrictionsvc.CheckLimitResult, error) {
	if m.checkLimitFunc == nil {
		panic("unexpected call: RestrictionGRPC.CheckLimit")
	}
	return m.checkLimitFunc(ctx, params)
}

func (m *mockRestrictionGRPCService) CountersUpdate(ctx context.Context, params restrictionsvc.CounterUpdateParams) error {
	if m.countersUpdateFunc == nil {
		panic("unexpected call: RestrictionGRPC.CountersUpdate")
	}
	return m.countersUpdateFunc(ctx, params)
}

func (m *mockRestrictionGRPCService) GetConnection() (*grpc.ClientConn, error) {
	if m.getConnectionFunc == nil {
		return nil, nil
	}
	return m.getConnectionFunc()
}

func (m *mockRestrictionGRPCService) Close() error {
	if m.closeFunc == nil {
		return nil
	}
	return m.closeFunc()
}

func (m *mockRestrictionGRPCService) Reset() error {
	if m.resetFunc == nil {
		return nil
	}
	return m.resetFunc()
}

func (m *mockRestrictionGRPCService) SetRequiredVerification(ctx context.Context, userID uint64, required bool) error {
	if m.setRequiredVerificationFunc == nil {
		panic("unexpected call: RestrictionGRPC.SetRequiredVerification")
	}
	return m.setRequiredVerificationFunc(ctx, userID, required)
}

// ============================================================
// Mocks — UserService
// ============================================================

type mockUserService struct {
	validateSessionFunc func(context.Context, string, bool) (usersvc.Session, error)
	blockUserFunc       func(context.Context, uint64) error
}

func (m *mockUserService) ValidateSession(ctx context.Context, token string, updateActivity bool) (usersvc.Session, error) {
	if m.validateSessionFunc == nil {
		panic("unexpected call: User.ValidateSession")
	}
	return m.validateSessionFunc(ctx, token, updateActivity)
}

func (m *mockUserService) BlockUser(ctx context.Context, userID uint64) error {
	if m.blockUserFunc == nil {
		panic("unexpected call: User.BlockUser")
	}
	return m.blockUserFunc(ctx, userID)
}

// mockOutboxRepo is a minimal in-process outbox mock shared across controller tests.
// It satisfies domain.OutboxRepository; only Create and WithTxFromContext are used by controllers.
type mockOutboxRepo struct {
	createFunc func(context.Context, outbox.CreateParams) error
}

func (m *mockOutboxRepo) Create(ctx context.Context, p outbox.CreateParams) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, p)
	}
	return nil
}

func (m *mockOutboxRepo) WithTxFromContext(_ context.Context) outbox.Repository {
	return m
}

func (m *mockOutboxRepo) ClaimDue(_ context.Context, _ string, _ time.Duration, _ int) ([]outbox.Message, error) {
	return nil, nil
}

func (m *mockOutboxRepo) MarkDelivered(_ context.Context, _ int64, _ string) error { return nil }

func (m *mockOutboxRepo) MarkFailed(_ context.Context, _ int64, _, _ string, _ time.Time) error {
	return nil
}

func (m *mockOutboxRepo) MarkDead(_ context.Context, _ int64, _, _ string) error { return nil }

func (m *mockOutboxRepo) OldestPendingAge(_ context.Context) (map[string]time.Duration, error) {
	return nil, nil
}

func (m *mockOutboxRepo) Prune(_ context.Context, _, _ time.Duration) (int64, error) {
	return 0, nil
}

func (m *mockOutboxRepo) ClaimByDedupeKey(_ context.Context, _ string, _ time.Duration, _ string) (outbox.Message, bool, error) {
	return outbox.Message{}, false, nil
}
