package node

import (
	"context"
	"fmt"
	"strings"

	"account-service/config"
	"account-service/internal/domain"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tools"
)

const _generateAddressURL = "/api/wallet/generate-address"

type generateAddressRequest struct {
	Currency string `json:"currency"`
	Network  string `json:"network"`
	Address  string `json:"address,omitempty"`
}

type generateAddressResponse struct {
	Address  string `json:"address"`
	Currency string `json:"currency"`
	Network  string `json:"network"`
}

// GenerateAddress generates a new wallet address for the specified asset and network.
func (s Service) GenerateAddress(ctx context.Context, params domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error) {
	logger.LogCtx(ctx).Debugf("[node][Service:GenerateAddress][params:%+v]", params)

	var (
		response    generateAddressResponse
		errResponse nodeErrResponse
	)

	requestBody := &generateAddressRequest{
		Currency: params.AssetCode.String(),
		Network:  params.Network.String(),
	}
	if params.RefreshAddress != nil {
		requestBody.Address = *params.RefreshAddress
	}

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
		SetBody(requestBody).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Post(strings.TrimRight(config.Commons().NodeServiceURL, "/") + _generateAddressURL)
	if err != nil {
		return domain.GenerateBlockchainAddressResult{}, fmt.Errorf("[node][Service:GenerateAddress][err:%w]", err)
	}
	if !res.IsSuccessState() {
		return domain.GenerateBlockchainAddressResult{}, fmt.Errorf("[node][Service:GenerateAddress][httpCode:%d][appErrMessage:%s]", res.StatusCode, errResponse.Message)
	}

	logger.Log().Debugf("[node][Service:GenerateAddress][response:%+v]", response)

	return response.decode(), nil
}

// decode turns a generateAddressResponse into domain.GenerateBlockchainAddressResult.
func (r generateAddressResponse) decode() domain.GenerateBlockchainAddressResult {
	return domain.GenerateBlockchainAddressResult{
		Address: r.Address,
	}
}
