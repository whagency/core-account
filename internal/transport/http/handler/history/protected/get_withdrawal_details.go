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

// WithdrawalDetailsHandler returns withdrawal details for the authenticated user.
type WithdrawalDetailsHandler struct {
	ctrl domain.HistoryController
}

// NewWithdrawalDetailsHandler builds a new WithdrawalDetailsHandler instance.
func NewWithdrawalDetailsHandler(ctrl domain.HistoryController) *WithdrawalDetailsHandler {
	return &WithdrawalDetailsHandler{ctrl: ctrl}
}

type getWithdrawalDetailsURLParams struct {
	ID uint64 `uri:"id" validate:"required"`
}

type withdrawalDetailsResponse struct {
	ID                        uint64          `json:"id" example:"12"`
	Amount                    decimal.Decimal `json:"amount" example:"1200.084"`
	Fee                       decimal.Decimal `json:"fee" example:"1.012"`
	AssetCode                 string          `json:"assetCode" example:"USDT"`
	Network                   string          `json:"network" example:"trx"`
	Address                   string          `json:"address" example:"TVCxeTNnWaCAt6fEoCz4WDsmZfTW86HtWd"`
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

// Handle returns withdrawal details for the user.
//
//	@Summary        Withdrawal details
//	@Description    Retrieves withdrawal details for the authenticated user.
//	@Tags           history
//	@Security       Authorization
//	@Produce        json
//	@Param          id path string true "Withdrawal ID"
//	@Success        200 {object} commonHTTP.Response{data=withdrawalDetailsResponse} "Withdrawal details."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Unable to retrieve withdrawals due to internal issues."
//	@Router         /api/v1/history/withdrawals/{id} [get]
func (h WithdrawalDetailsHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[WithdrawalDetailsHandler:Handle]")
	if !ok {
		return
	}

	var params getWithdrawalDetailsURLParams
	if !commonValidation.BindAndValidateURI(c, httpValidation.Validator(), &params, "[WithdrawalDetailsHandler:Handle]") {
		return
	}

	result, err := h.ctrl.WithdrawalDetails(ctx, domain.NewIDFromUint64(session.User.ID), domain.NewIDFromUint64(params.ID))
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeWithdrawalDetailsResponse(result)))
}

// encodeWithdrawalDetailsResponse encodes domain.WithdrawalRequestWithTransactionDetails into a response format
func encodeWithdrawalDetailsResponse(result domain.WithdrawalRequestWithTransactionDetails) withdrawalDetailsResponse {
	blockchainTxConfirmedAt := ""
	if result.Transaction.ConfirmedAt != nil {
		blockchainTxConfirmedAt = result.Transaction.ConfirmedAt.Format(time.RFC3339)
	}
	return withdrawalDetailsResponse{
		ID:                        result.Withdrawal.ID.Uint64(),
		Amount:                    result.Withdrawal.Amount.Shift(-currency.DefaultAssetScale),
		Fee:                       result.Withdrawal.Fee.Shift(-currency.DefaultAssetScale),
		AssetCode:                 result.Withdrawal.AssetCode.String(),
		Network:                   result.Withdrawal.Network.String(),
		Address:                   result.Withdrawal.ToAddress,
		Status:                    result.Withdrawal.Status.String(),
		CreatedAt:                 result.Withdrawal.CreatedAt.Format(time.RFC3339),
		BlockchainTxID:            result.Transaction.TxID,
		BlockchainTxType:          result.Transaction.Type.String(),
		BlockchainTxFromAddress:   result.Transaction.FromAddress,
		BlockchainTxToAddress:     result.Transaction.ToAddress,
		BlockchainTxConfirmations: result.Transaction.Confirmations,
		BlockchainTxStatus:        result.Transaction.Status.String(),
		BlockchainTxConfirmedAt:   blockchainTxConfirmedAt,
	}
}
