package handlers

import (
	"context"
	"fmt"

	"account-service/internal/domain"

	jsoniter "github.com/json-iterator/go"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	commonAccountQueue "github.com/whagency/core-common/services/account/queue"
)

// TransactionRollbackHandler handles withdrawal request lifecycle messages.
type TransactionRollbackHandler struct {
	transactionCtrl domain.TransactionController
}

// NewTransactionRollbackHandler creates a new instance of TransactionRollbackHandler.
func NewTransactionRollbackHandler(transactionCtrl domain.TransactionController) *TransactionRollbackHandler {
	return &TransactionRollbackHandler{
		transactionCtrl: transactionCtrl,
	}
}

// Handle handles the incoming message.
func (h TransactionRollbackHandler) Handle(ctx context.Context, msgType queue.MessageType, data []byte) error {
	logger.LogCtx(ctx).Debugf("[TransactionRollbackHandler:Handle][type: %s][msg: %s]", msgType.String(), string(data))

	if msgType != "" && msgType != domain.MessageTypeTransactionRollbackRequested {
		return queue.MarkNonRetryable(fmt.Errorf("[TransactionRollbackHandler:Handle][err: unexpected message type %q: %w]", msgType.String(), domain.ErrMessageBadFormat))
	}

	var message commonAccountQueue.TransactionRollbackRequestedMessage
	if err := jsoniter.Unmarshal(data, &message); err != nil {
		return queue.MarkNonRetryable(fmt.Errorf("[TransactionRollbackHandler:Handle][err: %w]", domain.ErrMessageBadFormat))
	}
	if message.ID == 0 {
		return queue.MarkNonRetryable(fmt.Errorf("[TransactionRollbackHandler:Handle][err: id is required: %w]", domain.ErrMessageBadFormat))
	}

	if err := h.transactionCtrl.RollbackTransaction(ctx, domain.NewIDFromUint64(message.ID)); err != nil {
		return discardOnKnownAccountQueueError(
			fmt.Errorf("[TransactionRollbackHandler:Handle]%w", err),
			appErrors.RecordNotFoundError,
			appErrors.InvalidRequestError,
		)
	}

	return nil
}
