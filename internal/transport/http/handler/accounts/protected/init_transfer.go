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
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
)

// TransferHandler handles transfer requests between users.
type TransferHandler struct {
	ctrl domain.TransferController
}

// NewTransferHandler creates a new TransferHandler instance.
func NewTransferHandler(ctrl domain.TransferController) *TransferHandler {
	return &TransferHandler{ctrl: ctrl}
}

type transferRequest struct {
	Asset        string          `json:"asset" validate:"required,not_blank" example:"USDT" enums:"USDT"`
	ToUserNumber uint64          `json:"toUserNumber" validate:"required" example:"148728641822"`
	Amount       decimal.Decimal `json:"amount" validate:"required" example:"1200.056"`
}

type transferResponse struct {
	JournalID uint64 `json:"journalId" example:"115"`
	Status    string `json:"status" example:"completed"`
}

// Handle processes a transfer between users.
//
//	@Summary        Transfer funds between users
//	@Description    Performs an internal transfer between users.
//	@Tags           accounts
//	@Accept         json
//	@Produce        json
//	@Param          request body transferRequest true "Transfer payload. Allowed values: asset=USDT."
//	@Success        200 {object} commonHTTP.Response{data=transferResponse} "Transfer completed successfully."
//	@Failure        400 {object} commonHTTP.Response "Missing or malformed request payload."
//	@Failure        401 {object} commonHTTP.Response "Authentication required."
//	@Failure        500 {object} commonHTTP.Response "Failed to process transfer due to internal errors."
//	@Router         /api/v1/accounts/transfers/init [post]
func (h TransferHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[TransferHandler:Handle]")
	if !ok {
		return
	}

	var req transferRequest
	if !commonValidation.BindAndValidateJSON(c, httpValidation.Validator(), &req, "[TransferHandler:Handle]") {
		return
	}

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		er := svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[TransferHandler:Handle][err: amount must be greater than zero]")),
			appErrors.WithValidationEntry("amount", appErrors.ValidationTagGT, "0"),
		)
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	if int(currency.DefaultAssetScale) < int(req.Amount.Exponent())*-1 {
		er := svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[TransferHandler:Handle][err: maximum number of fractional digits must not exceed %d]", currency.DefaultAssetScale)),
			appErrors.WithValidationEntry(
				"amount",
				appErrors.ValidationTagMaxFractionalDigits,
				fmt.Sprintf("%d", currency.DefaultAssetScale),
			),
		)
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	assetCode, erAssetCode := assetCodeToDomain("[TransferHandler:Handle]", "asset", req.Asset)
	if erAssetCode != nil {
		c.AbortWithStatusJSON(erAssetCode.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, erAssetCode))
		return
	}

	result, err := h.ctrl.Init(ctx, domain.TransferParams{
		User:         session.User,
		ToUserNumber: domain.NewIDFromUint64(req.ToUserNumber),
		AssetCode:    assetCode,
		Amount:       req.Amount,
	})
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, transferResponse{
		JournalID: result.JournalID.Uint64(),
		Status:    result.Status.String(),
	}))
}
