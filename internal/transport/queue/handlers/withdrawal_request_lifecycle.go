package handlers

import (
	"context"
	"fmt"

	"account-service/internal/domain"

	jsoniter "github.com/json-iterator/go"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
)

// WithdrawalRequestLifecycleHandler handles withdrawal request lifecycle messages.
type WithdrawalRequestLifecycleHandler struct {
	withdrawalCtrl domain.WithdrawalController
}

// NewWithdrawalRequestLifecycleHandler creates a new instance of WithdrawalRequestLifecycleHandler.
func NewWithdrawalRequestLifecycleHandler(w domain.WithdrawalController) *WithdrawalRequestLifecycleHandler {
	return &WithdrawalRequestLifecycleHandler{
		withdrawalCtrl: w,
	}
}

// Handle handles the incoming message.
func (h WithdrawalRequestLifecycleHandler) Handle(ctx context.Context, msgType queue.MessageType, data []byte) error {
	logger.LogCtx(ctx).Debugf("[WithdrawalRequestLifecycleHandler:Handle][type: %s][msg: %s]", msgType.String(), string(data))

	switch msgType {
	case domain.MessageTypeWithdrawalRequestCreated:
		if err := h.handleWithdrawalRequestCreated(ctx, data); err != nil {
			return fmt.Errorf("[WithdrawalRequestLifecycleHandler:Handle]%w", err)
		}
	default:
		return queue.MarkNonRetryable(fmt.Errorf("[WithdrawalRequestLifecycleHandler:Handle][err: unexpected message type %q: %w]", msgType.String(), domain.ErrMessageBadFormat))
	}

	return nil
}

// handleWithdrawalRequestCreated processes the withdrawal.request.created message.
func (h WithdrawalRequestLifecycleHandler) handleWithdrawalRequestCreated(ctx context.Context, data []byte) error {
	var message domain.WithdrawalRequestCreatedEvent
	if err := jsoniter.Unmarshal(data, &message); err != nil {
		return queue.MarkNonRetryable(fmt.Errorf("[handleWithdrawalRequestCreated][err: %w]", domain.ErrMessageBadFormat))
	}
	if message.ID == 0 {
		return queue.MarkNonRetryable(fmt.Errorf("[handleWithdrawalRequestCreated][err: id is required: %w]", domain.ErrMessageBadFormat))
	}

	if err := h.withdrawalCtrl.SendToBlockchain(ctx, domain.SendWithdrawalToBlockchain{WithdrawalRequestID: domain.NewIDFromUint64(message.ID)}); err != nil {
		return discardOnKnownAccountQueueError(
			fmt.Errorf("[handleWithdrawalRequestCreated]%w", err),
			appErrors.RecordNotFoundError,
			appErrors.InvalidRequestError,
		)
	}

	return nil
}
