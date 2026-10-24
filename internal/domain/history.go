package domain

import (
	"context"
)

// HistoryController defines the business logic for managing history data lists.
type HistoryController interface {
	TransactionList(context.Context, ID, TransactionFilter) (TransactionListResult, error)
	WithdrawalList(context.Context, ID, WithdrawalFilter) (WithdrawalRequestListResult, error)
	DepositList(context.Context, ID, DepositFilter) (DepositRequestListResult, error)
	TransferList(context.Context, ID, TransferFilter) (TransferRequestListResult, error)
	WithdrawalDetails(context.Context, ID, ID) (WithdrawalRequestWithTransactionDetails, error)
	DepositDetails(context.Context, ID, ID) (DepositRequestWithTransactionDetails, error)
}
