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

// _internalDepositOperationValues defines the allowed operations for internal deposit requests.
var _internalDepositOperationValues = []string{
	"approve",
}

type DepositOperationsHandler struct {
	ctrl domain.DepositController
}

// NewDepositOperationsHandlerHandler creates a new instance of DepositOperationsHandler.
func NewDepositOperationsHandlerHandler(c domain.DepositController) *DepositOperationsHandler {
	return &DepositOperationsHandler{
		ctrl: c,
	}
}

type depositOperationsRequest struct {
	DepositRequestID uint64 `json:"depositRequestId" validate:"required" example:"120"`
	Operation        string `json:"operation" validate:"required,not_blank" example:"approve"`
}

// Handle handles deposit operations requests.
//
//	@Summary 	  Perform deposit operations
//	@Description   Performs operations on deposit requests, such as resending to blockchain or rejecting the request.
//	@Tags           accounts
//	@Accept         json
//	@Produce        json
//	@Param          request body depositOperationsRequest true "Deposit operation payload"
//	@Success        200 {object} commonHTTP.Response{} "Deposit operation completed successfully."
//	@Failure        400 {object} commonHTTP.Response "Missing or malformed request payload."
//	@Failure        500 {object} commonHTTP.Response "Failed to perform withdrawal operation due to internal errors."
//	@Router         /api/v1/internal/accounts/deposits/operations [post]
func (h DepositOperationsHandler) Handle(c *gin.Context) {
	ctx := c.Request.Context()

	var req depositOperationsRequest
	if !commonValidation.BindAndValidateJSON(c, httpValidation.Validator(), &req, "[DepositOperationsHandler:Handle]") {
		return
	}

	operation, erOperation := decodeInternalDepositOperation("[DepositOperationsHandler:Handle]", "operation", req.Operation)
	if erOperation != nil {
		c.AbortWithStatusJSON(erOperation.HTTPStatus, commonHTTP.EncodeErrorResponse(ctx, erOperation))
		return
	}

	switch *operation {
	case "approve":
		if err := h.ctrl.Approve(ctx, domain.NewIDFromUint64(req.DepositRequestID)); commonHTTP.AbortIfError(c, ctx, err) {
			return
		}
	}

	c.JSON(http.StatusOK, commonHTTP.EncodeSuccessResponse(ctx, nil))
}

// decodeInternalDepositOperation decodes and validates the operation for internal deposit requests.
func decodeInternalDepositOperation(scope, field, value string) (*string, *appErrors.ErrorResponse) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if slices.Contains(_internalDepositOperationValues, normalized) {
		return &normalized, nil
	}

	return nil, svcErrors.NewOneOfValidationError(scope, field, value, _internalDepositOperationValues)
}
