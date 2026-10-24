package domain

import (
	"context"
)

type NotificationPurpose string

// Supported notification purposes.
const (
	NotificationPurposePushSuccessfulDeposit           NotificationPurpose = "successfulDeposit"
	NotificationPurposePushRequiredVerificationDeposit NotificationPurpose = "requiredVerificationDeposit"
	NotificationPurposePushSuccessfulWithdrawal        NotificationPurpose = "successfulWithdrawal"
	NotificationPurposePushSuccessfulIncomingTransfer  NotificationPurpose = "successfulIncomingTransfer"
	NotificationPurposePushSuccessfulOutgoingTransfer  NotificationPurpose = "successfulOutgoingTransfer"
)

type PushData map[string]any

// PushMessage is both the outbox JSON payload for event_type = "notification.push"
// and the argument to EventController.PublishNotificationPush.
//
// When the outbox worker picks up a "notification.push" row it deserialises this
// struct and calls PublishNotificationPush, which forwards it to the notification
// push exchange. Payload unmarshal errors are non-retryable.
type PushMessage struct {
	Purpose NotificationPurpose `json:"purpose"`
	Data    PushData            `json:"data"`

	// DedupeKey identifies this notification to notification-service so an
	// outbox retry does not deliver it to the user twice. It is set by the
	// outbox handler from the message's own dedupe_key, not by the enqueuing
	// controller, so it always matches the row that produced the publish.
	DedupeKey string `json:"dedupeKey,omitempty"`
	Locale    string `json:"locale"`
}

type WithdrawalRequestCreatedEvent struct {
	ID uint64 `json:"id"`
}

type BlockchainTransactionUpdateEvent struct {
	ID   uint64 `json:"id"`
	Type uint8  `json:"type"`
}

// EventController is responsible for publishing withdrawal-related events to the message queue.
type EventController interface {
	PublishWithdrawalCreated(context.Context, WithdrawalRequestCreatedEvent) error
	PublishTransactionRollback(context.Context, ID) error
	PublishNotificationPush(context.Context, PushMessage) error
	PublishBlockchainTransactionUpdate(context.Context, BlockchainTransactionUpdateEvent) error
}
