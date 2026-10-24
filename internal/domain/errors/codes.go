package errors

const _serviceCode = 300000

const (
	AccountIntegrityError = _serviceCode + iota + 1
	WithdrawalRequestExistsError
	UserNumberNotFoundError
	SelfPaymentError
	FeeMoreThanAmountError
	WithdrawalAlreadyInProgressError
	DepositAlreadyInProgressError
	TransferAlreadyInProgressError
	BlockchainTxInProgressError
	DepositRequestInProgressError
	WithdrawalRequestInProgressError
	KYTVerdictUnavailableError
)
