package controllers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/cache"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/services/currency"
)

// Reconciliation tests target the decision logic in reconcilePendingOne and
// reconcileNewOne — what gets called, and what does not, for each branch.
// Process() and SendToBlockchain() internals are covered by their own tests;
// here we only assert the reconcile path's outcomes by wiring just-enough
// mocks for the controller calls to reach (or short-circuit at) the
// behavior we want to verify.

func reconcilePendingFixture(t *testing.T) (*withdrawalFixture, *domain.WithdrawalRequest, domain.BlockchainTx) {
	t.Helper()
	f := newWithdrawalFixture()
	bcID := domain.NewIDFromUint64(99)
	withdrawal := &domain.WithdrawalRequest{
		ID:             domain.NewIDFromUint64(1),
		AssetCode:      currency.DefaultAccountAssetCode,
		Network:        domain.ChainNetworkTRX,
		Status:         domain.WithdrawalStatusPending,
		BlockchainTxID: &bcID,
		Amount:         decimal.NewFromInt(1_000_000),
	}
	hash := "tx-hash"
	bcTx := domain.BlockchainTx{
		ID:                bcID,
		ExternalRequestID: "ext-1",
		TxID:              &hash,
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeOutbound,
		Amount:            decimal.NewFromInt(1_000_000),
		Status:            domain.BlockchainTxStatusPending,
	}
	return f, withdrawal, bcTx
}

func TestReconcilePendingOne_StillPending(t *testing.T) {
	f, w, _ := reconcilePendingFixture(t)
	called := 0
	f.blockchain.pollTransactionStatusFunc = func(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
		called++
		return domain.TransactionStatusResult{Status: domain.BlockchainTxStatusPending, Confirmations: 3, RequiredConfirmations: 20}, nil
	}
	// Neither GetTransactionByID nor ProcessTransaction nor Process should
	// be called when the node still reports non-terminal — the mock would
	// panic on unexpected calls.
	f.ctrl.reconcilePendingOne(context.Background(), *w)
	if called != 1 {
		t.Fatalf("expected 1 PollTransactionStatus call, got %d", called)
	}
}

func TestReconcilePendingOne_InvalidStateNoBlockchainTxID(t *testing.T) {
	f, w, _ := reconcilePendingFixture(t)
	w.BlockchainTxID = nil
	// No external calls at all; no mocks wired => any unexpected call panics.
	f.ctrl.reconcilePendingOne(context.Background(), *w)
}

func TestReconcilePendingOne_NodeUnreachable(t *testing.T) {
	f, w, _ := reconcilePendingFixture(t)
	f.blockchain.pollTransactionStatusFunc = func(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
		return domain.TransactionStatusResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(errors.New("node down")),
		)
	}
	f.ctrl.reconcilePendingOne(context.Background(), *w)
}

func TestReconcilePendingOne_BlockchainTxMissingHashWaitsForNextTick(t *testing.T) {
	f, w, _ := reconcilePendingFixture(t)
	pollCalled := 0
	f.blockchain.pollTransactionStatusFunc = func(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
		pollCalled++
		return domain.TransactionStatusResult{}, svcErrors.New(
			appErrors.InvalidRequestError,
			appErrors.WithCause(errors.New("blockchain tx has no on-chain hash")),
		)
	}
	// No GetTransactionByID / ProcessTransaction / Process wiring: the test
	// passes only if reconcile treats this as waiting-for-hash and exits.
	f.ctrl.reconcilePendingOne(context.Background(), *w)
	if pollCalled != 1 {
		t.Fatalf("expected 1 PollTransactionStatus call, got %d", pollCalled)
	}
}

func TestReconcilePendingOne_TerminalConfirmed_Recovers(t *testing.T) {
	f, w, bcTx := reconcilePendingFixture(t)

	pollCalled, getTxCalled, processTxCalled, processCalled := 0, 0, 0, 0
	freshTxID := "broadcasted-hash"
	f.blockchain.pollTransactionStatusFunc = func(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
		pollCalled++
		return domain.TransactionStatusResult{TxID: &freshTxID, Status: domain.BlockchainTxStatusConfirmed, Confirmations: 25, RequiredConfirmations: 20}, nil
	}
	f.blockchain.getTransactionByIDFunc = func(ctx context.Context, id domain.ID) (domain.BlockchainTx, error) {
		getTxCalled++
		return bcTx, nil
	}
	f.blockchain.processTransactionFunc = func(ctx context.Context, params domain.ProcessBlockchainTxParams) error {
		processTxCalled++
		// Verify reconciliation passed through the freshly polled values.
		if params.Status != domain.BlockchainTxStatusConfirmed {
			t.Fatalf("expected ProcessTransaction status=Confirmed, got %v", params.Status)
		}
		if params.Confirmations != 25 || params.RequiredConfirmations != 20 {
			t.Fatalf("expected (25, 20) confirmations, got (%d, %d)", params.Confirmations, params.RequiredConfirmations)
		}
		if params.ExternalRequestID != bcTx.ExternalRequestID {
			t.Fatalf("expected ExternalRequestID=%q, got %q", bcTx.ExternalRequestID, params.ExternalRequestID)
		}
		if params.TxID == nil || *params.TxID != freshTxID {
			t.Fatalf("expected ProcessTransaction txID=%q, got %v", freshTxID, params.TxID)
		}
		return nil
	}
	// Process(BlockchainTxID) → controller-internal flow. We short-circuit
	// it via the status guard inside Process: the loaded withdrawal is no
	// longer Pending, so Process returns nil immediately without writing.
	f.withdrawals.withTxFromCtxFunc = func(ctx context.Context) domain.WithdrawalRequestRepository {
		return f.withdrawals
	}
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
		processCalled++
		return domain.WithdrawalRequest{ID: w.ID, Status: domain.WithdrawalStatusSuccess}, nil
	}
	// Process loads BlockchainTx by ID via the same mock used above.

	f.ctrl.reconcilePendingOne(context.Background(), *w)

	// GetTransactionByID is called twice: once by reconcilePendingOne to
	// build ProcessTransaction params, and once inside Process when it
	// loads the BlockchainTx to drive the withdrawal status transition.
	if pollCalled != 1 || getTxCalled != 2 || processTxCalled != 1 || processCalled != 1 {
		t.Fatalf("expected (poll=1, getTx=2, processTx=1, process=1), got (%d, %d, %d, %d)",
			pollCalled, getTxCalled, processTxCalled, processCalled)
	}
}

func TestReconcilePendingOne_ProcessTransactionLockBusy(t *testing.T) {
	f, w, bcTx := reconcilePendingFixture(t)

	f.blockchain.pollTransactionStatusFunc = func(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
		return domain.TransactionStatusResult{Status: domain.BlockchainTxStatusFailed, Confirmations: 0, RequiredConfirmations: 20}, nil
	}
	f.blockchain.getTransactionByIDFunc = func(ctx context.Context, id domain.ID) (domain.BlockchainTx, error) {
		return bcTx, nil
	}
	f.blockchain.processTransactionFunc = func(ctx context.Context, params domain.ProcessBlockchainTxParams) error {
		// Simulate the callback handler holding the per-ExternalRequestID
		// processingLocker — the cron should classify this as lock_busy and
		// not attempt Process or escalate the error.
		return svcErrors.New(svcErrors.BlockchainTxInProgressError, appErrors.WithCause(cache.KeyAlreadyLockedErr))
	}
	// No Process call expected — getByBlockchainIDForUpdateFunc not wired,
	// so any call would panic.

	f.ctrl.reconcilePendingOne(context.Background(), *w)
}

// ----------------------------------------------------------------
// Pass 2 — stuck New
// ----------------------------------------------------------------

func reconcileNewFixture(t *testing.T) (*withdrawalFixture, domain.WithdrawalRequest) {
	t.Helper()
	f := newWithdrawalFixture()
	w := domain.WithdrawalRequest{
		ID:        domain.NewIDFromUint64(7),
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
		Status:    domain.WithdrawalStatusNew,
	}
	return f, w
}

func TestReconcileNewOne_StatusChangedBeforeLock(t *testing.T) {
	f, w := reconcileNewFixture(t)
	getCalled := 0
	f.withdrawals.getByIDFunc = func(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
		getCalled++
		// Pre-check sees the withdrawal already moved to Pending — the
		// queue consumer beat us to it. We must not call SendToBlockchain.
		return domain.WithdrawalRequest{ID: id, Status: domain.WithdrawalStatusPending}, nil
	}
	// SendToBlockchain not wired => panic on call. Test passes only when
	// reconcile takes the early-exit path.
	f.ctrl.reconcileNewOne(context.Background(), w)
	if getCalled != 1 {
		t.Fatalf("expected 1 GetByID call, got %d", getCalled)
	}
}

func TestReconcileNewOne_GetByIDError(t *testing.T) {
	f, w := reconcileNewFixture(t)
	f.withdrawals.getByIDFunc = func(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
		return domain.WithdrawalRequest{}, fmt.Errorf("db down")
	}
	f.ctrl.reconcileNewOne(context.Background(), w)
}

// fakeAlwaysLockedLocker rejects every TryLock with cache.KeyAlreadyLockedErr.
// SendToBlockchain wraps that into WithdrawalRequestInProgressError, which
// reconcile classifies as lock_busy.
type fakeAlwaysLockedLocker struct{}

func (fakeAlwaysLockedLocker) TryLock(_ context.Context, _ string) error {
	return cache.KeyAlreadyLockedErr
}
func (fakeAlwaysLockedLocker) Unlock(_ context.Context, _ string) error { return nil }

func TestReconcileNewOne_SendToBlockchainLockerBusy(t *testing.T) {
	f, w := reconcileNewFixture(t)
	f.withdrawals.getByIDFunc = func(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
		return w, nil
	}
	// Replace the processing locker so SendToBlockchain returns
	// WithdrawalRequestInProgressError, which reconcile maps to
	// outcome=lock_busy without escalating to error.
	f.ctrl.processingLocker = fakeAlwaysLockedLocker{}

	// No GetByIDForUpdate wired: SendToBlockchain bails out at the locker
	// before reaching repository calls, so the test passes only on the
	// expected early-exit path.
	f.ctrl.reconcileNewOne(context.Background(), w)
}

// ----------------------------------------------------------------
// Additional Pass 1 — multisig pre-broadcast path
// ----------------------------------------------------------------

// TestReconcilePendingOne_MultisigTxIDCapture_Recovers simulates the full
// multisig recovery path: the withdrawal's blockchain tx initially has no
// TxID (pre-broadcast), PollTransactionStatus (backed by ExternalRequestID
// polling) returns a terminal status WITH a freshly-assigned TxID, and
// reconcile drives ProcessTransaction + Process to completion.
func TestReconcilePendingOne_MultisigTxIDCapture_Recovers(t *testing.T) {
	f, w, _ := reconcilePendingFixture(t)
	// Remove TxID from the stored blockchain tx to simulate pre-broadcast.
	bcIDNoHash := domain.NewIDFromUint64(99)
	w.BlockchainTxID = &bcIDNoHash
	bcTxNoHash := domain.BlockchainTx{
		ID:                bcIDNoHash,
		ExternalRequestID: "ext-multisig",
		TxID:              nil, // pre-broadcast: no hash yet
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeOutbound,
		Amount:            decimal.NewFromInt(1_000_000),
		Status:            domain.BlockchainTxPendingSignatures,
	}

	broadcastedHash := "broadcasted-tx-hash"
	pollCalled := 0
	f.blockchain.pollTransactionStatusFunc = func(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
		pollCalled++
		// Node-service now returns the hash along with terminal status.
		return domain.TransactionStatusResult{
			TxID:                  &broadcastedHash,
			Status:                domain.BlockchainTxStatusConfirmed,
			Confirmations:         20,
			RequiredConfirmations: 20,
		}, nil
	}

	getTxCalled := 0
	f.blockchain.getTransactionByIDFunc = func(ctx context.Context, id domain.ID) (domain.BlockchainTx, error) {
		getTxCalled++
		return bcTxNoHash, nil
	}

	processTxCalled := 0
	f.blockchain.processTransactionFunc = func(ctx context.Context, params domain.ProcessBlockchainTxParams) error {
		processTxCalled++
		// Verify TxID is forwarded so ProcessTransaction can persist the hash.
		if params.TxID == nil || *params.TxID != broadcastedHash {
			t.Fatalf("expected TxID=%q forwarded to ProcessTransaction, got %v", broadcastedHash, params.TxID)
		}
		if params.Status != domain.BlockchainTxStatusConfirmed {
			t.Fatalf("expected status=Confirmed, got %v", params.Status)
		}
		return nil
	}

	// Process() short-circuits at status guard: withdrawal is already Success.
	f.withdrawals.withTxFromCtxFunc = func(ctx context.Context) domain.WithdrawalRequestRepository {
		return f.withdrawals
	}
	processCalled := 0
	f.withdrawals.getByBlockchainIDForUpdateFunc = func(ctx context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
		processCalled++
		return domain.WithdrawalRequest{ID: w.ID, Status: domain.WithdrawalStatusSuccess}, nil
	}

	f.ctrl.reconcilePendingOne(context.Background(), *w)

	if pollCalled != 1 || getTxCalled != 2 || processTxCalled != 1 || processCalled != 1 {
		t.Fatalf("expected (poll=1, getTx=2, processTx=1, process=1), got (%d,%d,%d,%d)",
			pollCalled, getTxCalled, processTxCalled, processCalled)
	}
}

// ----------------------------------------------------------------
// ReconcileStuck-level — both passes, partial errors, batch size
// ----------------------------------------------------------------

// TestReconcileStuck_PendingPassQueryError_NewPassStillRuns verifies that a
// query failure in Pass 1 does not abort Pass 2. Both passes are independent.
func TestReconcileStuck_PendingPassQueryError_NewPassStillRuns(t *testing.T) {
	f, _ := reconcileNewFixture(t)

	// Override pending pass to fail the query.
	pendingQueryCalled, newQueryCalled := 0, 0
	f.withdrawals.findStuckByStatusFunc = func(_ context.Context, status domain.WithdrawalStatus, _ time.Duration, _ int) ([]domain.WithdrawalRequest, error) {
		if status == domain.WithdrawalStatusPending {
			pendingQueryCalled++
			return nil, errors.New("db timeout on pending query")
		}
		newQueryCalled++
		return nil, nil // no stuck New records
	}

	err := f.ctrl.ReconcileStuck(context.Background(), domain.ReconcileStuckParams{
		PendingThreshold: 30 * time.Minute,
		NewThreshold:     3 * time.Hour,
		BatchSize:        50,
	})
	if err != nil {
		t.Fatalf("ReconcileStuck must never return an error, got: %v", err)
	}
	if pendingQueryCalled != 1 {
		t.Fatalf("expected 1 pending query call, got %d", pendingQueryCalled)
	}
	if newQueryCalled != 1 {
		t.Fatalf("expected 1 new query call, got %d", newQueryCalled)
	}
}

// TestReconcileStuck_PartialNewPassErrors_ContinuesAllRecords verifies that
// an error on one New withdrawal in Pass 2 does not abort processing of
// subsequent withdrawals in the same batch.
func TestReconcileStuck_PartialNewPassErrors_ContinuesAllRecords(t *testing.T) {
	f, _ := reconcileNewFixture(t)

	w1 := domain.WithdrawalRequest{ID: domain.NewIDFromUint64(1), Status: domain.WithdrawalStatusNew}
	w2 := domain.WithdrawalRequest{ID: domain.NewIDFromUint64(2), Status: domain.WithdrawalStatusNew}

	getByIDCalls := 0
	f.withdrawals.findStuckByStatusFunc = func(_ context.Context, status domain.WithdrawalStatus, _ time.Duration, _ int) ([]domain.WithdrawalRequest, error) {
		if status == domain.WithdrawalStatusPending {
			return nil, nil
		}
		return []domain.WithdrawalRequest{w1, w2}, nil
	}
	f.withdrawals.getByIDFunc = func(_ context.Context, id domain.ID) (domain.WithdrawalRequest, error) {
		getByIDCalls++
		if id == w1.ID {
			return domain.WithdrawalRequest{}, fmt.Errorf("db error on w1")
		}
		// w2 pre-check passes; locker blocks further progress (no DB mocks wired).
		return domain.WithdrawalRequest{ID: id, Status: domain.WithdrawalStatusNew}, nil
	}
	// w2 hits lock_busy via the processing locker — simplest no-DB path.
	f.ctrl.processingLocker = fakeAlwaysLockedLocker{}

	err := f.ctrl.ReconcileStuck(context.Background(), domain.ReconcileStuckParams{
		PendingThreshold: 30 * time.Minute,
		NewThreshold:     3 * time.Hour,
		BatchSize:        50,
	})
	if err != nil {
		t.Fatalf("ReconcileStuck must never return an error, got: %v", err)
	}
	if getByIDCalls != 2 {
		t.Fatalf("expected 2 GetByID calls (one per record), got %d", getByIDCalls)
	}
}
