package webhooks

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

type ProcessTxHandler struct {
	ctrl domain.BlockchainController
}

// NewProcessTxHandler creates a new instance of ProcessTxHandler.
func NewProcessTxHandler(c domain.BlockchainController) *ProcessTxHandler {
	return &ProcessTxHandler{
		ctrl: c,
	}
}

// processTxRequest represents the webhook request for blockchain transactions
type processTxRequest struct {
	RequestID             string          `json:"request_id" validate:"required,not_blank" example:"1234567890"`
	TxID                  *string         `json:"txid" validate:"omitempty,not_blank" example:"dee1805a013369317c9df462376881e4a56679fc05a01cecec46e059e377820c"`
	Currency              string          `json:"currency" validate:"required,not_blank" example:"USDT" enums:"USDT"`
	Network               string          `json:"network" validate:"required,not_blank" example:"trx" enums:"trx"`
	Type                  string          `json:"type" validate:"required,not_blank" example:"incoming" enums:"incoming,outgoing"`
	FromAddress           string          `json:"from_address" validate:"required,not_blank" example:"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"`
	ToAddress             string          `json:"to_address" validate:"required,not_blank" example:"3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy"`
	Amount                decimal.Decimal `json:"amount" validate:"required" example:"1200.056"`
	Confirmations         uint8           `json:"confirmations" validate:"required" example:"5"`
	RequiredConfirmations uint8           `json:"required_confirmations" validate:"required" example:"17"`
	Status                string          `json:"status" validate:"required,not_blank" example:"new" enums:"new,pending,confirmed,failed,ignored,blocked,pending_signatures"`
}

// _processTxAllowedAssetCodes defines the supported asset codes for blockchain transaction processing.
var _processTxAllowedAssetCodes = []currency.AssetCode{
	currency.DefaultAccountAssetCode,
}

// _processTxAllowedNetworks defines the supported blockchain networks for transaction processing.
var _processTxAllowedNetworks = []domain.ChainNetwork{
	domain.ChainNetworkTRX,
}

// _processTxAllowedTypes defines the supported transaction directions for blockchain webhooks.
var _processTxAllowedTypes = []domain.BlockchainTxType{
	domain.BlockchainTxTypeInbound,
	domain.BlockchainTxTypeOutbound,
}

// _processTxAllowedStatuses defines the supported blockchain transaction statuses for webhooks.
var _processTxAllowedStatuses = []domain.BlockchainTxStatus{
	domain.BlockchainTxStatusNew,
	domain.BlockchainTxStatusPending,
	domain.BlockchainTxStatusConfirmed,
	domain.BlockchainTxStatusFailed,
	domain.BlockchainTxStatusIgnored,
	domain.BlockchainTxStatusBlocked,
	domain.BlockchainTxPendingSignatures,
}

// Handle processes the blockchain transaction webhook.
//
//	@Summary        Process blockchain transaction
//	@Description    Processes a blockchain transaction notification from a node.
//	@Tags           blockchain
//	@Accept         json
//	@Produce        json
//	@Param          request body processTxRequest true "Blockchain transaction payload. Allowed values: currency=USDT, network=trx, type=incoming|outgoing, status=new|pending|confirmed|failed|ignored|blocked."
//	@Success        200 {object} commonHTTP.Response{data=object} "Transaction processed successfully."
//	@Failure        400 {object} commonHTTP.Response "Malformed payload or unsupported asset/network."
//	@Failure        500 {object} commonHTTP.Response "Failed to process transaction due to internal errors."
//	@Router         /api/v1/internal/blockchain/webhooks/transaction/process [post]
func (h ProcessTxHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	var req processTxRequest
	if !commonValidation.BindAndValidateJSON(c, httpValidation.Validator(), &req, "[ProcessTxHandler:Handle]") {
		return
	}

	params, erDecode := req.toDomain()
	if erDecode != nil {
		c.AbortWithStatusJSON(erDecode.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, erDecode))
		return
	}

	err := h.ctrl.ProcessTransaction(ctx, params)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, nil))
}

// toDomain converts processTxRequest to domain.
func (r *processTxRequest) toDomain() (domain.ProcessBlockchainTxParams, *appErrors.ErrorResponse) {
	assetCode, erAssetCode := decodeProcessTxAssetCode("currency", r.Currency)
	if erAssetCode != nil {
		return domain.ProcessBlockchainTxParams{}, erAssetCode
	}

	network, erNetwork := decodeProcessTxNetwork("network", r.Network)
	if erNetwork != nil {
		return domain.ProcessBlockchainTxParams{}, erNetwork
	}

	txType, erTxType := decodeProcessTxType("type", r.Type)
	if erTxType != nil {
		return domain.ProcessBlockchainTxParams{}, erTxType
	}

	status, erStatus := decodeProcessTxStatus("status", r.Status)
	if erStatus != nil {
		return domain.ProcessBlockchainTxParams{}, erStatus
	}

	return domain.ProcessBlockchainTxParams{
		ExternalRequestID:     r.RequestID,
		TxID:                  r.TxID,
		AssetCode:             assetCode,
		Network:               network,
		Type:                  txType,
		FromAddress:           r.FromAddress,
		ToAddress:             r.ToAddress,
		Amount:                r.Amount,
		Confirmations:         r.Confirmations,
		RequiredConfirmations: r.RequiredConfirmations,
		Status:                status,
	}, nil
}

// decodeProcessTxAssetCode validates and normalizes the asset code for blockchain transaction processing.
func decodeProcessTxAssetCode(field, value string) (currency.AssetCode, *appErrors.ErrorResponse) {
	assetCode := currency.NewAssetCode(strings.ToUpper(strings.TrimSpace(value)))
	if slices.Contains(_processTxAllowedAssetCodes, assetCode) {
		return assetCode, nil
	}

	return "", svcErrors.NewOneOfValidationError(
		"[ProcessTxHandler:Handle]",
		field,
		value,
		commonTools.StringValues(_processTxAllowedAssetCodes),
	)
}

// decodeProcessTxNetwork validates and normalizes the blockchain network for transaction processing.
func decodeProcessTxNetwork(field, value string) (domain.ChainNetwork, *appErrors.ErrorResponse) {
	network, err := domain.NewChainNetworkFromString(value)
	if err == nil && slices.Contains(_processTxAllowedNetworks, network) {
		return network, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[ProcessTxHandler:Handle]",
		field,
		value,
		commonTools.StringValues(_processTxAllowedNetworks),
	)
}

// decodeProcessTxType validates and normalizes the transaction direction for blockchain webhooks.
func decodeProcessTxType(field, value string) (domain.BlockchainTxType, *appErrors.ErrorResponse) {
	txType, err := domain.NewBlockchainTxTypeFromString(value)
	if err == nil && slices.Contains(_processTxAllowedTypes, txType) {
		return txType, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[ProcessTxHandler:Handle]",
		field,
		value,
		commonTools.StringValues(_processTxAllowedTypes),
	)
}

// decodeProcessTxStatus validates and normalizes the blockchain transaction status for webhooks.
func decodeProcessTxStatus(field, value string) (domain.BlockchainTxStatus, *appErrors.ErrorResponse) {
	status, err := domain.NewBlockchainTxStatusFromString(value)
	if err == nil && slices.Contains(_processTxAllowedStatuses, status) {
		return status, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[ProcessTxHandler:Handle]",
		field,
		value,
		commonTools.StringValues(_processTxAllowedStatuses),
	)
}
