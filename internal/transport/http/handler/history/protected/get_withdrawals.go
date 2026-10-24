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

// WithdrawalListHandler returns a withdrawal list for the authenticated user.
type WithdrawalListHandler struct {
	ctrl domain.HistoryController
}

// NewWithdrawalListHandler builds a new WithdrawalListHandler instance.
func NewWithdrawalListHandler(ctrl domain.HistoryController) *WithdrawalListHandler {
	return &WithdrawalListHandler{ctrl: ctrl}
}

type withdrawalListRequestQueryParams struct {
	Page int `form:"page" default:"1"`
	// PageSize is capped at 100; a larger value is clamped, not rejected.
	PageSize  int     `form:"per-page" default:"10"`
	AssetCode *string `form:"asset" example:"USDT" validate:"omitempty,not_blank,min=2,max=8" enums:"USDT"`
	Status    *string `form:"status" validate:"omitempty,not_blank" example:"new" enums:"new,pending,success,failed,cancelled,blocked,rejected,manual_review"`
}

// _withdrawalListAllowedStatuses defines the supported status filters for withdrawal history.
var _withdrawalListAllowedStatuses = []domain.WithdrawalStatus{
	domain.WithdrawalStatusNew,
	domain.WithdrawalStatusPending,
	domain.WithdrawalStatusSuccess,
	domain.WithdrawalStatusFailed,
	domain.WithdrawalStatusCancelled,
	domain.WithdrawalStatusBlocked,
	domain.WithdrawalStatusRejected,
	domain.WithdrawalStatusManualReview,
}

type withdrawalListItem struct {
	ID        uint64          `json:"id" example:"34"`
	Amount    decimal.Decimal `json:"amount" example:"1200.075"`
	AssetCode string          `json:"assetCode" example:"USDT"`
	Network   string          `json:"network" example:"trx"`
	Address   string          `json:"address" example:"TVCxeTNnWaCAt6fEoCz4WDsmZfTW86HtWd"`
	Status    string          `json:"status" example:"success"`
	CreatedAt string          `json:"createdAt" example:"2026-03-02T06:31:43Z"`
}

type withdrawalListResponse struct {
	Withdrawals []withdrawalListItem  `json:"items"`
	Pagination  commonHTTP.Pagination `json:"pagination"`
}

// Handle returns a withdrawal list for the user.
//
//	@Summary        Withdrawal list
//	@Description    Retrieves a withdrawal list for the authenticated user.
//	@Tags           history
//	@Security       Authorization
//	@Produce        json
//	@Param          request query withdrawalListRequestQueryParams false "Filter query params. Allowed values: asset=USDT; status=new|pending|success|failed|cancelled|blocked|rejected|manual_review."
//	@Success        200 {object} commonHTTP.Response{data=withdrawalListResponse} "Withdrawal list."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Unable to retrieve withdrawals due to internal issues."
//	@Router         /api/v1/history/withdrawals [get]
func (h WithdrawalListHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[WithdrawalListHandler:Handle]")
	if !ok {
		return
	}

	var params withdrawalListRequestQueryParams
	if !commonValidation.BindAndValidateQuery(c, httpValidation.Validator(), &params, "[WithdrawalListHandler:Handle]") {
		return
	}

	filter, er := params.toWithdrawalFilter()
	if er != nil {
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	result, err := h.ctrl.WithdrawalList(ctx, domain.NewIDFromUint64(session.User.ID), filter)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeWithdrawalListResponse(result)))
}

// toWithdrawalFilter converts request query params to domain.WithdrawalFilter.
func (r withdrawalListRequestQueryParams) toWithdrawalFilter() (domain.WithdrawalFilter, *appErrors.ErrorResponse) {
	// Clamps both values: an uncapped per-page lets any authenticated caller ask
	// for the whole table, and an uncapped page overflows the offset.
	limit, offset := pagination.Normalize(r.Page, r.PageSize)

	var withdrawalStatus *domain.WithdrawalStatus
	if r.Status != nil {
		status, erStatus := decodeWithdrawalListStatus(*r.Status)
		if erStatus != nil {
			return domain.WithdrawalFilter{}, erStatus
		}

		withdrawalStatus = &status
	}

	assetCode, erAssetCode := decodeOptionalHistoryAssetCode("[WithdrawalListHandler:Handle]", "asset", r.AssetCode)
	if erAssetCode != nil {
		return domain.WithdrawalFilter{}, erAssetCode
	}

	return domain.WithdrawalFilter{
		Limit:     limit,
		Offset:    offset,
		AssetCode: assetCode,
		Status:    withdrawalStatus,
	}, nil
}

// decodeWithdrawalListStatus validates and normalizes the withdrawal status filter.
func decodeWithdrawalListStatus(value string) (domain.WithdrawalStatus, *appErrors.ErrorResponse) {
	status, err := domain.NewWithdrawalStatusFromString(value)
	if err == nil && slices.Contains(_withdrawalListAllowedStatuses, status) {
		return status, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[WithdrawalListHandler:Handle]",
		"status",
		value,
		commonTools.StringValues(_withdrawalListAllowedStatuses),
	)
}

// encodeWithdrawalListResponse encodes domain.WithdrawalRequestListResult into a response format
func encodeWithdrawalListResponse(result domain.WithdrawalRequestListResult) withdrawalListResponse {
	items := make([]withdrawalListItem, 0, len(result.List))
	for _, t := range result.List {
		items = append(items, withdrawalListItem{
			ID:        t.ID.Uint64(),
			Amount:    t.Amount.Shift(-currency.DefaultAssetScale),
			AssetCode: t.AssetCode.String(),
			Network:   t.Network.String(),
			Address:   t.ToAddress,
			Status:    t.Status.String(),
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		})
	}

	return withdrawalListResponse{Withdrawals: items, Pagination: commonHTTP.EncodePaginationResponse(result.Pagination)}
}
