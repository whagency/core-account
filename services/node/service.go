package node

import (
	"account-service/internal/domain"

	"github.com/imroc/req/v3"
)

type Service struct {
	client *req.Client
}

func NewNodeService(client *req.Client) *Service {
	return &Service{client: client}
}

type nodeErrResponse struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
}

// transactionStatusToDomainStatus converts node transaction status string to domain.BlockchainTxStatus.
//
// Both legacy ("confirmed") and the GET /wallet/transaction-status response
// vocabulary ("success", emitted by NETWORK_STATUSES in node-service) map to
// BlockchainTxStatusConfirmed — they describe the same terminal state.
func transactionStatusToDomainStatus(status string) domain.BlockchainTxStatus {
	switch status {
	case "new":
		return domain.BlockchainTxStatusNew
	case "pending":
		return domain.BlockchainTxStatusPending
	case "confirmed", "success":
		return domain.BlockchainTxStatusConfirmed
	case "failed":
		return domain.BlockchainTxStatusFailed
	case "ignored":
		return domain.BlockchainTxStatusIgnored
	case "pending_signatures":
		return domain.BlockchainTxPendingSignatures
	default:
		return domain.BlockchainTxStatusNew
	}
}
