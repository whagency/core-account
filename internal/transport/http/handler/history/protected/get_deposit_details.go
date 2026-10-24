package protected

import (
	"net/http"
	"time"

	"account-service/internal/domain"
	httpValidation "account-service/internal/transport/http/validation"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	commonHTTP "github.com/whagency/core-common/http"
	commonValidation "github.com/whagency/core-common/http/validation"
	"github.com/whagency/core-common/services/currency"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
)

// DepositDetailsHandler returns deposit details for the authenticated user.
type DepositDetailsHandler struct {
	ctrl domain.HistoryController
}

// NewDepositDetailsHandler builds a new DepositDetailsHandler instance.
func NewDepositDetailsHandler(ctrl domain.HistoryController) *DepositDetailsHandler {
	return &DepositDetailsHandler{ctrl: ctrl}
}

type getDepositDetailsURLParams struct {
	ID uint64 `uri:"id" validate:"required"`
}

type depositDetailsResponse struct {
	ID                        uint64          `json:"id" example:"12"`
	Amount                    decimal.Decimal `json:"amount" example:"1200.084"`
	Fee                       decimal.Decimal `json:"fee" example:"1.012"`
	AssetCode                 string          `json:"assetCode" example:"USDT"`
	Network                   string          `json:"network" example:"trx"`
	Status                    string          `json:"status" example:"success"`
	CreatedAt                 string          `json:"createdAt" example:"2026-03-02T06:31:43Z"`
	BlockchainTxID            *string         `json:"blockchainTxID" example:"dee1805a013369317c9df462376881e4a56679fc05a01cecec46e059e377820c"`
	BlockchainTxType          string          `json:"blockchainTxType" example:"inbound"`
	BlockchainTxFromAddress   string          `json:"blockchainTxFromAddress" example:"TVCxeTNnWaCAt6fEoCz4WDsmZfTW86HtWd"`
	BlockchainTxToAddress     string          `json:"blockchainTxToAddress" example:"TCtvPqALz7cSTguGMTVeqhFBXevthJzUSt"`
	BlockchainTxConfirmations uint8           `json:"blockchainTxConfirmations" example:"17"`
	BlockchainTxStatus        string          `json:"blockchainTxStatus" example:"confirmed"`
	BlockchainTxConfirmedAt   string          `json:"blockchainTxConfirmedAt" example:"2026-03-02T06:31:43Z"`
}

// Handle returns deposit details for the user.
//
//	@Summary        Deposit details
//	@Description    Retrieves deposit details for the authenticated user.
//	@Tags           history
//	@Security       Authorization
//	@Produce        json
//	@Param          id path string true "Deposit ID"
//	@Success        200 {object} commonHTTP.Response{data=depositDetailsResponse} "Deposit details."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Unable to retrieve deposits due to internal issues."
//	@Router         /api/v1/history/deposits/{id} [get]
func (h DepositDetailsHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[DepositDetailsHandler:Handle]")
	if !ok {
		return
	}

	var params getDepositDetailsURLParams
	if !commonValidation.BindAndValidateURI(c, httpValidation.Validator(), &params, "[DepositDetailsHandler:Handle]") {
		return
	}

	result, err := h.ctrl.DepositDetails(ctx, domain.NewIDFromUint64(session.User.ID), domain.NewIDFromUint64(params.ID))
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeDepositDetailsResponse(result)))
}

// encodeDepositDetailsResponse encodes domain.DepositRequestWithTransactionDetails into a response format
func encodeDepositDetailsResponse(result domain.DepositRequestWithTransactionDetails) depositDetailsResponse {
	blockchainTxConfirmedAt := ""
	if result.Transaction.ConfirmedAt != nil {
		blockchainTxConfirmedAt = result.Transaction.ConfirmedAt.Format(time.RFC3339)
	}
	return depositDetailsResponse{
		ID:                        result.Deposit.ID.Uint64(),
		Amount:                    result.Deposit.Amount.Shift(-currency.DefaultAssetScale),
		Fee:                       result.Deposit.Fee.Shift(-currency.DefaultAssetScale),
		AssetCode:                 result.Deposit.AssetCode.String(),
		Network:                   result.Deposit.Network.String(),
		Status:                    result.Deposit.Status.String(),
		CreatedAt:                 result.Deposit.CreatedAt.Format(time.RFC3339),
		BlockchainTxID:            result.Transaction.TxID,
		BlockchainTxType:          result.Transaction.Type.String(),
		BlockchainTxFromAddress:   result.Transaction.FromAddress,
		BlockchainTxToAddress:     result.Transaction.ToAddress,
		BlockchainTxConfirmations: result.Transaction.Confirmations,
		BlockchainTxStatus:        result.Transaction.Status.String(),
		BlockchainTxConfirmedAt:   blockchainTxConfirmedAt,
	}
}
