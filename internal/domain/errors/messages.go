package errors

const (
	AccountIntegrityErrMessage            = "account integrity verification failed."
	WithdrawalRequestExistsErrMessage     = "existing pending withdrawal request."
	UserNumberNotFoundErrMessage          = "the user with the specified number does not exist."
	SelfPaymentErrMessage                 = "self-payment transactions are forbidden."
	FeeMoreThanAmountErrMessage           = "commission is greater or equal than the requested amount."
	WithdrawalAlreadyInProgressErrMessage = "withdrawal is already in progress."
	DepositAlreadyInProgressErrMessage    = "deposit is already in progress."
	TransferAlreadyInProgressErrMessage   = "transfer is already in progress."
	BlockchainTxInProgressErrMessage      = "blockchain transaction is already in progress."
	DepositRequestInProgressErrMessage    = "deposit request is already in progress."
	WithdrawalRequestInProgressErrMessage = "withdrawal request is already in progress."
	KYTVerdictUnavailableErrMessage       = "address risk verification is unavailable."
)
