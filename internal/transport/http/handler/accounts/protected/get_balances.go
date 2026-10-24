package protected

import (
	"net/http"

	"account-service/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/services/currency"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
)

// GetBalancesHandler provides an endpoint to get user's account balances.
type GetBalancesHandler struct {
	ctrl domain.AccountController
}

// NewGetBalancesHandler builds a new GetBalancesHandler instance.
func NewGetBalancesHandler(c domain.AccountController) *GetBalancesHandler {
	return &GetBalancesHandler{
		ctrl: c,
	}
}

// getBalancesResponse defines the response structure for account balances.
type getBalancesResponse map[string]map[string]decimal.Decimal

// Handle get user's account balances data.
//
//	@Summary        Get user's account balances
//	@Description    Return the authenticated user's account balances for all asset codes and account types.
//	@Tags           accounts
//	@Security       Authorization
//	@Accept         json
//	@Produce        json
//	@Success        200 {object} commonHTTP.Response{data=getBalancesResponse} "User's account balances."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Failed getting the account balances due to internal errors."
//	@Router         /api/v1/accounts/get-balances [get]
func (h GetBalancesHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[SessionsHandler:Handle]")
	if !ok {
		return
	}

	result, err := h.ctrl.GetBalancesByOwnerID(ctx, domain.NewNullableIDFromUint64(&session.User.ID))
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeGetBalancesResponse(result)))
}

// encodeGetBalancesResponse encodes the domain.GetBalancesResult into a response format.
func encodeGetBalancesResponse(res domain.GetBalancesResult) getBalancesResponse {
	encoded := make(getBalancesResponse)
	for assetCode, acctTypes := range res {
		assetCodeStr := assetCode.String()
		encoded[assetCodeStr] = make(map[string]decimal.Decimal)
		for acctType, balance := range acctTypes {
			acctTypeStr := acctType.String()
			encoded[assetCodeStr][acctTypeStr] = balance.Shift(-currency.DefaultAssetScale)
		}
	}
	return encoded
}
