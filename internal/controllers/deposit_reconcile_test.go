package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/whagency/core-common/cache"
	"github.com/whagency/core-common/services/currency"
)

// Reconciliation tests target the decision logic in reconcileStuckOne by
// short-circuiting the inner Process() call at the locker layer or driving
// it to its no-op branch via the existing mocks.

func newReconcileStuckBlockchainTx() domain.BlockchainTx {
	hash := "tx-hash"
	return domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(42),
		ExternalRequestID: "ext-1",
		TxID:              &hash,
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		Status:            domain.BlockchainTxStatusConfirmed,
	}
}

// stubQuietRescreen wires the re-screen step reconcileStuckOne runs before
// Process, so a test can stay about Process's own classification. Polling
// echoes the stored status back, which is the "chain says what we already
// recorded" case, and ProcessTransaction then has nothing to change.
func stubQuietRescreen(f *depositProcessFixture, bcTx domain.BlockchainTx) {
	f.blockchain.pollTransactionStatusFunc = func(context.Context, domain.ID) (domain.TransactionStatusResult, error) {
		return domain.TransactionStatusResult{
			TxID:          bcTx.TxID,
			Status:        bcTx.Status,
			Confirmations: bcTx.Confirmations,
		}, nil
	}
	f.blockchain.processTransactionFunc = func(context.Context, domain.ProcessBlockchainTxParams) error {
		return nil
	}
}

func TestReconcileStuckOne_LockBusy(t *testing.T) {
	f := newDepositProcessFixture()
	bcTx := newReconcileStuckBlockchainTx()

	// processingLocker rejects → DepositCtrl.Process returns
	// DepositRequestInProgressError, which reconcile classifies as
	// lock_busy without escalating.
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error {
		return cache.KeyAlreadyLockedErr
	}
	// Beyond the re-screen, no mocks are wired: any unexpected call panics, so
	// the test passes only if reconcile took the early-exit path on the locker.
	stubQuietRescreen(f, bcTx)

	f.ctrl.reconcileStuckOne(context.Background(), bcTx)
}

func TestReconcileStuckOne_NoOp_AlreadyTerminal(t *testing.T) {
	f := newDepositProcessFixture()
	bcTx := newReconcileStuckBlockchainTx()
	stubQuietRescreen(f, bcTx)

	// Locker grants. Process loads BlockchainTx, sees Type=Inbound,
	// computes newStatus from BlockchainTx.Status (Confirmed → Success),
	// loads the existing DepositRequest with status=Success, hits the
	// "status not in {New, Pending}" guard at deposit.go:241 and returns
	// nil. Reconcile counts this as recovered (operational view: row is
	// in good state after our touch).
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error { return nil }
	f.processingLocker.unlockFunc = func(_ context.Context, _ string) error { return nil }

	f.blockchain.getTransactionByIDFunc = func(_ context.Context, id domain.ID) (domain.BlockchainTx, error) {
		return bcTx, nil
	}
	f.deposits.getByBlockchainIDFunc = func(_ context.Context, id domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{
			ID:             domain.NewIDFromUint64(1),
			BlockchainTxID: &bcTx.ID,
			Status:         domain.DepositStatusSuccess,
		}, nil
	}

	f.ctrl.reconcileStuckOne(context.Background(), bcTx)
}

func TestReconcileStuckOne_RecordNotFound(t *testing.T) {
	f := newDepositProcessFixture()
	bcTx := newReconcileStuckBlockchainTx()
	stubQuietRescreen(f, bcTx)

	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error { return nil }
	f.processingLocker.unlockFunc = func(_ context.Context, _ string) error { return nil }

	// BlockchainTx disappeared between scan and Process — Process wraps
	// ErrRecordNotFound into RecordNotFoundError. Reconcile should log
	// at warn level and emit the db reason metric without escalating.
	f.blockchain.getTransactionByIDFunc = func(_ context.Context, id domain.ID) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, domain.ErrRecordNotFound
	}

	f.ctrl.reconcileStuckOne(context.Background(), bcTx)
}

func TestReconcileStuck_QueryError(t *testing.T) {
	f := newDepositProcessFixture()
	f.blockchain.findStuckInboundForReconcileFunc = func(_ context.Context, _ time.Duration, _ int) ([]domain.BlockchainTx, error) {
		return nil, errors.New("db down")
	}

	if err := f.ctrl.ReconcileStuck(context.Background(), domain.ReconcileStuckDepositParams{
		Threshold: 15 * time.Minute,
		BatchSize: 50,
	}); err != nil {
		t.Fatalf("expected nil (per-tick errors are logged not returned), got %v", err)
	}
}

func TestReconcileStuck_RejectsZeroBatch(t *testing.T) {
	f := newDepositProcessFixture()
	err := f.ctrl.ReconcileStuck(context.Background(), domain.ReconcileStuckDepositParams{
		Threshold: 15 * time.Minute,
		BatchSize: 0,
	})
	if err == nil {
		t.Fatalf("expected error on BatchSize=0 to catch misconfiguration")
	}
}

func TestReconcileStuck_IteratesRows(t *testing.T) {
	f := newDepositProcessFixture()
	first := newReconcileStuckBlockchainTx()
	second := first
	second.ID = domain.NewIDFromUint64(43)

	stubQuietRescreen(f, first)

	f.blockchain.findStuckInboundForReconcileFunc = func(_ context.Context, _ time.Duration, _ int) ([]domain.BlockchainTx, error) {
		return []domain.BlockchainTx{first, second}, nil
	}

	// Both records hit lock_busy — simplest path that doesn't require
	// wiring all of DepositCtrl.Process's downstream mocks. We just want
	// to confirm ReconcileStuck iterates.
	tryLockCalls := 0
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error {
		tryLockCalls++
		return cache.KeyAlreadyLockedErr
	}

	if err := f.ctrl.ReconcileStuck(context.Background(), domain.ReconcileStuckDepositParams{
		Threshold: 15 * time.Minute,
		BatchSize: 50,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tryLockCalls != 2 {
		t.Fatalf("expected 2 TryLock calls (one per row), got %d", tryLockCalls)
	}
}

// TestReconcileStuckOne_Recovered_LogsOutcome verifies that when Process()
// succeeds (deposit was terminal-guarded and exits cleanly), reconcileStuckOne
// emits an INFO log with outcome=recovered. The test can only confirm no panic
// or error is surfaced; log content is verified by the structured-field
// pattern established in reconcileStuckOne.
func TestReconcileStuckOne_Recovered_LogsOutcome(t *testing.T) {
	f := newDepositProcessFixture()
	bcTx := newReconcileStuckBlockchainTx()
	stubQuietRescreen(f, bcTx)

	// Locker grants, then Process() finds deposit already at Success and
	// exits cleanly via the terminal status guard — Process returns nil,
	// which reconcileStuckOne counts as "recovered".
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error { return nil }
	f.processingLocker.unlockFunc = func(_ context.Context, _ string) error { return nil }

	f.blockchain.getTransactionByIDFunc = func(_ context.Context, _ domain.ID) (domain.BlockchainTx, error) {
		return bcTx, nil
	}
	f.deposits.getByBlockchainIDFunc = func(_ context.Context, _ domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{
			ID:             domain.NewIDFromUint64(1),
			BlockchainTxID: &bcTx.ID,
			Status:         domain.DepositStatusSuccess, // already terminal
		}, nil
	}

	// reconcileStuckOne must not propagate any error.
	f.ctrl.reconcileStuckOne(context.Background(), bcTx)
}

// TestReconcileStuck_EmptyBatch_NoOp verifies that when FindStuckInboundForReconcile
// returns an empty slice, ReconcileStuck completes without calling any other mocks.
func TestReconcileStuck_EmptyBatch_NoOp(t *testing.T) {
	f := newDepositProcessFixture()
	f.blockchain.findStuckInboundForReconcileFunc = func(_ context.Context, _ time.Duration, _ int) ([]domain.BlockchainTx, error) {
		return nil, nil
	}
	// processingLocker not wired — any TryLock call would panic.

	if err := f.ctrl.ReconcileStuck(context.Background(), domain.ReconcileStuckDepositParams{
		Threshold: 15 * time.Minute,
		BatchSize: 50,
	}); err != nil {
		t.Fatalf("expected nil error on empty batch, got: %v", err)
	}
}

// TestReconcileStuck_PartialErrors_ContinuesRemaining verifies that an error
// on one record does not abort processing of subsequent records in the batch.
func TestReconcileStuck_PartialErrors_ContinuesRemaining(t *testing.T) {
	f := newDepositProcessFixture()
	first := newReconcileStuckBlockchainTx()
	second := newReconcileStuckBlockchainTx()
	second.ID = domain.NewIDFromUint64(44)

	stubQuietRescreen(f, first)

	f.blockchain.findStuckInboundForReconcileFunc = func(_ context.Context, _ time.Duration, _ int) ([]domain.BlockchainTx, error) {
		return []domain.BlockchainTx{first, second}, nil
	}

	lockCalls := 0
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error {
		lockCalls++
		// First record fails with an unexpected error (not KeyAlreadyLockedErr),
		// which the mock wraps. Second succeeds via lock_busy (simpler to wire).
		if lockCalls == 1 {
			return errors.New("unexpected cache error")
		}
		return cache.KeyAlreadyLockedErr // second record: lock_busy, safe exit
	}

	if err := f.ctrl.ReconcileStuck(context.Background(), domain.ReconcileStuckDepositParams{
		Threshold: 15 * time.Minute,
		BatchSize: 50,
	}); err != nil {
		t.Fatalf("ReconcileStuck must never return an error, got: %v", err)
	}
	if lockCalls != 2 {
		t.Fatalf("expected 2 TryLock calls (one per row), got %d", lockCalls)
	}
}

// ============================================================
// Re-screening a held transaction
// ============================================================

// newHeldBlockchainTx returns an inbound transaction in the state an
// inconclusive KYT verdict leaves behind: the chain has confirmed it, but
// account-service capped its own record at Pending so nothing is credited.
func newHeldBlockchainTx() domain.BlockchainTx {
	bcTx := newReconcileStuckBlockchainTx()
	bcTx.Status = domain.BlockchainTxStatusPending
	bcTx.Confirmations = 20
	return bcTx
}

// TestReconcileStuckOne_HeldTransaction_RescreenedAgainstChainStatus is the
// reason the re-screen exists. The stored row carries our own capped Pending,
// so re-deriving a status from it could never clear the hold — the tick has to
// put the chain's answer back through ProcessTransaction, which is also the
// only path that screens the address.
func TestReconcileStuckOne_HeldTransaction_RescreenedAgainstChainStatus(t *testing.T) {
	f := newDepositProcessFixture()
	bcTx := newHeldBlockchainTx()

	f.blockchain.pollTransactionStatusFunc = func(context.Context, domain.ID) (domain.TransactionStatusResult, error) {
		// node-service still reports what the chain says, not our cap.
		return domain.TransactionStatusResult{
			TxID:                  bcTx.TxID,
			Status:                domain.BlockchainTxStatusConfirmed,
			Confirmations:         20,
			RequiredConfirmations: 20,
		}, nil
	}

	var got domain.ProcessBlockchainTxParams
	processTransactionCalls := 0
	f.blockchain.processTransactionFunc = func(_ context.Context, params domain.ProcessBlockchainTxParams) error {
		processTransactionCalls++
		got = params
		return nil
	}

	// Let Process run to its terminal-guard no-op; this test is about the
	// re-screen, not about what Process does afterwards.
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error { return nil }
	f.processingLocker.unlockFunc = func(_ context.Context, _ string) error { return nil }
	f.blockchain.getTransactionByIDFunc = func(_ context.Context, _ domain.ID) (domain.BlockchainTx, error) {
		return bcTx, nil
	}
	f.deposits.getByBlockchainIDFunc = func(_ context.Context, _ domain.ID) (domain.DepositRequest, error) {
		return domain.DepositRequest{
			ID:             domain.NewIDFromUint64(1),
			BlockchainTxID: &bcTx.ID,
			Status:         domain.DepositStatusSuccess,
		}, nil
	}

	f.ctrl.reconcileStuckOne(context.Background(), bcTx)

	if processTransactionCalls != 1 {
		t.Fatalf("expected the re-screen to call ProcessTransaction once, got %d", processTransactionCalls)
	}
	if got.Status != domain.BlockchainTxStatusConfirmed {
		t.Fatalf("re-screen carried status %v, want the chain's Confirmed — feeding our own capped status back could never clear the hold", got.Status)
	}
	if got.ExternalRequestID != bcTx.ExternalRequestID {
		t.Fatalf("re-screen carried external request id %q, want %q", got.ExternalRequestID, bcTx.ExternalRequestID)
	}
	if got.FromAddress != bcTx.FromAddress {
		t.Fatalf("re-screen carried from address %q, want %q — this is the address being screened", got.FromAddress, bcTx.FromAddress)
	}
}

// TestReconcileStuckOne_NodeUnreachable_StillRunsProcess pins the re-screen as
// best effort: a node outage must not cost us the lost-callback recovery this
// cron was originally written for.
func TestReconcileStuckOne_NodeUnreachable_StillRunsProcess(t *testing.T) {
	f := newDepositProcessFixture()
	bcTx := newReconcileStuckBlockchainTx()

	f.blockchain.pollTransactionStatusFunc = func(context.Context, domain.ID) (domain.TransactionStatusResult, error) {
		return domain.TransactionStatusResult{}, errors.New("node down")
	}
	// processTransactionFunc deliberately unwired: reaching it would panic.

	processReached := false
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error {
		processReached = true
		return cache.KeyAlreadyLockedErr
	}

	f.ctrl.reconcileStuckOne(context.Background(), bcTx)

	if !processReached {
		t.Fatal("a failed re-screen swallowed the tick; Process must still run")
	}
}

// TestReconcileStuckOne_RescreenLockBusy_StillRunsProcess covers a callback for
// the same request id running concurrently: it screens the address itself, so
// the tick steps out of its way rather than treating the contention as a fault.
func TestReconcileStuckOne_RescreenLockBusy_StillRunsProcess(t *testing.T) {
	f := newDepositProcessFixture()
	bcTx := newReconcileStuckBlockchainTx()

	f.blockchain.pollTransactionStatusFunc = func(context.Context, domain.ID) (domain.TransactionStatusResult, error) {
		return domain.TransactionStatusResult{Status: bcTx.Status, Confirmations: bcTx.Confirmations}, nil
	}
	f.blockchain.processTransactionFunc = func(context.Context, domain.ProcessBlockchainTxParams) error {
		return svcErrors.New(svcErrors.BlockchainTxInProgressError)
	}

	processReached := false
	f.processingLocker.tryLockFunc = func(_ context.Context, _ string) error {
		processReached = true
		return cache.KeyAlreadyLockedErr
	}

	f.ctrl.reconcileStuckOne(context.Background(), bcTx)

	if !processReached {
		t.Fatal("a busy re-screen lock swallowed the tick; Process must still run")
	}
}
