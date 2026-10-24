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

type blockchainTransactionUpdateMessage struct {
	ID   uint64 `json:"id"`
	Type uint8  `json:"type"`
}

// BlockchainTransactionLifecycleHandler handles blockchain transaction lifecycle messages.
type BlockchainTransactionLifecycleHandler struct {
	depositCtrl    domain.DepositController
	withdrawalCtrl domain.WithdrawalController
}

// NewBlockchainTransactionLifecycleHandler creates a new instance of BlockchainTransactionLifecycleHandler.
func NewBlockchainTransactionLifecycleHandler(d domain.DepositController, w domain.WithdrawalController) *BlockchainTransactionLifecycleHandler {
	return &BlockchainTransactionLifecycleHandler{
		depositCtrl:    d,
		withdrawalCtrl: w,
	}
}

// Handle handles the incoming message.
func (h BlockchainTransactionLifecycleHandler) Handle(ctx context.Context, msgType queue.MessageType, data []byte) error {
	logger.LogCtx(ctx).Debugf("[BlockchainTransactionLifecycleHandler:Handle][type: %s][msg: %s]", msgType.String(), string(data))

	switch msgType {
	case domain.MessageTypeBlockchainTransactionUpdated:
		if err := h.handleBlockchainTransactionUpdate(ctx, data); err != nil {
			return fmt.Errorf("[BlockchainTransactionLifecycleHandler:Handle]%w", err)
		}
	default:
		return queue.MarkNonRetryable(fmt.Errorf("[BlockchainTransactionLifecycleHandler:Handle][err: unexpected message type %q: %w]", msgType.String(), domain.ErrMessageBadFormat))
	}

	return nil
}

// handleBlockchainTransactionUpdate processes the blockchain.transaction.updated message.
func (h BlockchainTransactionLifecycleHandler) handleBlockchainTransactionUpdate(ctx context.Context, data []byte) error {
	var message blockchainTransactionUpdateMessage
	if err := jsoniter.Unmarshal(data, &message); err != nil {
		return queue.MarkNonRetryable(fmt.Errorf("[handleBlockchainTransactionUpdate][err: %w]", domain.ErrMessageBadFormat))
	}
	if message.ID == 0 {
		return queue.MarkNonRetryable(fmt.Errorf("[handleBlockchainTransactionUpdate][err: id is required: %w]", domain.ErrMessageBadFormat))
	}

	switch message.Type {
	case domain.BlockchainTxTypeInbound.Uint8():
		if err := h.depositCtrl.Process(ctx, domain.ProcessDepositParams{BlockchainTxID: domain.NewIDFromUint64(message.ID)}); err != nil {
			return discardOnKnownAccountQueueError(
				fmt.Errorf("[handleBlockchainTransactionUpdate]%w", err),
				appErrors.RecordNotFoundError,
			)
		}
	case domain.BlockchainTxTypeOutbound.Uint8():
		if err := h.withdrawalCtrl.Process(ctx, domain.ProcessWithdrawalParams{BlockchainTxID: domain.NewIDFromUint64(message.ID)}); err != nil {
			return discardOnKnownAccountQueueError(
				fmt.Errorf("[handleBlockchainTransactionUpdate]%w", err),
				appErrors.RecordNotFoundError,
			)
		}
	default:
		return queue.MarkNonRetryable(fmt.Errorf("[handleBlockchainTransactionUpdate][err: unsupported blockchain tx type %d: %w]", message.Type, domain.ErrMessageBadFormat))
	}

	return nil
}
