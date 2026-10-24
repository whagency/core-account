package protected

import (
	"fmt"
	"net/http"

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

// TopUpUserAccountHandler provides a local/dev-only internal faucet for funding a user balance.
type TopUpUserAccountHandler struct {
	ctrl domain.DevController
}

// NewTopUpUserAccountHandler creates a new TopUpUserAccountHandler instance.
func NewTopUpUserAccountHandler(ctrl domain.DevController) *TopUpUserAccountHandler {
	return &TopUpUserAccountHandler{ctrl: ctrl}
}

type topUpUserAccountRequest struct {
	UserID         uint64          `json:"userId" validate:"required" example:"1"`
	Asset          string          `json:"asset" validate:"required,not_blank" example:"USDT" enums:"USDT"`
	Amount         decimal.Decimal `json:"amount" validate:"required" example:"100.00"`
	IdempotencyKey *string         `json:"idempotencyKey,omitempty" example:"dev-topup-user-1-usdt-100"`
	Description    *string         `json:"description,omitempty" example:"Local funding for checkout test"`
}

type topUpUserAccountResponse struct {
	JournalID uint64 `json:"journalId" example:"115"`
	Status    string `json:"status" example:"completed"`
}

// Handle funds a user's balance through the regular deposit journal type.
//
//	@Summary        Local dev balance top-up
//	@Description    Credits a user's balance through the regular deposit journal type for local/dev testing scenarios.
//	@Tags           accounts
//	@Accept         json
//	@Produce        json
//	@Param          X-API-KEY header string true "Internal security token"
//	@Param          request body topUpUserAccountRequest true "Local/dev-only balance top-up payload. Allowed values: asset=USDT."
//	@Success        200 {object} commonHTTP.Response{data=topUpUserAccountResponse} "Balance funded successfully."
//	@Failure        400 {object} commonHTTP.Response "Missing or malformed request payload."
//	@Failure        403 {object} commonHTTP.Response "Missing or invalid internal security token."
//	@Failure        500 {object} commonHTTP.Response "Failed to fund balance due to internal errors."
//	@Router         /api/v1/internal/dev/accounts/topup [post]
func (h TopUpUserAccountHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	var req topUpUserAccountRequest
	if !commonValidation.BindAndValidateJSON(c, httpValidation.Validator(), &req, "[TopUpUserAccountHandler:Handle]") {
		return
	}

	if int(currency.DefaultAssetScale) < int(req.Amount.Exponent())*-1 {
		er := svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[TopUpUserAccountHandler:Handle][err: maximum number of fractional digits must not exceed %d]", currency.DefaultAssetScale)),
			appErrors.WithValidationEntry(
				"amount",
				appErrors.ValidationTagMaxFractionalDigits,
				fmt.Sprintf("%d", currency.DefaultAssetScale),
			),
		)
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	assetCode, erAssetCode := assetCodeToDomain("[TopUpUserAccountHandler:Handle]", "asset", req.Asset)
	if erAssetCode != nil {
		c.AbortWithStatusJSON(erAssetCode.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, erAssetCode))
		return
	}

	result, err := h.ctrl.TopUpUserAccount(ctx, domain.TopUpUserAccountParams{
		UserID:         domain.NewIDFromUint64(req.UserID),
		AssetCode:      assetCode,
		Amount:         req.Amount,
		IdempotencyKey: commonTools.TrimmedOrNil(req.IdempotencyKey),
		Description:    commonTools.TrimmedOrNil(req.Description),
	})
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, topUpUserAccountResponse{
		JournalID: result.JournalID.Uint64(),
		Status:    result.Status.String(),
	}))
}
