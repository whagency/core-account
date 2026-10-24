package protected

import (
	"net/http"

	"account-service/internal/domain"
	httpValidation "account-service/internal/transport/http/validation"

	"github.com/gin-gonic/gin"
	appErrors "github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
	commonValidation "github.com/whagency/core-common/http/validation"
	"github.com/whagency/core-common/services/user"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
)

type InitDepositHandler struct {
	ctrl domain.DepositController
}

// NewInitDepositHandler creates a new instance of NewInitDepositHandler.
func NewInitDepositHandler(c domain.DepositController) *InitDepositHandler {
	return &InitDepositHandler{
		ctrl: c,
	}
}

// initDepositRequest represents the request body for deposit address initialization
type initDepositRequest struct {
	Asset   string `json:"asset" validate:"required,not_blank" example:"USDT" enums:"USDT"`
	Network string `json:"network" validate:"required,not_blank" example:"trx" enums:"trx"`
}

// initDepositResponse represents the response for deposit address initialization
type initDepositResponse struct {
	Address          string `json:"address" example:"TVCxeTNnWaCAt6fEoCz4WDsmZfTW86HtWd"`
	MinConfirmations uint8  `json:"min_confirmations" example:"17"`
}

// Handle processes the deposit address initialization request.
//
//	@Summary        Initialize a deposit address
//	@Description    Initializes a deposit address for the authenticated user.
//	@Tags           accounts
//	@Accept         json
//	@Produce        json
//	@Param          request body initDepositRequest true "Deposit address initialization payload. Allowed values: asset=USDT, network=trx."
//	@Success        200 {object} commonHTTP.Response{data=initDepositResponse} "Deposit address initialized successfully."
//	@Failure        400 {object} commonHTTP.Response "Malformed payload or unsupported asset/network."
//	@Failure        401 {object} commonHTTP.Response "Authentication required."
//	@Failure        500 {object} commonHTTP.Response "Failed to allocate address due to internal errors."
//	@Router         /api/v1/accounts/deposits/init [post]
func (h InitDepositHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[InitDepositHandler:Handle]")
	if !ok {
		return
	}

	var req initDepositRequest
	if !commonValidation.BindAndValidateJSON(c, httpValidation.Validator(), &req, "[InitDepositHandler:Handle]") {
		return
	}

	params, er := req.toDomain(session)
	if er != nil {
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	result, err := h.ctrl.Init(ctx, params)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeInitDepositResponse(result)))
}

// toDomain converts initDepositRequest to domain.
func (r initDepositRequest) toDomain(session user.Session) (domain.InitDepositParams, *appErrors.ErrorResponse) {
	assetCode, erAssetCode := assetCodeToDomain("[InitDepositHandler:Handle]", "asset", r.Asset)
	if erAssetCode != nil {
		return domain.InitDepositParams{}, erAssetCode
	}

	network, erNetwork := networkToDomain("[InitDepositHandler:Handle]", "network", r.Network)
	if erNetwork != nil {
		return domain.InitDepositParams{}, erNetwork
	}

	return domain.InitDepositParams{
		User:      session.User,
		Network:   network,
		AssetCode: assetCode,
	}, nil
}

// encodeInitDepositResponse encodes the domain.InitDepositResult into a response format.
func encodeInitDepositResponse(result domain.InitDepositResult) initDepositResponse {
	return initDepositResponse{
		Address:          result.AddressAssignment.Address,
		MinConfirmations: result.ChainConfig.MinConfirmations,
	}
}
