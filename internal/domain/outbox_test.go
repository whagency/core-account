package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// TestNewCounterOutboxParams_KeysAreAContract pins the counter idempotency key
// format.
//
// This string is not an implementation detail. restriction-service stores it in
// limit_counter_idempotency_keys and fences on it, so changing the format
// un-fences every counter update already recorded under the old spelling — and
// the symptom is double-counted limits, not a failure.
func TestNewCounterOutboxParams_KeysAreAContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation OutboxCounterOperation
		isReverse bool
		wantKey   string
	}{
		{name: "deposit", operation: OutboxCounterOperationDeposit, wantKey: "account-deposit-42"},
		{name: "transfer", operation: OutboxCounterOperationTransfer, wantKey: "account-transfer-42"},
		{name: "withdrawal", operation: OutboxCounterOperationWithdrawal, wantKey: "account-withdrawal-42"},
		{
			// A reversal is a distinct event from the operation it reverses. Without
			// the suffix the two collapse into one row and the rollback is dropped.
			name:      "withdrawal reversal is a distinct key",
			operation: OutboxCounterOperationWithdrawal,
			isReverse: true,
			wantKey:   "account-withdrawal-42-reverse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			params := NewCounterOutboxParams(CounterOutboxParams{
				AggregateType: OutboxAggregateDeposit,
				AggregateID:   NewIDFromUint64(42),
				Operation:     tt.operation,
				IsReverse:     tt.isReverse,
				UserID:        7,
				AssetCode:     "USDT",
				Amount:        decimal.NewFromInt(100),
				OccurredAt:    time.Now().UTC(),
			})

			if params.DedupeKey != "counter:"+tt.wantKey {
				t.Fatalf("expected dedupe key %q, got %q", "counter:"+tt.wantKey, params.DedupeKey)
			}

			// The payload key and the dedupe key must agree: the first is what
			// restriction-service fences on, the second is what stops us enqueueing
			// twice. Letting them drift fences nothing while looking correct, which
			// is why the builder derives both rather than taking them.
			raw, err := json.Marshal(params.Payload)
			if err != nil {
				t.Fatalf("failed to marshal payload: %v", err)
			}
			var payload CounterUpdatePayload
			if errUnmarshal := json.Unmarshal(raw, &payload); errUnmarshal != nil {
				t.Fatalf("failed to decode payload: %v", errUnmarshal)
			}
			if payload.IdempotencyKey != tt.wantKey {
				t.Fatalf("expected payload key %q, got %q", tt.wantKey, payload.IdempotencyKey)
			}
			if payload.IsReverse != tt.isReverse {
				t.Fatalf("expected isReverse=%t in payload", tt.isReverse)
			}
		})
	}
}

// TestNewNotificationPushOutboxParams_KeysAreAContract pins the push dedupe key
// format.
//
// The operation and suffix both matter: one deposit request notifies on success
// and on manual review, and one transfer notifies both parties. Keying on the
// aggregate alone would deliver only the first of each pair.
func TestNewNotificationPushOutboxParams_KeysAreAContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		aggregate string
		operation OutboxCounterOperation
		suffix    string
		want      string
	}{
		{"deposit success", OutboxAggregateDeposit, OutboxCounterOperationDeposit, OutboxPushSuffixSuccess, "push:deposit:42:success"},
		{"deposit manual review", OutboxAggregateDeposit, OutboxCounterOperationDeposit, OutboxPushSuffixManualReview, "push:deposit:42:manual_review"},
		{"transfer outgoing", OutboxAggregateTransfer, OutboxCounterOperationTransfer, OutboxPushSuffixOutgoing, "push:transfer:42:outgoing"},
		{"transfer incoming", OutboxAggregateTransfer, OutboxCounterOperationTransfer, OutboxPushSuffixIncoming, "push:transfer:42:incoming"},
		{"withdrawal success", OutboxAggregateWithdrawal, OutboxCounterOperationWithdrawal, OutboxPushSuffixSuccess, "push:withdrawal:42:success"},
	}

	seen := make(map[string]string, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := NewNotificationPushOutboxParams(
				tt.aggregate, NewIDFromUint64(42), tt.operation, tt.suffix, PushMessage{},
			)
			if params.DedupeKey != tt.want {
				t.Fatalf("expected dedupe key %q, got %q", tt.want, params.DedupeKey)
			}
			if params.EventType != OutboxEventNotificationPush {
				t.Fatalf("unexpected event type %q", params.EventType)
			}
		})

		// Every push a single aggregate can emit must land on its own key.
		if previous, ok := seen[tt.want]; ok {
			t.Fatalf("key %q is shared by %q and %q", tt.want, previous, tt.name)
		}
		seen[tt.want] = tt.name
	}
}
