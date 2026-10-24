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

const _getFeeURL = "/api/wallet/gas-fee/%s"

type getFeeResponse struct {
	Network         string          `json:"network"`
	Unit            string          `json:"unit"`
	EnergyPriceSun  decimal.Decimal `json:"energy_price_sun"`
	EstimatedEnergy decimal.Decimal `json:"estimated_energy"`
	Slow            getFeeItem      `json:"slow"`
	Standard        getFeeItem      `json:"standard"`
	Fast            getFeeItem      `json:"fast"`
}

type getFeeItem struct {
	GasPrice      decimal.Decimal `json:"gas_price"`
	EstimatedTime int64           `json:"estimated_time"`
	EstimatedFee  decimal.Decimal `json:"estimated_fee"`
}

// GetFees getting a list of fees and estimated sending times.
func (s Service) GetFees(ctx context.Context, network domain.ChainNetwork) (domain.GetBlockchainFeeResult, error) {
	logger.LogCtx(ctx).Debugf("[node][Service:GetFees][network: %s]", network.String())

	var (
		response    getFeeResponse
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
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Get(strings.TrimRight(config.Commons().NodeServiceURL, "/") + fmt.Sprintf(_getFeeURL, strings.ToLower(network.String())))
	if err != nil {
		return domain.GetBlockchainFeeResult{}, fmt.Errorf("[node][Service:GetFee][err: %w]", err)
	}
	if !res.IsSuccessState() {
		return domain.GetBlockchainFeeResult{}, fmt.Errorf("[node][Service:GetFee][httpCode: %d][appErrMessage: %s]", res.StatusCode, errResponse.Message)
	}

	logger.Log().Debugf("[node][Service:GetFee][response: %+v]", response)

	return response.decode(), nil
}

// decode turns a getFeeResponse into domain.GetBlockchainFeeResult.
func (r getFeeResponse) decode() domain.GetBlockchainFeeResult {
	return domain.GetBlockchainFeeResult{
		Network:         r.Network,
		Unit:            r.Unit,
		EnergyPriceSun:  r.EnergyPriceSun,
		EstimatedEnergy: r.EstimatedEnergy,
		Slow: domain.GetBlockchainFeeItem{
			GasPrice:      r.Slow.GasPrice,
			EstimatedTime: r.Slow.EstimatedTime,
			EstimatedFee:  r.Slow.EstimatedFee,
		},
		Standard: domain.GetBlockchainFeeItem{
			GasPrice:      r.Standard.GasPrice,
			EstimatedTime: r.Standard.EstimatedTime,
			EstimatedFee:  r.Standard.EstimatedFee,
		},
		Fast: domain.GetBlockchainFeeItem{
			GasPrice:      r.Fast.GasPrice,
			EstimatedTime: r.Fast.EstimatedTime,
			EstimatedFee:  r.Fast.EstimatedFee,
		},
	}
}
