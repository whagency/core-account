package protected

import (
	"net/http"
	"slices"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"
	httpValidation "account-service/internal/transport/http/validation"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
	commonValidation "github.com/whagency/core-common/http/validation"
	"github.com/whagency/core-common/pagination"
	"github.com/whagency/core-common/services/currency"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
	commonTools "github.com/whagency/core-common/tools"
)

// DepositListHandler returns a deposit list for the authenticated user.
type DepositListHandler struct {
	ctrl domain.HistoryController
}

// NewDepositListHandler builds a new DepositListHandler instance.
func NewDepositListHandler(ctrl domain.HistoryController) *DepositListHandler {
	return &DepositListHandler{ctrl: ctrl}
}

type depositListRequestQueryParams struct {
	Page int `form:"page" default:"1"`
	// PageSize is capped at 100; a larger value is clamped, not rejected.
	PageSize  int     `form:"per-page" default:"10"`
	AssetCode *string `form:"asset" example:"USDT" validate:"omitempty,not_blank,min=2,max=8" enums:"USDT"`
	Status    *string `form:"status" validate:"omitempty,not_blank" example:"new" enums:"new,pending,success,failed,blocked,manual_review"`
}

// _depositListAllowedStatuses defines the supported status filters for deposit history.
var _depositListAllowedStatuses = []domain.DepositStatus{
	domain.DepositStatusNew,
	domain.DepositStatusPending,
	domain.DepositStatusSuccess,
	domain.DepositStatusFailed,
	domain.DepositStatusBlocked,
	domain.DepositStatusManualReview,
}

type depositListItem struct {
	ID        uint64          `json:"id" example:"34"`
	Amount    decimal.Decimal `json:"amount" example:"1200.075"`
	AssetCode string          `json:"assetCode" example:"USDT"`
	Network   string          `json:"network" example:"trx"`
	Address   string          `json:"address" example:"TVCxeTNnWaCAt6fEoCz4WDsmZfTW86HtWd"`
	Status    string          `json:"status" example:"success"`
	CreatedAt string          `json:"createdAt" example:"2026-03-02T06:31:43Z"`
}

type depositListResponse struct {
	Deposits   []depositListItem     `json:"items"`
	Pagination commonHTTP.Pagination `json:"pagination"`
}

// Handle returns a deposit list for the user.
//
//	@Summary        Deposit list
//	@Description    Retrieves a deposit list for the authenticated user.
//	@Tags           history
//	@Security       Authorization
//	@Produce        json
//	@Param          request query depositListRequestQueryParams false "Filter query params. Allowed values: asset=USDT; status=new|pending|success|failed|blocked|manual_review."
//	@Success        200 {object} commonHTTP.Response{data=depositListResponse} "Deposit list."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Unable to retrieve deposits due to internal issues."
//	@Router         /api/v1/history/deposits [get]
func (h DepositListHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[DepositListHandler:Handle]")
	if !ok {
		return
	}

	var params depositListRequestQueryParams
	if !commonValidation.BindAndValidateQuery(c, httpValidation.Validator(), &params, "[DepositListHandler:Handle]") {
		return
	}

	filter, er := params.toDepositFilter()
	if er != nil {
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	result, err := h.ctrl.DepositList(ctx, domain.NewIDFromUint64(session.User.ID), filter)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeDepositListResponse(result)))
}

// toDepositFilter converts request query params to domain.DepositFilter.
func (r depositListRequestQueryParams) toDepositFilter() (domain.DepositFilter, *appErrors.ErrorResponse) {
	// Clamps both values: an uncapped per-page lets any authenticated caller ask
	// for the whole table, and an uncapped page overflows the offset.
	limit, offset := pagination.Normalize(r.Page, r.PageSize)

	var depositStatus *domain.DepositStatus
	if r.Status != nil {
		status, erStatus := decodeDepositListStatus(*r.Status)
		if erStatus != nil {
			return domain.DepositFilter{}, erStatus
		}

		depositStatus = &status
	}

	assetCode, erAssetCode := decodeOptionalHistoryAssetCode("[DepositListHandler:Handle]", "asset", r.AssetCode)
	if erAssetCode != nil {
		return domain.DepositFilter{}, erAssetCode
	}

	return domain.DepositFilter{
		Limit:     limit,
		Offset:    offset,
		AssetCode: assetCode,
		Status:    depositStatus,
	}, nil
}

// decodeDepositListStatus validates and normalizes the deposit status filter.
func decodeDepositListStatus(value string) (domain.DepositStatus, *appErrors.ErrorResponse) {
	status, err := domain.NewDepositStatusFromString(value)
	if err == nil && slices.Contains(_depositListAllowedStatuses, status) {
		return status, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[DepositListHandler:Handle]",
		"status",
		value,
		commonTools.StringValues(_depositListAllowedStatuses),
	)
}

// encodeDepositListResponse encodes domain.DepositRequestListResult into a response format
func encodeDepositListResponse(result domain.DepositRequestListResult) depositListResponse {
	items := make([]depositListItem, 0, len(result.List))
	for _, t := range result.List {
		if t.Status == domain.DepositStatusManualReview {
			t.Status = domain.DepositStatusPending
		}
		items = append(items, depositListItem{
			ID:        t.ID.Uint64(),
			Amount:    t.Amount.Shift(-currency.DefaultAssetScale),
			AssetCode: t.AssetCode.String(),
			Network:   t.Network.String(),
			Address:   t.Address,
			Status:    t.Status.String(),
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		})
	}

	return depositListResponse{Deposits: items, Pagination: commonHTTP.EncodePaginationResponse(result.Pagination)}
}
