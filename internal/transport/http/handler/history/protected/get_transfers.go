package protected

import (
	"net/http"
	"time"

	"account-service/internal/domain"
	httpValidation "account-service/internal/transport/http/validation"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
	commonValidation "github.com/whagency/core-common/http/validation"
	"github.com/whagency/core-common/pagination"
	"github.com/whagency/core-common/services/currency"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
)

// TransferListHandler returns a transfer list for the authenticated user.
type TransferListHandler struct {
	ctrl domain.HistoryController
}

// NewTransferListHandler builds a new TransferListHandler instance.
func NewTransferListHandler(ctrl domain.HistoryController) *TransferListHandler {
	return &TransferListHandler{ctrl: ctrl}
}

type transferListRequestQueryParams struct {
	Page int `form:"page" default:"1"`
	// PageSize is capped at 100; a larger value is clamped, not rejected.
	PageSize  int     `form:"per-page" default:"10"`
	AssetCode *string `form:"asset" example:"USDT" validate:"omitempty,not_blank,min=2,max=8" enums:"USDT"`
}

type transferListItem struct {
	ID        uint64          `json:"id" example:"34"`
	Amount    decimal.Decimal `json:"amount" example:"1200.075"`
	AssetCode string          `json:"assetCode" example:"USDT"`
	Incoming  bool            `json:"incoming" example:"true"`
	CreatedAt string          `json:"createdAt" example:"2026-03-02T06:31:43Z"`
}

type transferListResponse struct {
	Transfers  []transferListItem    `json:"items"`
	Pagination commonHTTP.Pagination `json:"pagination"`
}

// Handle returns a transfer list for the user.
//
//	@Summary        Transfer list
//	@Description    Retrieves a transfer list for the authenticated user.
//	@Tags           history
//	@Security       Authorization
//	@Produce        json
//	@Param          request query transferListRequestQueryParams false "Filter query params. Allowed values: asset=USDT."
//	@Success        200 {object} commonHTTP.Response{data=transferListResponse} "Transfer list."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Unable to retrieve transfers due to internal issues."
//	@Router         /api/v1/history/transfers [get]
func (h TransferListHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[TransferListHandler:Handle]")
	if !ok {
		return
	}

	var params transferListRequestQueryParams
	if !commonValidation.BindAndValidateQuery(c, httpValidation.Validator(), &params, "[TransferListHandler:Handle]") {
		return
	}

	filter, er := params.toDomain()
	if er != nil {
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	result, err := h.ctrl.TransferList(ctx, domain.NewIDFromUint64(session.User.ID), filter)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeTransferListResponse(result, domain.NewIDFromUint64(session.User.ID))))
}

// toDomain converts transferListRequestURLParams to domain.TransferFilter.
func (r *transferListRequestQueryParams) toDomain() (domain.TransferFilter, *appErrors.ErrorResponse) {
	// Clamps both values: an uncapped per-page lets any authenticated caller ask
	// for the whole table, and an uncapped page overflows the offset.
	limit, offset := pagination.Normalize(r.Page, r.PageSize)

	assetCode, erAssetCode := decodeOptionalHistoryAssetCode("[TransferListHandler:Handle]", "asset", r.AssetCode)
	if erAssetCode != nil {
		return domain.TransferFilter{}, erAssetCode
	}

	return domain.TransferFilter{
		Limit:     limit,
		Offset:    offset,
		AssetCode: assetCode,
	}, nil
}

// encodeTransferListResponse encodes domain.TransferRequestListResult into a response format
func encodeTransferListResponse(result domain.TransferRequestListResult, userID domain.ID) transferListResponse {
	items := make([]transferListItem, 0, len(result.List))
	for _, t := range result.List {
		items = append(items, transferListItem{
			ID:        t.ID.Uint64(),
			Amount:    t.Amount.Shift(-currency.DefaultAssetScale),
			AssetCode: t.AssetCode.String(),
			Incoming:  t.UserToID == userID,
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		})
	}

	return transferListResponse{Transfers: items, Pagination: commonHTTP.EncodePaginationResponse(result.Pagination)}
}
