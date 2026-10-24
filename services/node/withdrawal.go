package node

import (
	"context"
	"fmt"
	"strings"

	"account-service/config"
	"account-service/internal/domain"

	"github.com/shopspring/decimal"
	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tools"
)

const _withdrawalURL = "/api/wallet/withdraw"

type withdrawalRequest struct {
	Currency       string          `json:"currency"`
	Network        string          `json:"network"`
	ToAddress      string          `json:"to_address"`
	Amount         decimal.Decimal `json:"amount"`
	Speed          string          `json:"speed"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type withdrawalResponse struct {
	RequestID             string          `json:"request_id"`
	TxID                  *string         `json:"txid"`
	Currency              string          `json:"currency"`
	Network               string          `json:"network"`
	Amount                decimal.Decimal `json:"amount"`
	FromAddress           string          `json:"from_address"`
	ToAddress             string          `json:"to_address"`
	Confirmations         uint8           `json:"confirmations"`
	RequiredConfirmations uint8           `json:"required_confirmations"`
	Status                string          `json:"status"`
	Fee                   decimal.Decimal `json:"fee"`
	GasLimit              uint64          `json:"gas_limit"`
	GasPrice              decimal.Decimal `json:"gas_price"`
	Unit                  string          `json:"unit"`
}

// Withdrawal initiates a withdrawal transaction via the node service.
func (s Service) Withdrawal(ctx context.Context, params domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
	logger.LogCtx(ctx).Debugf("[node][Service:Withdrawal][params:%+v]", params)

	var (
		response    withdrawalResponse
		errResponse nodeErrResponse
	)

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
		SetBody(&withdrawalRequest{
			Currency:       params.AssetCode.String(),
			Network:        params.Network.String(),
			ToAddress:      params.ToAddress,
			Amount:         params.Amount,
			Speed:          params.FeeTier.String(),
			IdempotencyKey: params.IdempotencyKey,
		}).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Post(strings.TrimRight(config.Commons().NodeServiceURL, "/") + _withdrawalURL)
	if err != nil {
		return domain.WithdrawalBlockchainTxResult{}, fmt.Errorf("[node][Service:Withdrawal][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return domain.WithdrawalBlockchainTxResult{}, fmt.Errorf("[node][Service:Withdrawal][httpCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Message)
	}

	logger.Log().Debugf("[node][Service:Withdrawal][response:%+v]", response)

	return response.decode(), nil
}

// decode turns a withdrawalResponse into domain.WithdrawalBlockchainTxResult.
func (r withdrawalResponse) decode() domain.WithdrawalBlockchainTxResult {
	return domain.WithdrawalBlockchainTxResult{
		ExternalRequestID:     r.RequestID,
		TxID:                  r.TxID,
		Amount:                r.Amount,
		Confirmations:         r.Confirmations,
		RequiredConfirmations: r.RequiredConfirmations,
		Status:                transactionStatusToDomainStatus(r.Status),
		Fee:                   r.Fee,
		FromAddress:           r.FromAddress,
	}
}
