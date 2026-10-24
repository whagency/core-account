package node

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"account-service/config"
	"account-service/internal/domain"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tools"
)

const (
	_transactionStatusURL          = "/api/wallet/transaction-status/"
	_transactionStatusByRequestURL = "/api/wallet/transaction-status/by-request/"
)

type transactionStatusResponse struct {
	TxID                  *string `json:"txid"`
	Currency              string  `json:"currency"`
	Network               string  `json:"network"`
	Confirmations         uint8   `json:"confirmations"`
	RequiredConfirmations uint8   `json:"required_confirmations"`
	Status                string  `json:"status"`
	RequestID             string  `json:"request_id"`
}

// decode turns a transactionStatusResponse into domain.TransactionStatusResult.
func (r transactionStatusResponse) decode() domain.TransactionStatusResult {
	return domain.TransactionStatusResult{
		TxID:                  r.TxID,
		Status:                transactionStatusToDomainStatus(r.Status),
		Confirmations:         r.Confirmations,
		RequiredConfirmations: r.RequiredConfirmations,
	}
}

// GetTransactionStatus fetches the current chain status for a previously
// submitted transaction. Used by the withdrawal reconciliation cron to detect
// stuck Pending withdrawals when the blockchain.transaction.event callback was
// lost. This call is read-only — no side effects on the chain.
func (s Service) GetTransactionStatus(ctx context.Context, network domain.ChainNetwork, txHash string) (domain.TransactionStatusResult, error) {
	logger.LogCtx(ctx).Debugf("[node][Service:GetTransactionStatus][network:%s][txHash:%s]", network.String(), txHash)

	var (
		response    transactionStatusResponse
		errResponse nodeErrResponse
	)

	endpoint := strings.TrimRight(config.Commons().NodeServiceURL, "/") +
		_transactionStatusURL + url.PathEscape(txHash)

	// Bound this call without touching the shared client — see
	// commonHTTP.WithCallTimeout.
	ctx, cancel := commonHTTP.WithCallTimeout(ctx, config.Commons().TimeoutExtCall)
	defer cancel()

	res, err := s.client.
		R().
		SetContext(ctx).
		SetHeaders(map[string]string{
			tools.AcceptHeader:                   tools.JSONContentType,
			logger.DefaultRequestIDHeader:        logger.GetRequestID(ctx),
			config.Commons().SecurityTokenHeader: config.Commons().SecurityToken,
		}).
		SetQueryParam("network", network.String()).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Get(endpoint)
	if err != nil {
		return domain.TransactionStatusResult{}, fmt.Errorf("[node][Service:GetTransactionStatus][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return domain.TransactionStatusResult{}, fmt.Errorf("[node][Service:GetTransactionStatus][httpCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Message)
	}

	return response.decode(), nil
}

// GetTransactionStatusByRequestID fetches the current node-side transaction
// status by request id. This supports multisig withdrawals before an on-chain
// tx hash exists, and returns the tx hash once node-service has broadcasted it.
func (s Service) GetTransactionStatusByRequestID(ctx context.Context, network domain.ChainNetwork, requestID string) (domain.TransactionStatusResult, error) {
	logger.LogCtx(ctx).Debugf("[node][Service:GetTransactionStatusByRequestID][network:%s][requestID:%s]", network.String(), requestID)

	var (
		response    transactionStatusResponse
		errResponse nodeErrResponse
	)

	endpoint := strings.TrimRight(config.Commons().NodeServiceURL, "/") +
		_transactionStatusByRequestURL + url.PathEscape(requestID)

	// Bound this call without touching the shared client — see
	// commonHTTP.WithCallTimeout.
	ctx, cancel := commonHTTP.WithCallTimeout(ctx, config.Commons().TimeoutExtCall)
	defer cancel()

	res, err := s.client.
		R().
		SetContext(ctx).
		SetHeaders(map[string]string{
			tools.AcceptHeader:                   tools.JSONContentType,
			logger.DefaultRequestIDHeader:        logger.GetRequestID(ctx),
			config.Commons().SecurityTokenHeader: config.Commons().SecurityToken,
		}).
		SetQueryParam("network", network.String()).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Get(endpoint)
	if err != nil {
		return domain.TransactionStatusResult{}, fmt.Errorf("[node][Service:GetTransactionStatusByRequestID][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return domain.TransactionStatusResult{}, fmt.Errorf("[node][Service:GetTransactionStatusByRequestID][httpCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Message)
	}

	return response.decode(), nil
}
