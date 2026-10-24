package protected

import (
	"net/http"
	"slices"
	"strings"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"
	httpValidation "account-service/internal/transport/http/validation"

	"github.com/gin-gonic/gin"
	appErrors "github.com/whagency/core-common/errorsx"
	commonHTTP "github.com/whagency/core-common/http"
	commonValidation "github.com/whagency/core-common/http/validation"
)

// _internalWithdrawalOperationValues defines the allowed operations for internal withdrawal requests.
var _internalWithdrawalOperationValues = []string{
	"reject",
	"resend",
}

type WithdrawalOperationsHandler struct {
	ctrl domain.WithdrawalController
}

// NewWithdrawalOperationsHandlerHandler creates a new instance of WithdrawalOperationsHandler.
func NewWithdrawalOperationsHandlerHandler(c domain.WithdrawalController) *WithdrawalOperationsHandler {
	return &WithdrawalOperationsHandler{
		ctrl: c,
	}
}

type withdrawalOperationsRequest struct {
	WithdrawalRequestID uint64 `json:"withdrawalRequestId" validate:"required" example:"310"`
	Operation           string `json:"operation" validate:"required,not_blank" example:"resend" enums:"reject,resend"`
}

// Handle handles withdrawal operations requests.
//
//	@Summary 	  Perform withdrawal operations
//	@Description   Performs operations on withdrawal requests, such as resending to blockchain or rejecting the request.
//	@Tags           accounts
//	@Accept         json
//	@Produce        json
//	@Param          request body withdrawalOperationsRequest true "Withdrawal operation payload. Allowed values: operation=reject|resend."
//	@Success        200 {object} commonHTTP.Response{} "Withdrawal operation completed successfully."
//	@Failure        400 {object} commonHTTP.Response "Missing or malformed request payload."
//	@Failure        500 {object} commonHTTP.Response "Failed to perform withdrawal operation due to internal errors."
//	@Router         /api/v1/internal/accounts/withdrawals/operations [post]
func (h WithdrawalOperationsHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	var req withdrawalOperationsRequest
	if !commonValidation.BindAndValidateJSON(c, httpValidation.Validator(), &req, "[SendWithdrawalToBlockchainHandler:Handle]") {
		return
	}

	operation, erOperation := decodeInternalWithdrawalOperation("[WithdrawalOperationsHandler:Handle]", "operation", req.Operation)
	if erOperation != nil {
		c.AbortWithStatusJSON(erOperation.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, erOperation))
		return
	}

	switch *operation {
	case "resend":
		if err := h.ctrl.SendToBlockchain(ctx, domain.SendWithdrawalToBlockchain{
			WithdrawalRequestID: domain.NewIDFromUint64(req.WithdrawalRequestID),
		}); commonHTTP.AbortIfError(c, ctx, err) {
			return
		}
	case "reject":
		if err := h.ctrl.Reject(ctx, domain.NewIDFromUint64(req.WithdrawalRequestID)); commonHTTP.AbortIfError(c, ctx, err) {
			return
		}
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, nil))
}

// decodeInternalWithdrawalOperation validates and normalizes the operation value for internal withdrawal requests.
func decodeInternalWithdrawalOperation(scope, field, value string) (*string, *appErrors.ErrorResponse) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if slices.Contains(_internalWithdrawalOperationValues, normalized) {
		return &normalized, nil
	}

	return nil, svcErrors.NewOneOfValidationError(scope, field, value, slices.Clone(_internalWithdrawalOperationValues))
}
