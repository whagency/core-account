package handlers

import (
	"context"
	"fmt"

	"account-service/internal/domain"

	jsoniter "github.com/json-iterator/go"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	commonUserQueue "github.com/whagency/core-common/services/user/queue"
)

// UserCreatedLifeCycleHandler handles user creation lifecycle events.
type UserCreatedLifeCycleHandler struct {
	ctrl domain.AccountController
}

// NewUserCreatedLifeCycleHandler creates a new instance of UserLifeCycleHandler.
func NewUserCreatedLifeCycleHandler(ctrl domain.AccountController) *UserCreatedLifeCycleHandler {
	return &UserCreatedLifeCycleHandler{
		ctrl: ctrl,
	}
}

// Handle handles the incoming message.
func (h UserCreatedLifeCycleHandler) Handle(ctx context.Context, msgType queue.MessageType, data []byte) error {
	logger.LogCtx(ctx).Debugf("[UserCreatedLifeCycleHandler:Handle][type: %s][msg: %s]", msgType.String(), string(data))

	if msgType != "" && msgType != commonUserQueue.MessageTypeUserCreated {
		return queue.MarkNonRetryable(fmt.Errorf("[UserCreatedLifeCycleHandler:Handle][err: unexpected message type %q: %w]", msgType.String(), domain.ErrMessageBadFormat))
	}

	var message commonUserQueue.UserCreatedEvent
	if err := jsoniter.Unmarshal(data, &message); err != nil {
		return queue.MarkNonRetryable(fmt.Errorf("[UserCreatedLifeCycleHandler:Handle][err: %w]", domain.ErrMessageBadFormat))
	}
	if message.UserID == 0 {
		return queue.MarkNonRetryable(fmt.Errorf("[UserCreatedLifeCycleHandler:Handle][err: userId is required: %w]", domain.ErrMessageBadFormat))
	}
	if message.OccurredAt.IsZero() {
		return queue.MarkNonRetryable(fmt.Errorf("[UserCreatedLifeCycleHandler:Handle][err: occurredAt is required: %w]", domain.ErrMessageBadFormat))
	}

	if err := h.ctrl.CreateDefaultAccountsForUser(ctx, domain.NewIDFromUint64(message.UserID)); err != nil {
		return fmt.Errorf("[UserCreatedLifeCycleHandler:Handle]%w", err)
	}

	return nil
}
