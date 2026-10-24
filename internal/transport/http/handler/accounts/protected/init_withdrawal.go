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
	"github.com/whagency/core-common/services/user"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
	commonRules "github.com/whagency/core-common/validation"
)

type InitWithdrawalHandler struct {
	ctrl domain.WithdrawalController
}

// NewInitWithdrawalHandler creates a new instance of InitWithdrawalHandler.
func NewInitWithdrawalHandler(c domain.WithdrawalController) *InitWithdrawalHandler {
	return &InitWithdrawalHandler{
		ctrl: c,
	}
}

type initWithdrawalRequest struct {
	Asset     string          `json:"asset" validate:"required,not_blank" example:"USDT" enums:"USDT"`
	Network   string          `json:"network" validate:"required,not_blank" example:"trx" enums:"trx"`
	ToAddress string          `json:"to_address" validate:"required,not_blank" example:"TVCxeTNnWaCAt6fEoCz4WDsmZfTW86HtWd"`
	Amount    decimal.Decimal `json:"amount" validate:"required" example:"1200.056"`
	FeeTier   string          `json:"fee_tier" validate:"required,not_blank" example:"standard" enums:"slow,standard,fast"`
}

// Handle initializes a withdrawal request for a user.
//
//	@Summary        Initialize a withdrawal request
//	@Description    Initializes a withdrawal request for the authenticated user.
//	@Tags           accounts
//	@Accept         json
//	@Produce        json
//	@Param          request body initWithdrawalRequest true "Withdrawal initialization payload. Allowed values: asset=USDT, network=trx, fee_tier=slow|standard|fast."
//	@Success        200 {object} commonHTTP.Response "Withdrawal initialized successfully."
//	@Failure        400 {object} commonHTTP.Response "Missing or malformed request payload."
//	@Failure        401 {object} commonHTTP.Response "Authentication required."
//	@Failure        500 {object} commonHTTP.Response "Failed to initialize withdrawal due to internal errors."
//	@Router         /api/v1/accounts/withdrawals/init [post]
func (h InitWithdrawalHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[InitWithdrawalHandler:Handle]")
	if !ok {
		return
	}

	var req initWithdrawalRequest
	if !commonValidation.BindAndValidateJSON(c, httpValidation.Validator(), &req, "[InitWithdrawalHandler:Handle]") {
		return
	}

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		er := svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[InitWithdrawalHandler:Handle][err: amount must be greater than zero]")),
			appErrors.WithValidationEntry("amount", appErrors.ValidationTagGT, "0"),
		)
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	params, erDecode := req.toDomain(session)
	if erDecode != nil {
		c.AbortWithStatusJSON(erDecode.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, erDecode))
		return
	}

	if err := commonRules.ValidateCryptoAddress(params.Network.String(), req.ToAddress); err != nil {
		er := svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[InitWithdrawalHandler:Handle]%w", err)),
			appErrors.WithValidationEntry("to_address", appErrors.ValidationTagFormat, params.Network.String()),
		)
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	if err := h.ctrl.Init(ctx, params); commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, nil))
}

// toDomain converts the initWithdrawalRequest into domain.InitWithdrawalParams.
func (r initWithdrawalRequest) toDomain(session user.Session) (domain.InitWithdrawalParams, *appErrors.ErrorResponse) {
	assetCode, erAssetCode := assetCodeToDomain("[InitWithdrawalHandler:Handle]", "asset", r.Asset)
	if erAssetCode != nil {
		return domain.InitWithdrawalParams{}, erAssetCode
	}

	network, erNetwork := networkToDomain("[InitWithdrawalHandler:Handle]", "network", r.Network)
	if erNetwork != nil {
		return domain.InitWithdrawalParams{}, erNetwork
	}

	feeTier, erFeeTier := feeTierToDomain("[InitWithdrawalHandler:Handle]", "fee_tier", r.FeeTier)
	if erFeeTier != nil {
		return domain.InitWithdrawalParams{}, erFeeTier
	}

	return domain.InitWithdrawalParams{
		User:      session.User,
		AssetCode: assetCode,
		Network:   network,
		FeeTier:   feeTier,
		ToAddress: r.ToAddress,
		Amount:    r.Amount,
	}, nil
}
