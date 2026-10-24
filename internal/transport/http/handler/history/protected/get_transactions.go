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

// TransactionListHandler returns a transaction list for the authenticated user.
type TransactionListHandler struct {
	ctrl domain.HistoryController
}

// NewTransactionListHandler builds a new TransactionListHandler instance.
func NewTransactionListHandler(ctrl domain.HistoryController) *TransactionListHandler {
	return &TransactionListHandler{ctrl: ctrl}
}

type transactionListRequestURLParams struct {
	Page int `form:"page" default:"1"`
	// PageSize is capped at 100; a larger value is clamped, not rejected.
	PageSize    int     `form:"per-page" default:"10"`
	AssetCode   *string `form:"asset" validate:"omitempty,not_blank,min=2,max=8" example:"USDT" enums:"USDT"`
	Status      *string `form:"status" validate:"omitempty,not_blank" example:"pending" enums:"pending,completed,failed,cancelled,reversed"`
	Type        *string `form:"type" validate:"omitempty,not_blank" example:"credit" enums:"credit,debit,fee"`
	EntryType   *string `form:"entryType" validate:"omitempty,not_blank" example:"deposit" enums:"deposit,withdrawal_request,withdrawal_complete,withdrawal_reject,withdrawal_cancel,transfer,checkout_payment,checkout_payment_hold,checkout_payment_hold_release"`
	AccountType *string `form:"accountType" validate:"omitempty,not_blank" example:"liability" enums:"liability,hold,revenue,expense"`
}

// _transactionListAllowedStatuses defines the supported status filters for transaction history.
var _transactionListAllowedStatuses = []domain.JournalStatus{
	domain.JournalStatusPending,
	domain.JournalStatusCompleted,
	domain.JournalStatusFailed,
	domain.JournalStatusCancelled,
	domain.JournalStatusReversed,
}

// _transactionListAllowedTypes defines the supported transaction type filters for transaction history.
var _transactionListAllowedTypes = []domain.TransactionType{
	domain.TransactionTypeCredit,
	domain.TransactionTypeDebit,
	domain.TransactionTypeFee,
}

// _transactionListAllowedEntryTypes defines the supported business entry filters for transaction history.
var _transactionListAllowedEntryTypes = []domain.JournalBizType{
	domain.JournalBizTypeDeposit,
	domain.JournalBizTypeWithdrawalRequest,
	domain.JournalBizTypeWithdrawalComplete,
	domain.JournalBizTypeWithdrawalReject,
	domain.JournalBizTypeWithdrawalCancel,
	domain.JournalBizTypeTransfer,
	domain.JournalBizTypeCheckoutPayment,
	domain.JournalBizTypeCheckoutPaymentHold,
	domain.JournalBizTypeCheckoutPaymentHoldRelease,
}

// _transactionListAllowedAccountTypes defines the supported account type filters for transaction history.
var _transactionListAllowedAccountTypes = []domain.AccountType{
	domain.AccountTypeLiability,
	domain.AccountTypeHold,
	domain.AccountTypeRevenue,
	domain.AccountTypeExpense,
}

type transactionListItem struct {
	ID        uint64          `json:"id" example:"120"`
	Amount    decimal.Decimal `json:"amount" example:"1200.056"`
	AssetCode string          `json:"assetCode" example:"USDT"`
	Type      string          `json:"type" example:"completed"`
	Status    string          `json:"status" example:"debit"`
	BizType   string          `json:"entryType" example:"withdrawal_request"`
	AcctType  string          `json:"accountType" example:"liability"`
	CreatedAt string          `json:"createdAt" example:"2026-03-02T06:31:43Z"`
}

type transactionListResponse struct {
	Transactions []transactionListItem `json:"items"`
	Pagination   commonHTTP.Pagination `json:"pagination"`
}

// Handle returns a transaction list for the user.
//
//	@Summary        Transaction list
//	@Description    Retrieves a transaction list for the authenticated user.
//	@Tags           history
//	@Security       Authorization
//	@Produce        json
//	@Param          request query transactionListRequestURLParams false "Filter query params. Allowed values: asset=USDT; status=pending|completed|failed|cancelled|reversed; type=credit|debit|fee; entryType=deposit|withdrawal_request|withdrawal_complete|withdrawal_reject|withdrawal_cancel|transfer|checkout_payment|checkout_payment_hold|checkout_payment_hold_release; accountType=liability|hold|revenue|expense."
//	@Success        200 {object} commonHTTP.Response{data=transactionListResponse} "Transaction list."
//	@Failure        401 {object} commonHTTP.Response "Missing or invalid access token."
//	@Failure        500 {object} commonHTTP.Response "Unable to retrieve transactions due to internal issues."
//	@Router         /api/v1/history/transactions [get]
func (h TransactionListHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	session, ok := userMiddlewares.RequireAuthenticatedSession(c, "[TransactionListHandler:Handle]")
	if !ok {
		return
	}

	var params transactionListRequestURLParams
	if !commonValidation.BindAndValidateQuery(c, httpValidation.Validator(), &params, "[TransactionListHandler:Handle]") {
		return
	}

	filter, er := params.toTransactionFilter()
	if er != nil {
		c.AbortWithStatusJSON(er.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, er))
		return
	}

	result, err := h.ctrl.TransactionList(ctx, domain.NewIDFromUint64(session.User.ID), filter)
	if commonHTTP.AbortIfError(c, ctx, err) {
		return
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, encodeTransactionListResponse(result)))
}

// toTransactionFilter converts request query params to domain.TransactionFilter.
func (r transactionListRequestURLParams) toTransactionFilter() (domain.TransactionFilter, *appErrors.ErrorResponse) {
	// Clamps both values: an uncapped per-page lets any authenticated caller ask
	// for the whole table, and an uncapped page overflows the offset.
	limit, offset := pagination.Normalize(r.Page, r.PageSize)

	var transactionStatus *domain.JournalStatus
	if r.Status != nil {
		status, erStatus := decodeTransactionListStatus(*r.Status)
		if erStatus != nil {
			return domain.TransactionFilter{}, erStatus
		}

		transactionStatus = &status
	}

	var transactionType *domain.TransactionType
	if r.Type != nil {
		txType, erType := decodeTransactionListType(*r.Type)
		if erType != nil {
			return domain.TransactionFilter{}, erType
		}

		transactionType = &txType
	}

	var entryType *domain.JournalBizType
	if r.EntryType != nil {
		bizType, erEntryType := decodeTransactionListEntryType(*r.EntryType)
		if erEntryType != nil {
			return domain.TransactionFilter{}, erEntryType
		}

		entryType = &bizType
	}

	var accountType *domain.AccountType
	if r.AccountType != nil {
		parsedAccountType, erAccountType := decodeTransactionListAccountType(*r.AccountType)
		if erAccountType != nil {
			return domain.TransactionFilter{}, erAccountType
		}

		accountType = &parsedAccountType
	}

	assetCode, erAssetCode := decodeOptionalHistoryAssetCode("[TransactionListHandler:Handle]", "asset", r.AssetCode)
	if erAssetCode != nil {
		return domain.TransactionFilter{}, erAssetCode
	}

	return domain.TransactionFilter{
		Limit:       limit,
		Offset:      offset,
		AssetCode:   assetCode,
		Status:      transactionStatus,
		Type:        transactionType,
		EntryType:   entryType,
		AccountType: accountType,
	}, nil
}

// decodeTransactionListStatus validates and normalizes the transaction status filter.
func decodeTransactionListStatus(value string) (domain.JournalStatus, *appErrors.ErrorResponse) {
	status, err := domain.NewJournalStatusFromString(value)
	if err == nil && slices.Contains(_transactionListAllowedStatuses, status) {
		return status, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[TransactionListHandler:Handle]",
		"status",
		value,
		commonTools.StringValues(_transactionListAllowedStatuses),
	)
}

// decodeTransactionListType validates and normalizes the transaction type filter.
func decodeTransactionListType(value string) (domain.TransactionType, *appErrors.ErrorResponse) {
	txType, err := domain.NewTransactionTypeFromString(value)
	if err == nil && slices.Contains(_transactionListAllowedTypes, txType) {
		return txType, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[TransactionListHandler:Handle]",
		"type",
		value,
		commonTools.StringValues(_transactionListAllowedTypes),
	)
}

// decodeTransactionListEntryType validates and normalizes the business entry filter.
func decodeTransactionListEntryType(value string) (domain.JournalBizType, *appErrors.ErrorResponse) {
	entryType, err := domain.NewJournalBizTypeFromString(value)
	if err == nil && slices.Contains(_transactionListAllowedEntryTypes, entryType) {
		return entryType, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[TransactionListHandler:Handle]",
		"entryType",
		value,
		commonTools.StringValues(_transactionListAllowedEntryTypes),
	)
}

// decodeTransactionListAccountType validates and normalizes the account type filter.
func decodeTransactionListAccountType(value string) (domain.AccountType, *appErrors.ErrorResponse) {
	accountType, err := domain.NewAccountTypeFromString(value)
	if err == nil && slices.Contains(_transactionListAllowedAccountTypes, accountType) {
		return accountType, nil
	}

	return 0, svcErrors.NewOneOfValidationError(
		"[TransactionListHandler:Handle]",
		"accountType",
		value,
		commonTools.StringValues(_transactionListAllowedAccountTypes),
	)
}

// encodeTransactionListResponse encodes domain.TransactionListResult into a response format
func encodeTransactionListResponse(result domain.TransactionListResult) transactionListResponse {
	items := make([]transactionListItem, 0, len(result.List))
	for _, t := range result.List {
		items = append(items, transactionListItem{
			ID:        t.ID.Uint64(),
			Amount:    t.Amount.Shift(-currency.DefaultAssetScale),
			AssetCode: t.AssetCode.String(),
			Type:      t.Type.String(),
			Status:    t.Status.String(),
			BizType:   t.BizType.String(),
			AcctType:  t.AcctType.String(),
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		})
	}

	return transactionListResponse{Transactions: items, Pagination: commonHTTP.EncodePaginationResponse(result.Pagination)}
}
