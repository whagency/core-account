package blockchain

import (
	"net/http"

	"account-service/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/services/currency"
)

// ChainConfigListHandler provides an endpoint to get a chain configs list.
type ChainConfigListHandler struct {
	ctrl domain.BlockchainController
}

// NewChainConfigListHandler builds a new ChainConfigListHandler instance.
func NewChainConfigListHandler(c domain.BlockchainController) *ChainConfigListHandler {
	return &ChainConfigListHandler{
		ctrl: c,
	}
}

// chainConfigListResponse defines the response structure for a chain configs list.
type chainConfigListResponse map[string]map[string]chainConfigListItem

type chainConfigListItem struct {
	Decimals             uint8           `json:"decimals" example:"8"`
	DustThreshold        decimal.Decimal `json:"dustThreshold" example:"0.001"`
	MinConfirmations     uint8           `json:"minConfirmations" example:"1"`
	ConfirmFinality      uint8           `json:"confirmFinality" example:"19"`
	EstimatedArrivalTime int             `json:"estimatedArrivalTime" example:"150"`
	WithdrawFee          decimal.Decimal `json:"withdrawFee" example:"0.45"`
	DepositFee           decimal.Decimal `json:"depositFee" example:"1.1501"`
	WithdrawFeeType      string          `json:"withdrawFeeType" example:"percent"`
	DepositFeeType       string          `json:"depositFeeType" example:"fix_amount"`
	Status               string          `json:"status" example:"all"`
	AssetName            string          `json:"assetName" example:"USDT"`
	BlockUrl             string          `json:"blockUrl" example:"https://nile.tronscan.org/#/"`
	TxUrl                string          `json:"txUrl" example:"https://nile.tronscan.org/#/transaction/"`
	AddressUrl           string          `json:"addressUrl" example:"https://nile.tronscan.org/#/address/"`
	ContractAddressUrl   string          `json:"contractAddressUrl" example:"https://nile.tronscan.org/#/token20/"`
	AddressRegex         string          `json:"addressRegex" example:"^T[1-9A-HJ-NP-Za-km-z]{33}$"`
}

// Handle get a chain configs list.
//
//	@Summary        Get a chain configs list
//	@Description    Return a chain configs list.
//	@Tags           blockchain
//	@Security       Authorization
//	@Accept         json
//	@Produce        json
//	@Success        200 {object} commonHTTP.Response{data=chainConfigListResponse} "Chain configs list."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Failed getting a chain configs list due to internal errors."
//	@Router         /api/v1/blockchain/configs [get]
func (h ChainConfigListHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	result, err := h.ctrl.GetChainConfigs(ctx)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeChainConfigsListResponse(result)))
}

// encodeChainConfigsListResponse encodes the domain.ChainConfigs into a response format.
func encodeChainConfigsListResponse(res domain.ChainConfigs) chainConfigListResponse {
	encoded := make(chainConfigListResponse)
	for assetCode, assetConfig := range res {
		assetCodeStr := assetCode.String()
		encoded[assetCodeStr] = make(map[string]chainConfigListItem)
		for network, config := range assetConfig {
			networkStr := network.String()

			var blockUrl string
			if config.BlockUrl != nil {
				blockUrl = *config.BlockUrl
			}
			var txUrl string
			if config.TxUrl != nil {
				txUrl = *config.TxUrl
			}
			var addressUrl string
			if config.AddressUrl != nil {
				addressUrl = *config.AddressUrl
			}
			var contractAddressUrl string
			if config.ContractAddressUrl != nil {
				contractAddressUrl = *config.ContractAddressUrl
			}
			var addressRegex string
			if config.AddressRegex != nil {
				addressRegex = *config.AddressRegex
			}

			encoded[assetCodeStr][networkStr] = chainConfigListItem{
				Decimals:             config.Decimals,
				DustThreshold:        config.DustThreshold.Shift(-currency.DefaultAssetScale),
				MinConfirmations:     config.MinConfirmations,
				ConfirmFinality:      config.ConfirmFinality,
				EstimatedArrivalTime: config.EstimatedArrivalTime,
				WithdrawFee:          config.WithdrawFee,
				DepositFee:           config.DepositFee,
				WithdrawFeeType:      config.WithdrawFeeType.String(),
				DepositFeeType:       config.DepositFeeType.String(),
				Status:               config.Status.String(),
				AssetName:            config.AssetName,
				BlockUrl:             blockUrl,
				TxUrl:                txUrl,
				AddressUrl:           addressUrl,
				ContractAddressUrl:   contractAddressUrl,
				AddressRegex:         addressRegex,
			}
		}
	}
	return encoded
}
