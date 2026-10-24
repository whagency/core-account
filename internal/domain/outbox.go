package domain

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/outbox"
)

// OutboxRepository is the account-service binding for the shared outbox.Repository.
// Each business transaction uses WithTxFromContext to join the active DB transaction
// before calling Create, ensuring the outbox row and the business state change are
// committed atomically.
type OutboxRepository interface {
	outbox.Repository
}

// Outbox event_type constants for account-service.
const (
	OutboxEventCounterUpdate    = "restriction.counter.update"
	OutboxEventNotificationPush = "notification.push"
)

// Outbox aggregate_type constants for account-service.
const (
	OutboxAggregateDeposit    = "deposit_request"
	OutboxAggregateWithdrawal = "withdrawal_request"
	OutboxAggregateTransfer   = "transfer_request"
)

// CounterUpdatePayload is the JSON payload stored in outbox_messages for
// event_type = "restriction.counter.update".
//
// The outbox worker deserialises this and calls restriction.CountersUpdate via gRPC.
// The IdempotencyKey matches the key that restriction-service uses in its
// limit_counter_idempotency_keys table — guaranteeing exactly-once counter application
// even when the worker retries.
type CounterUpdatePayload struct {
	// IdempotencyKey is passed directly to restriction-service CountersUpdate.
	// Format: "account-{operation}-{id}[{-reverse}]"
	// Examples: "account-deposit-42", "account-withdrawal-42", "account-withdrawal-42-reverse"
	IdempotencyKey string `json:"idempotencyKey"`

	UserID        uint64          `json:"userId"`
	OperationType uint8           `json:"operationType"` // restriction.LimitOperationType value
	AssetCode     string          `json:"assetCode"`
	Amount        decimal.Decimal `json:"amount"`
	OccurredAt    time.Time       `json:"occurredAt"`
	IsReverse     bool            `json:"isReverse"`
}

// OutboxCounterOperation identifies the flow a counter update came from.
//
// It is baked into the idempotency key that restriction-service stores in
// limit_counter_idempotency_keys, so these values are a cross-service contract,
// not a free-form label. Changing one silently un-fences every counter update
// already recorded under the old spelling.
type OutboxCounterOperation string

const (
	OutboxCounterOperationDeposit    OutboxCounterOperation = "deposit"
	OutboxCounterOperationTransfer   OutboxCounterOperation = "transfer"
	OutboxCounterOperationWithdrawal OutboxCounterOperation = "withdrawal"
)

// Push dedupe key suffixes. They discriminate the events a single aggregate can
// emit: a deposit that succeeds and one that goes to manual review are different
// notifications about the same request, and a transfer notifies both sides.
const (
	OutboxPushSuffixSuccess      = "success"
	OutboxPushSuffixManualReview = "manual_review"
	OutboxPushSuffixOutgoing     = "outgoing"
	OutboxPushSuffixIncoming     = "incoming"
)

// CounterOutboxParams describes a restriction counter update to enqueue.
//
// The idempotency key is derived by NewCounterOutboxParams rather than supplied,
// because it has to appear in two places at once — the row's dedupe key and the
// payload restriction-service fences on. Letting callers pass it invited those two
// to drift apart, which would fence nothing while looking correct.
type CounterOutboxParams struct {
	AggregateType string
	AggregateID   ID
	Operation     OutboxCounterOperation

	UserID        uint64
	OperationType uint8
	AssetCode     string
	Amount        decimal.Decimal
	OccurredAt    time.Time

	// IsReverse marks a rollback. It is part of the key: reversing a withdrawal is
	// a distinct event from making it, and the two must not collapse into one.
	IsReverse bool
}

// NewCounterOutboxParams builds the outbox row for a restriction counter update.
func NewCounterOutboxParams(p CounterOutboxParams) outbox.CreateParams {
	idempotencyKey := counterIdempotencyKey(p.Operation, p.AggregateID, p.IsReverse)

	return outbox.CreateParams{
		AggregateType: p.AggregateType,
		AggregateID:   p.AggregateID.String(),
		EventType:     OutboxEventCounterUpdate,
		DedupeKey:     "counter:" + idempotencyKey,
		Payload: CounterUpdatePayload{
			IdempotencyKey: idempotencyKey,
			UserID:         p.UserID,
			OperationType:  p.OperationType,
			AssetCode:      p.AssetCode,
			Amount:         p.Amount,
			OccurredAt:     p.OccurredAt,
			IsReverse:      p.IsReverse,
		},
	}
}

// counterIdempotencyKey builds the key restriction-service fences on.
func counterIdempotencyKey(operation OutboxCounterOperation, id ID, isReverse bool) string {
	key := fmt.Sprintf("account-%s-%d", operation, id)
	if isReverse {
		key += "-reverse"
	}
	return key
}

// NewNotificationPushOutboxParams builds the outbox row for a push notification.
//
// operation and suffix together make the key unique per event rather than per
// aggregate: one deposit request can notify on success and on manual review, and
// one transfer notifies both parties.
func NewNotificationPushOutboxParams(
	aggregateType string,
	aggregateID ID,
	operation OutboxCounterOperation,
	suffix string,
	msg PushMessage,
) outbox.CreateParams {
	return outbox.CreateParams{
		AggregateType: aggregateType,
		AggregateID:   aggregateID.String(),
		EventType:     OutboxEventNotificationPush,
		DedupeKey:     fmt.Sprintf("push:%s:%s:%s", operation, aggregateID.String(), suffix),
		Payload:       msg,
	}
}
