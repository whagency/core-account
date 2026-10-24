// Package outbox contains the transport adapters that deliver account-service
// outbox messages to their destinations.
//
// These are transport, not use-case logic: each handler deserialises a payload
// and delegates to the owning client. Business decisions about *what* to emit
// live in the controllers that write the outbox rows.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"account-service/internal/domain"

	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/services/restriction"
	"github.com/whagency/core-common/tools"
)

// NotificationPushHandler delivers outbox messages of event_type "notification.push"
// by forwarding them to the push exchange via EventController.PublishNotificationPush.
type NotificationPushHandler struct {
	eventCtrl domain.EventController
}

// NewNotificationPushHandler creates a NotificationPushHandler.
func NewNotificationPushHandler(eventCtrl domain.EventController) *NotificationPushHandler {
	return &NotificationPushHandler{eventCtrl: eventCtrl}
}

// EventType returns the event_type string this handler processes.
func (h *NotificationPushHandler) EventType() string {
	return domain.OutboxEventNotificationPush
}

// Handle deserialises the PushMessage payload and forwards it to the notification push exchange.
// Payload unmarshal errors are non-retryable; publish errors are retryable.
//
// The outbox row's dedupe_key travels with the payload so notification-service
// can fence duplicates: delivery is at-least-once, and a publish that succeeded
// before a failed status update would otherwise reach the user twice.
func (h *NotificationPushHandler) Handle(ctx context.Context, msg outbox.Message) error {
	var p domain.PushMessage
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return outbox.MarkNonRetryable(fmt.Errorf("[NotificationPushHandler:Handle][err: %w]", err))
	}
	p.DedupeKey = msg.DedupeKey
	return h.eventCtrl.PublishNotificationPush(ctx, p)
}

// CounterUpdateHandler delivers outbox messages of event_type "restriction.counter.update".
type CounterUpdateHandler struct {
	restriction domain.RestrictionService
}

// NewCounterUpdateHandler creates a CounterUpdateHandler.
func NewCounterUpdateHandler(restriction domain.RestrictionService) *CounterUpdateHandler {
	return &CounterUpdateHandler{restriction: restriction}
}

// EventType returns the event_type string this handler processes.
func (h *CounterUpdateHandler) EventType() string {
	return domain.OutboxEventCounterUpdate
}

// Handle deserialises the CounterUpdatePayload and forwards the gRPC call to restriction-service.
// Payload unmarshal errors are non-retryable (bad payload can never succeed on retry).
// gRPC errors are retryable — the outbox worker will retry with backoff.
//
// The IdempotencyKey travels with the payload, so restriction-service collapses
// duplicate applications on its side.
func (h *CounterUpdateHandler) Handle(ctx context.Context, msg outbox.Message) error {
	var p domain.CounterUpdatePayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return outbox.MarkNonRetryable(fmt.Errorf("[CounterUpdateHandler:Handle][err: %w]", err))
	}

	return h.restriction.CountersUpdate(ctx, restriction.CounterUpdateParams{
		UserID:         p.UserID,
		OperationType:  restriction.LimitOperationType(p.OperationType),
		AssetCode:      currency.NewAssetCode(p.AssetCode),
		Amount:         p.Amount,
		OccurredAt:     p.OccurredAt,
		IsReverse:      p.IsReverse,
		IdempotencyKey: tools.ToPtr(p.IdempotencyKey),
	})
}
