package controllers

import (
	"context"
	"fmt"

	"account-service/internal/domain"

	jsoniter "github.com/json-iterator/go"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	commonAccountQueue "github.com/whagency/core-common/services/account/queue"
	commonNotificationQueue "github.com/whagency/core-common/services/notification/queue"
)

type EventCtrl struct {
	blockchainTransactionLifecyclePublisher queue.Publisher
	withdrawalLifecyclePublisher            queue.Publisher
	notificationPushPublisher               queue.Publisher
	transactionRollbackPublisher            queue.Publisher
}

// NewEventController creates a new instance of BlockchainEventCtrl.
func NewEventController(
	blockchainTransactionLifecyclePublisher queue.Publisher,
	withdrawalLifecyclePublisher queue.Publisher,
	notificationPushPublisher queue.Publisher,
	transactionRollbackPublisher queue.Publisher,
) *EventCtrl {
	return &EventCtrl{
		blockchainTransactionLifecyclePublisher: blockchainTransactionLifecyclePublisher,
		withdrawalLifecyclePublisher:            withdrawalLifecyclePublisher,
		notificationPushPublisher:               notificationPushPublisher,
		transactionRollbackPublisher:            transactionRollbackPublisher,
	}
}

// PublishBlockchainTransactionUpdate publishes a domain.BlockchainTransactionUpdateEvent to the message queue.
func (c EventCtrl) PublishBlockchainTransactionUpdate(ctx context.Context, event domain.BlockchainTransactionUpdateEvent) error {
	logger.LogCtx(ctx).Debugf("[EventCtrl:PublishBlockchainTransactionUpdate][event: %+v]", event)

	payload, err := jsoniter.Marshal(event)
	if err != nil {
		return fmt.Errorf("[EventCtrl:PublishBlockchainTransactionUpdate][err: %w]", err)
	}

	if err = c.blockchainTransactionLifecyclePublisher.Publish(ctx, queue.Message{
		MessageType: domain.MessageTypeBlockchainTransactionUpdated,
		Data:        payload,
	}); err != nil {
		return fmt.Errorf("[EventCtrl:PublishBlockchainTransactionUpdate]%w", err)
	}

	return nil
}

// PublishWithdrawalCreated publishes a domain.WithdrawalRequestCreatedEvent to the message queue.
func (c EventCtrl) PublishWithdrawalCreated(ctx context.Context, event domain.WithdrawalRequestCreatedEvent) error {
	logger.LogCtx(ctx).Debugf("[EventCtrl:PublishWithdrawalCreated][event: %+v]", event)

	payload, err := jsoniter.Marshal(event)
	if err != nil {
		return fmt.Errorf("[EventCtrl:PublishWithdrawalCreated][err: %w]", err)
	}

	if err = c.withdrawalLifecyclePublisher.Publish(ctx, queue.Message{
		MessageType: domain.MessageTypeWithdrawalRequestCreated,
		Data:        payload,
	}); err != nil {
		return fmt.Errorf("[EventCtrl:PublishWithdrawalCreated]%w", err)
	}

	return nil
}

// PublishNotificationPush sends the push notification message to the specified destination using the configured publisher.
func (c EventCtrl) PublishNotificationPush(ctx context.Context, message domain.PushMessage) error {
	logger.LogCtx(ctx).Debugf("[EventCtrl:PublishNotificationPush][message: %+v]", message)

	payload := commonNotificationQueue.PushMessage{
		Purpose:   string(message.Purpose),
		Data:      map[string]any(message.Data),
		Locale:    message.Locale,
		DedupeKey: message.DedupeKey,
	}

	messageBytes, err := jsoniter.Marshal(payload)
	if err != nil {
		return fmt.Errorf("[EventCtrl:PublishNotificationPush][err: %w]", err)
	}

	if err = c.notificationPushPublisher.Publish(ctx, queue.Message{
		MessageType: commonNotificationQueue.MessageTypePush,
		Data:        messageBytes,
	}); err != nil {
		return fmt.Errorf("[EventCtrl:PublishNotificationPush]%w", err)
	}

	return nil
}

// PublishTransactionRollback publishes a rollback request for the given journal ID.
// This can be used from withdrawal, deposit, or transfer flows when a compensating action is required.
func (c EventCtrl) PublishTransactionRollback(ctx context.Context, journalID domain.ID) error {
	logger.LogCtx(ctx).Debugf("[EventCtrl:PublishTransactionRollback][journalID: %d]", journalID)

	payload, err := jsoniter.Marshal(commonAccountQueue.TransactionRollbackRequestedMessage{ID: journalID.Uint64()})
	if err != nil {
		return fmt.Errorf("[EventCtrl:PublishTransactionRollback][err: %w]", err)
	}

	if err = c.transactionRollbackPublisher.Publish(ctx, queue.Message{
		MessageType: domain.MessageTypeTransactionRollbackRequested,
		Data:        payload,
	}); err != nil {
		return fmt.Errorf("[EventCtrl:PublishTransactionRollback]%w", err)
	}

	return nil
}
