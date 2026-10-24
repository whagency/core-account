package handlers

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	"github.com/whagency/core-common/services/currency"
	commonUserQueue "github.com/whagency/core-common/services/user/queue"
)

func TestMain(m *testing.M) {
	// Handlers call logger.LogCtx(ctx).Debugf(...) at the top of Handle —
	// that panics with "global logger not initialized" unless we boot the
	// logger here. Mirror the pattern used by deposit_process_test.go.
	_ = logger.Init(logger.Config{
		AppName:      "account-service-tests",
		Environment:  "test",
		Version:      "test",
		Level:        "error",
		StdoutFormat: logger.StdoutFormatConsole,
	})
	os.Exit(m.Run())
}

// Tests in this file cover the retry-classification axis only — they assert
// for each handler that:
//
//   - payload-level errors (malformed JSON, missing required fields, wrong
//     message type) are marked as queue.NonRetryableErr;
//   - controller-level deterministic errors (RecordNotFound, InvalidRequest
//     where applicable) are also marked as queue.NonRetryableErr;
//   - controller-level transient errors (DatabaseError, InternalServerError,
//     ExternalServiceError) flow through unchanged so the consumer's retry
//     budget bounds the loop.
//
// Idempotency is tested separately in the controllers package; here we only
// verify that the handler maps controller errors to the right retry verdict.

// --- helpers ----------------------------------------------------------------

func mustNonRetryable(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected non-nil error", label)
	}
	if !errors.Is(err, queue.NonRetryableErr) {
		t.Fatalf("%s: expected NonRetryableErr, got %v", label, err)
	}
}

func mustRetryable(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected non-nil error", label)
	}
	if errors.Is(err, queue.NonRetryableErr) {
		t.Fatalf("%s: expected retryable error, got NonRetryable: %v", label, err)
	}
}

func mustNil(t *testing.T, label string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: expected nil, got %v", label, err)
	}
}

// --- AccountController stub -------------------------------------------------

type stubAccountCtrl struct {
	createDefaultFunc func(context.Context, domain.ID) error
}

func (s stubAccountCtrl) CreateDefaultAccountsForUser(ctx context.Context, id domain.ID) error {
	return s.createDefaultFunc(ctx, id)
}
func (stubAccountCtrl) CreateDefaultSystemAccounts(context.Context) error {
	panic("unexpected: CreateDefaultSystemAccounts")
}
func (stubAccountCtrl) CreateAccount(context.Context, *domain.ID, currency.AssetCode, domain.AccountType) error {
	panic("unexpected: CreateAccount")
}
func (stubAccountCtrl) GetBalancesByOwnerID(context.Context, *domain.ID) (domain.GetBalancesResult, error) {
	panic("unexpected: GetBalancesByOwnerID")
}
func (stubAccountCtrl) GetPnlByOwnerID(context.Context, *domain.ID) (domain.GetPnlResult, error) {
	panic("unexpected: GetPnlByOwnerID")
}
func (stubAccountCtrl) UpdatePnlStat(context.Context) error {
	panic("unexpected: UpdatePnlStat")
}
func (stubAccountCtrl) GetBalance(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
	panic("unexpected: GetBalance")
}

// --- WithdrawalController stub ----------------------------------------------

type stubWithdrawalCtrl struct {
	processFunc          func(context.Context, domain.ProcessWithdrawalParams) error
	sendToBlockchainFunc func(context.Context, domain.SendWithdrawalToBlockchain) error
}

func (stubWithdrawalCtrl) Init(context.Context, domain.InitWithdrawalParams) error {
	panic("unexpected: Init")
}
func (s stubWithdrawalCtrl) Process(ctx context.Context, p domain.ProcessWithdrawalParams) error {
	return s.processFunc(ctx, p)
}
func (s stubWithdrawalCtrl) SendToBlockchain(ctx context.Context, p domain.SendWithdrawalToBlockchain) error {
	return s.sendToBlockchainFunc(ctx, p)
}
func (stubWithdrawalCtrl) Reject(context.Context, domain.ID) error {
	panic("unexpected: Reject")
}
func (stubWithdrawalCtrl) ReconcileStuck(context.Context, domain.ReconcileStuckParams) error {
	panic("unexpected: ReconcileStuck")
}

// --- DepositController stub -------------------------------------------------

type stubDepositCtrl struct {
	processFunc func(context.Context, domain.ProcessDepositParams) error
}

func (stubDepositCtrl) Init(context.Context, domain.InitDepositParams) (domain.InitDepositResult, error) {
	panic("unexpected: Init")
}
func (s stubDepositCtrl) Process(ctx context.Context, p domain.ProcessDepositParams) error {
	return s.processFunc(ctx, p)
}
func (stubDepositCtrl) Approve(context.Context, domain.ID) error {
	panic("unexpected: Approve")
}
func (stubDepositCtrl) AutomaticManualApprove(context.Context, domain.ID) error {
	panic("unexpected: AutomaticManualApprove")
}
func (stubDepositCtrl) ReconcileStuck(context.Context, domain.ReconcileStuckDepositParams) error {
	panic("unexpected: ReconcileStuck")
}

// --- TransactionController stub ---------------------------------------------

type stubTransactionCtrl struct {
	rollbackFunc func(context.Context, domain.ID) error
}

func (stubTransactionCtrl) MakeTransaction(context.Context, domain.MakeTxParams) (domain.MakeTxResult, error) {
	panic("unexpected: MakeTransaction")
}
func (s stubTransactionCtrl) RollbackTransaction(ctx context.Context, id domain.ID) error {
	return s.rollbackFunc(ctx, id)
}
func (stubTransactionCtrl) FindJournalByIdempotencyKey(context.Context, domain.FindJournalParams) (domain.FindJournalResult, error) {
	panic("unexpected: FindJournalByIdempotencyKey")
}

// ============================================================================
// 1. UserCreatedLifeCycleHandler
// ============================================================================

func TestUserCreatedLifeCycleHandler_Classification(t *testing.T) {
	validPayload := func() []byte {
		// Match commonUserQueue.UserCreatedEvent shape — userId + occurredAt
		// are required by the handler. Other fields ignored at this layer.
		return []byte(`{"userId":42,"occurredAt":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`)
	}

	t.Run("malformed JSON → NonRetryable", func(t *testing.T) {
		h := NewUserCreatedLifeCycleHandler(stubAccountCtrl{
			createDefaultFunc: func(context.Context, domain.ID) error {
				t.Fatal("controller must not be invoked on malformed payload")
				return nil
			},
		})
		err := h.Handle(context.Background(), commonUserQueue.MessageTypeUserCreated, []byte(`{not json`))
		mustNonRetryable(t, "malformed", err)
	})

	t.Run("missing userId → NonRetryable", func(t *testing.T) {
		h := NewUserCreatedLifeCycleHandler(stubAccountCtrl{
			createDefaultFunc: func(context.Context, domain.ID) error {
				t.Fatal("controller must not be invoked when userId missing")
				return nil
			},
		})
		err := h.Handle(context.Background(), commonUserQueue.MessageTypeUserCreated,
			[]byte(`{"userId":0,"occurredAt":"`+time.Now().UTC().Format(time.RFC3339Nano)+`"}`))
		mustNonRetryable(t, "missing userId", err)
	})

	t.Run("missing occurredAt → NonRetryable", func(t *testing.T) {
		h := NewUserCreatedLifeCycleHandler(stubAccountCtrl{
			createDefaultFunc: func(context.Context, domain.ID) error {
				t.Fatal("controller must not be invoked when occurredAt missing")
				return nil
			},
		})
		err := h.Handle(context.Background(), commonUserQueue.MessageTypeUserCreated,
			[]byte(`{"userId":42}`))
		mustNonRetryable(t, "missing occurredAt", err)
	})

	t.Run("unexpected message type → NonRetryable", func(t *testing.T) {
		h := NewUserCreatedLifeCycleHandler(stubAccountCtrl{})
		err := h.Handle(context.Background(), queue.MessageType("user.unknown.event"), validPayload())
		mustNonRetryable(t, "wrong type", err)
	})

	t.Run("controller DatabaseError → retryable", func(t *testing.T) {
		// Documented contract: user_lifecycle handler does not classify
		// controller errors. CreateDefaultAccountsForUser is idempotent at
		// the repo level (CreateBatch uses ON CONFLICT DO NOTHING), so any
		// DB error here is genuinely transient.
		h := NewUserCreatedLifeCycleHandler(stubAccountCtrl{
			createDefaultFunc: func(context.Context, domain.ID) error {
				return svcErrors.New(appErrors.DatabaseError,
					appErrors.WithCause(errors.New("connection lost")))
			},
		})
		err := h.Handle(context.Background(), commonUserQueue.MessageTypeUserCreated, validPayload())
		mustRetryable(t, "db error", err)
	})

	t.Run("controller success → nil", func(t *testing.T) {
		h := NewUserCreatedLifeCycleHandler(stubAccountCtrl{
			createDefaultFunc: func(context.Context, domain.ID) error { return nil },
		})
		err := h.Handle(context.Background(), commonUserQueue.MessageTypeUserCreated, validPayload())
		mustNil(t, "happy path", err)
	})
}

// ============================================================================
// 2. WithdrawalRequestLifecycleHandler
// ============================================================================

func TestWithdrawalRequestLifecycleHandler_Classification(t *testing.T) {
	validPayload := []byte(`{"id":7}`)

	t.Run("malformed JSON → NonRetryable", func(t *testing.T) {
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				t.Fatal("controller must not be invoked")
				return nil
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, []byte(`{not json`))
		mustNonRetryable(t, "malformed", err)
	})

	t.Run("missing id → NonRetryable", func(t *testing.T) {
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, []byte(`{"id":0}`))
		mustNonRetryable(t, "missing id", err)
	})

	t.Run("unexpected message type → NonRetryable", func(t *testing.T) {
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), queue.MessageType("withdrawal.unknown"), validPayload)
		mustNonRetryable(t, "wrong type", err)
	})

	t.Run("RecordNotFoundError → NonRetryable", func(t *testing.T) {
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				return svcErrors.New(appErrors.RecordNotFoundError,
					appErrors.WithCause(domain.ErrRecordNotFound))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, validPayload)
		mustNonRetryable(t, "RecordNotFound", err)
	})

	t.Run("InvalidRequestError → NonRetryable", func(t *testing.T) {
		// Wrong-status re-delivery is deterministic: the request is no longer
		// New, so retrying the same message cannot make it sendable again.
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				return svcErrors.New(appErrors.InvalidRequestError,
					appErrors.WithCause(errors.New("invalid status")))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, validPayload)
		mustNonRetryable(t, "InvalidRequest", err)
	})

	t.Run("WithdrawalRequestInProgressError → retryable", func(t *testing.T) {
		// Locker contention is transient: another consumer/operator is
		// processing this withdrawal and will release the lock shortly.
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				return svcErrors.New(svcErrors.WithdrawalRequestInProgressError,
					appErrors.WithCause(errors.New("processing already in progress")))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, validPayload)
		mustRetryable(t, "withdrawal in progress", err)
	})

	t.Run("ExternalServiceError → retryable", func(t *testing.T) {
		// KYT or node-service transient — must retry.
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				return svcErrors.New(appErrors.ExternalServiceError,
					appErrors.WithCause(errors.New("node down")))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, validPayload)
		mustRetryable(t, "external service", err)
	})

	t.Run("DatabaseError → retryable", func(t *testing.T) {
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				return svcErrors.New(appErrors.DatabaseError,
					appErrors.WithCause(errors.New("conn lost")))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, validPayload)
		mustRetryable(t, "db error", err)
	})

	t.Run("KYTVerdictUnavailableError → retryable", func(t *testing.T) {
		// The provider owes us a verdict we did not get. Retrying is what makes
		// the hold self-healing: discarding the message would leave the
		// withdrawal at New with nothing driving it but the reconcile cron.
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				return svcErrors.New(svcErrors.KYTVerdictUnavailableError,
					appErrors.WithCause(errors.New("no usable KYT verdict")))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, validPayload)
		mustRetryable(t, "kyt verdict unavailable", err)
	})

	t.Run("controller success → nil", func(t *testing.T) {
		h := NewWithdrawalRequestLifecycleHandler(stubWithdrawalCtrl{
			sendToBlockchainFunc: func(context.Context, domain.SendWithdrawalToBlockchain) error {
				return nil
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeWithdrawalRequestCreated, validPayload)
		mustNil(t, "happy path", err)
	})
}

// ============================================================================
// 3. BlockchainTransactionLifecycleHandler
// ============================================================================

func TestBlockchainTransactionLifecycleHandler_Classification(t *testing.T) {
	inboundPayload := []byte(`{"id":11,"type":1}`)
	outboundPayload := []byte(`{"id":12,"type":2}`)

	t.Run("malformed JSON → NonRetryable", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, []byte(`{not json`))
		mustNonRetryable(t, "malformed", err)
	})

	t.Run("missing id → NonRetryable", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, []byte(`{"id":0,"type":1}`))
		mustNonRetryable(t, "missing id", err)
	})

	t.Run("unsupported tx type → NonRetryable", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, []byte(`{"id":11,"type":99}`))
		mustNonRetryable(t, "bad type", err)
	})

	t.Run("unexpected message type → NonRetryable", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), queue.MessageType("blockchain.unknown"), inboundPayload)
		mustNonRetryable(t, "wrong type", err)
	})

	t.Run("inbound RecordNotFound → NonRetryable", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{
			processFunc: func(context.Context, domain.ProcessDepositParams) error {
				return svcErrors.New(appErrors.RecordNotFoundError,
					appErrors.WithCause(domain.ErrRecordNotFound))
			},
		}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, inboundPayload)
		mustNonRetryable(t, "deposit RecordNotFound", err)
	})

	t.Run("outbound RecordNotFound → NonRetryable", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{}, stubWithdrawalCtrl{
			processFunc: func(context.Context, domain.ProcessWithdrawalParams) error {
				return svcErrors.New(appErrors.RecordNotFoundError,
					appErrors.WithCause(domain.ErrRecordNotFound))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, outboundPayload)
		mustNonRetryable(t, "withdrawal RecordNotFound", err)
	})

	t.Run("inbound DepositRequestInProgressError → retryable", func(t *testing.T) {
		// Locker contention is transient; another worker holds the per-tx
		// lock and will release it, so retrying after backoff succeeds.
		// The handler does NOT classify this as permanent — and that is
		// the correct behaviour the audit confirmed.
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{
			processFunc: func(context.Context, domain.ProcessDepositParams) error {
				return svcErrors.New(svcErrors.DepositRequestInProgressError,
					appErrors.WithCause(errors.New("processing already in progress")))
			},
		}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, inboundPayload)
		mustRetryable(t, "in progress", err)
	})

	t.Run("inbound DatabaseError → retryable", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{
			processFunc: func(context.Context, domain.ProcessDepositParams) error {
				return svcErrors.New(appErrors.DatabaseError,
					appErrors.WithCause(errors.New("conn lost")))
			},
		}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, inboundPayload)
		mustRetryable(t, "db error", err)
	})

	t.Run("inbound success → nil", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{
			processFunc: func(context.Context, domain.ProcessDepositParams) error { return nil },
		}, stubWithdrawalCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, inboundPayload)
		mustNil(t, "deposit happy", err)
	})

	t.Run("outbound success → nil", func(t *testing.T) {
		h := NewBlockchainTransactionLifecycleHandler(stubDepositCtrl{}, stubWithdrawalCtrl{
			processFunc: func(context.Context, domain.ProcessWithdrawalParams) error { return nil },
		})
		err := h.Handle(context.Background(), domain.MessageTypeBlockchainTransactionUpdated, outboundPayload)
		mustNil(t, "withdrawal happy", err)
	})
}

// ============================================================================
// 4. TransactionRollbackHandler
// ============================================================================

func TestTransactionRollbackHandler_Classification(t *testing.T) {
	validPayload := []byte(`{"id":21}`)

	t.Run("malformed JSON → NonRetryable", func(t *testing.T) {
		h := NewTransactionRollbackHandler(stubTransactionCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeTransactionRollbackRequested, []byte(`{not json`))
		mustNonRetryable(t, "malformed", err)
	})

	t.Run("missing id → NonRetryable", func(t *testing.T) {
		h := NewTransactionRollbackHandler(stubTransactionCtrl{})
		err := h.Handle(context.Background(), domain.MessageTypeTransactionRollbackRequested, []byte(`{"id":0}`))
		mustNonRetryable(t, "missing id", err)
	})

	t.Run("unexpected message type → NonRetryable", func(t *testing.T) {
		h := NewTransactionRollbackHandler(stubTransactionCtrl{})
		err := h.Handle(context.Background(), queue.MessageType("transaction.unknown"), validPayload)
		mustNonRetryable(t, "wrong type", err)
	})

	t.Run("RecordNotFoundError → NonRetryable", func(t *testing.T) {
		h := NewTransactionRollbackHandler(stubTransactionCtrl{
			rollbackFunc: func(context.Context, domain.ID) error {
				return svcErrors.New(appErrors.RecordNotFoundError,
					appErrors.WithCause(domain.ErrRecordNotFound))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeTransactionRollbackRequested, validPayload)
		mustNonRetryable(t, "RecordNotFound", err)
	})

	t.Run("InvalidRequestError (no postings) → NonRetryable", func(t *testing.T) {
		// RollbackTransaction returns InvalidRequestError only for "no
		// postings found for journal" — a data-integrity case. Retry
		// gives the same result, so permanent classification is correct.
		h := NewTransactionRollbackHandler(stubTransactionCtrl{
			rollbackFunc: func(context.Context, domain.ID) error {
				return svcErrors.New(appErrors.InvalidRequestError,
					appErrors.WithCause(errors.New("no postings")))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeTransactionRollbackRequested, validPayload)
		mustNonRetryable(t, "InvalidRequest", err)
	})

	t.Run("DatabaseError → retryable", func(t *testing.T) {
		h := NewTransactionRollbackHandler(stubTransactionCtrl{
			rollbackFunc: func(context.Context, domain.ID) error {
				return svcErrors.New(appErrors.DatabaseError,
					appErrors.WithCause(errors.New("conn lost")))
			},
		})
		err := h.Handle(context.Background(), domain.MessageTypeTransactionRollbackRequested, validPayload)
		mustRetryable(t, "db error", err)
	})

	t.Run("controller success → nil (idempotent on already-reversed)", func(t *testing.T) {
		// RollbackTransaction's status guard returns nil when the journal
		// is already JournalStatusReversed — re-delivery is a benign
		// no-op. The handler must propagate that nil unchanged.
		h := NewTransactionRollbackHandler(stubTransactionCtrl{
			rollbackFunc: func(context.Context, domain.ID) error { return nil },
		})
		err := h.Handle(context.Background(), domain.MessageTypeTransactionRollbackRequested, validPayload)
		mustNil(t, "happy path", err)
	})
}
