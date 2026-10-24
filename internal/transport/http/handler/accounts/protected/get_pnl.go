package protected

import (
	"net/http"

	"account-service/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	commonHTTP "github.com/whagency/core-common/http"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
)

// GetPnlHandler provides an endpoint to get user's pnl.
type GetPnlHandler struct {
	ctrl domain.AccountController
}

// NewGetPnlHandler builds a new GetPnlHandler instance.
func NewGetPnlHandler(c domain.AccountController) *GetPnlHandler {
	return &GetPnlHandler{
		ctrl: c,
	}
}

// getPnlResponse defines the response structure for account pnl.
type getPnlResponse struct {
	PnlTotalAmount  decimal.Decimal             `json:"pnlTotalAmount" example:"340.76"`
	PnlTotalPercent string                      `json:"pnlTotalPercent" example:"12.5"`
	PnlAssets       map[string]assetPnlResponse `json:"pnlAssets"`
}

type assetPnlResponse struct {
	PnlAmount  decimal.Decimal `json:"pnlAmount" example:"1200.056"`
	PnlPercent string          `json:"pnlPercent" example:"12.5"`
}

// Handle get user's account pnl data.
//
//	@Summary        Get user's account pnl
//	@Description    Return the authenticated user's account pnl for all asset codes and total value for today.
//	@Tags           accounts
//	@Security       Authorization
//	@Accept         json
//	@Produce        json
//	@Success        200 {object} commonHTTP.Response{data=getPnlResponse} "User's account pnl."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Failed getting the account pnl due to internal errors."
//	@Router         /api/v1/accounts/get-pnl [get]
func (h GetPnlHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[SessionsHandler:Handle]")
	if !ok {
		return
	}

	result, err := h.ctrl.GetPnlByOwnerID(ctx, domain.NewNullableIDFromUint64(&session.User.ID))
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeGetPnlResponse(result)))
}

// encodeGetPnlResponse encodes the domain.GetPnlResult into a response format.
func encodeGetPnlResponse(res domain.GetPnlResult) getPnlResponse {
	encoded := getPnlResponse{
		PnlTotalAmount:  res.PnlTotalAmount,
		PnlTotalPercent: res.PnlTotalPercent,
		PnlAssets:       make(map[string]assetPnlResponse, len(res.PnlAssets)),
	}

	for assetCode, assetData := range res.PnlAssets {
		encoded.PnlAssets[assetCode.String()] = assetPnlResponse{
			PnlAmount:  assetData.PnlAmount,
			PnlPercent: assetData.PnlPercent,
		}
	}
	return encoded
}
