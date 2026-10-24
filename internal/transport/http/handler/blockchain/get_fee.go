package blockchain

import (
	"net/http"
	"slices"
	"strings"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"
	httpValidation "account-service/internal/transport/http/validation"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
	commonValidation "github.com/whagency/core-common/http/validation"
	"github.com/whagency/core-common/services/currency"
	commonTools "github.com/whagency/core-common/tools"
)

// GetFeeHandler provides an endpoint to get gas fee list.
type GetFeeHandler struct {
	ctrl domain.BlockchainController
}

// NewGetFeeHandler builds a new GetFeeHandler instance.
func NewGetFeeHandler(c domain.BlockchainController) *GetFeeHandler {
	return &GetFeeHandler{
		ctrl: c,
	}
}

type getFeeRequestURLParams struct {
	Network string `uri:"network" validate:"required,not_blank" example:"trx" enums:"trx"`
	Asset   string `uri:"asset" validate:"required,not_blank" example:"USDT" enums:"USDT"`
}

// _getFeeAllowedAssetCodes defines the supported asset codes for the fee endpoint.
var _getFeeAllowedAssetCodes = []currency.AssetCode{
	currency.DefaultAccountAssetCode,
}

// _getFeeAllowedNetworks defines the supported blockchain networks for the fee endpoint.
var _getFeeAllowedNetworks = []domain.ChainNetwork{
	domain.ChainNetworkTRX,
}

// getFeeResponse defines the response structure for fee list.
type getFeeResponse struct {
	Network         string          `json:"network" example:"trx"`
	Unit            string          `json:"unit" example:"USDT"`
	EnergyPriceSun  decimal.Decimal `json:"energy_price_sun" example:"0.056"`
	EstimatedEnergy decimal.Decimal `json:"estimated_energy" example:"0.78"`
	Slow            getFeeItem      `json:"slow"`
	Standard        getFeeItem      `json:"standard"`
	Fast            getFeeItem      `json:"fast"`
}

type getFeeItem struct {
	GasPrice          decimal.Decimal `json:"gas_price" example:"0.0056"`
	EstimatedTime     int64           `json:"estimated_time" example:"140"`
	EstimatedFee      decimal.Decimal `json:"estimated_fee" example:"0.1098"`
	EstimatedFeeAsset decimal.Decimal `json:"estimated_fee_asset" example:"1.0046"`
}

// Handle get the blockchain gas fee list.
//
//	@Summary        Get the blockchain gas fee list
//	@Description    Return the blockchain gas fee list and estimated time for sending transaction.
//	@Tags           blockchain
//	@Security       Authorization
//	@Accept         json
//	@Produce        json
//	@Param          network path string true "Blockchain network name" Enums(trx)
//	@Param          asset path string true "Blockchain asset code" Enums(USDT)
//	@Success        200 {object} commonHTTP.Response{data=getFeeResponse} "Blockchain fee list."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Failed getting the fee list due to internal errors."
//	@Router         /api/v1/blockchain/fees/{network}/{asset} [get]
func (h GetFeeHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	var urlParams getFeeRequestURLParams
	if !commonValidation.BindAndValidateURI(c, httpValidation.Validator(), &urlParams, "[GetFeeHandler:Handle]") {
		return
	}

	params, erDecode := urlParams.toDomain()
	if erDecode != nil {
		c.AbortWithStatusJSON(erDecode.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, erDecode))
		return
	}

	result, err := h.ctrl.GetFees(ctx, params)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeGetFeeResponse(result)))
}

// toDomain converts the getFeeRequestURLParams into domain.GetFeesParams.
func (r getFeeRequestURLParams) toDomain() (domain.GetFeesParams, *appErrors.ErrorResponse) {
	assetCode, erAssetCode := decodeGetFeeAssetCode("asset", r.Asset)
	if erAssetCode != nil {
		return domain.GetFeesParams{}, erAssetCode
	}

	network, erNetwork := decodeGetFeeNetwork("network", r.Network)
	if erNetwork != nil {
		return domain.GetFeesParams{}, erNetwork
	}

	return domain.GetFeesParams{
		AssetCode: assetCode,
		Network:   network,
	}, nil
}

// decodeGetFeeAssetCode validates and normalizes the asset code for the fee endpoint.
func decodeGetFeeAssetCode(field, value string) (currency.AssetCode, *appErrors.ErrorResponse) {
	assetCode := currency.NewAssetCode(strings.ToUpper(strings.TrimSpace(value)))
	if slices.Contains(_getFeeAllowedAssetCodes, assetCode) {
		return assetCode, nil
	}

	return "", svcErrors.NewOneOfValidationError(
		"[GetFeeHandler:Handle]",
		field,
		value,
		commonTools.StringValues(_getFeeAllowedAssetCodes),
	)
}

// decodeGetFeeNetwork validates and normalizes the blockchain network for the fee endpoint.
func decodeGetFeeNetwork(field, value string) (domain.ChainNetwork, *appErrors.ErrorResponse) {
	network, err := domain.NewChainNetworkFromString(value)
	if err == nil && slices.Contains(_getFeeAllowedNetworks, network) {
		return network, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[GetFeeHandler:Handle]",
		field,
		value,
		commonTools.StringValues(_getFeeAllowedNetworks),
	)
}

// encodeGetFeeResponse encodes the domain.GetBlockchainFeeResult into a response format.
func encodeGetFeeResponse(res domain.GetBlockchainFeeResult) getFeeResponse {
	return getFeeResponse{
		Network:         res.Network,
		Unit:            res.Unit,
		EnergyPriceSun:  res.EnergyPriceSun,
		EstimatedEnergy: res.EstimatedEnergy,
		Slow: getFeeItem{
			GasPrice:          res.Slow.GasPrice,
			EstimatedTime:     res.Slow.EstimatedTime,
			EstimatedFee:      res.Slow.EstimatedFee,
			EstimatedFeeAsset: res.GetSlowFee().Round(currency.DefaultAssetScale),
		},
		Standard: getFeeItem{
			GasPrice:          res.Standard.GasPrice,
			EstimatedTime:     res.Standard.EstimatedTime,
			EstimatedFee:      res.Standard.EstimatedFee,
			EstimatedFeeAsset: res.GetStandardFee().Round(currency.DefaultAssetScale),
		},
		Fast: getFeeItem{
			GasPrice:          res.Fast.GasPrice,
			EstimatedTime:     res.Fast.EstimatedTime,
			EstimatedFee:      res.Fast.EstimatedFee,
			EstimatedFeeAsset: res.GetFastFee().Round(currency.DefaultAssetScale),
		},
	}
}
