package account

import (
	appErrors "github.com/whagency/core-common/errorsx"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const _makeTransactionErrorDomain = "account.make_transaction"

const (
	_makeTransactionInsufficientFundsErrorCode   = "insufficient_funds"
	_makeTransactionRestrictedOperationErrorCode = "operation_restricted"
)

// newMakeTransactionStatusError creates a gRPC status error with reason details.
func newMakeTransactionStatusError(code codes.Code, errorCode string) error {
	st := status.New(code, errorCode)
	stWithDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: errorCode,
		Domain: _makeTransactionErrorDomain,
	})
	if err != nil {
		return st.Err()
	}

	return stWithDetails.Err()
}

// mapMakeTransactionErrorResponse maps account domain error response to typed gRPC status errors.
func mapMakeTransactionErrorResponse(resp *appErrors.ErrorResponse) error {
	if resp == nil {
		return nil
	}

	switch resp.Code {
	case appErrors.InsufficientFundsError:
		return newMakeTransactionStatusError(codes.InvalidArgument, _makeTransactionInsufficientFundsErrorCode)
	case appErrors.RestrictedOperationError:
		return newMakeTransactionStatusError(codes.PermissionDenied, _makeTransactionRestrictedOperationErrorCode)
	default:
		return nil
	}
}
