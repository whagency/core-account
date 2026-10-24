package errors

import (
	"net/http"

	"github.com/whagency/core-common/errorsx"
)

// Templates returns a map of error codes to their corresponding error templates for the account domain.
func Templates() map[int]errorsx.Template {
	return map[int]errorsx.Template{
		AccountIntegrityError: {
			Code:       AccountIntegrityError,
			Message:    AccountIntegrityErrMessage,
			HTTPStatus: http.StatusForbidden,
		},
		WithdrawalRequestExistsError: {
			Code:       WithdrawalRequestExistsError,
			Message:    WithdrawalRequestExistsErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		UserNumberNotFoundError: {
			Code:       UserNumberNotFoundError,
			Message:    UserNumberNotFoundErrMessage,
			HTTPStatus: http.StatusNotFound,
		},
		SelfPaymentError: {
			Code:       SelfPaymentError,
			Message:    SelfPaymentErrMessage,
			HTTPStatus: http.StatusNotFound,
		},
		FeeMoreThanAmountError: {
			Code:       FeeMoreThanAmountError,
			Message:    FeeMoreThanAmountErrMessage,
			HTTPStatus: http.StatusBadRequest,
		},
		WithdrawalAlreadyInProgressError: {
			Code:       WithdrawalAlreadyInProgressError,
			Message:    WithdrawalAlreadyInProgressErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		DepositAlreadyInProgressError: {
			Code:       DepositAlreadyInProgressError,
			Message:    DepositAlreadyInProgressErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		TransferAlreadyInProgressError: {
			Code:       TransferAlreadyInProgressError,
			Message:    TransferAlreadyInProgressErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		BlockchainTxInProgressError: {
			Code:       BlockchainTxInProgressError,
			Message:    BlockchainTxInProgressErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		DepositRequestInProgressError: {
			Code:       DepositRequestInProgressError,
			Message:    DepositRequestInProgressErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		WithdrawalRequestInProgressError: {
			Code:       WithdrawalRequestInProgressError,
			Message:    WithdrawalRequestInProgressErrMessage,
			HTTPStatus: http.StatusConflict,
		},
		// Not a client mistake and not permanent: the KYT provider owes us a
		// verdict we did not get. Callers retry rather than fall through.
		KYTVerdictUnavailableError: {
			Code:       KYTVerdictUnavailableError,
			Message:    KYTVerdictUnavailableErrMessage,
			HTTPStatus: http.StatusServiceUnavailable,
		},
	}
}
