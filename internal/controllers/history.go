package controllers

import (
	"context"
	stdErrors "errors"
	"fmt"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/pagination"
)

type HistoryCtrl struct {
	transactions       domain.TransactionRepository
	depositRequests    domain.DepositRequestRepository
	withdrawalRequests domain.WithdrawalRequestRepository
	transferRequests   domain.TransferRequestRepository
	blockchainTxs      domain.BlockchainTxRepository
}

func NewHistoryController(
	transactions domain.TransactionRepository,
	depositRequests domain.DepositRequestRepository,
	withdrawalRequests domain.WithdrawalRequestRepository,
	transferRequests domain.TransferRequestRepository,
	blockchainTxs domain.BlockchainTxRepository,
) *HistoryCtrl {
	return &HistoryCtrl{
		transactions:       transactions,
		depositRequests:    depositRequests,
		withdrawalRequests: withdrawalRequests,
		transferRequests:   transferRequests,
		blockchainTxs:      blockchainTxs,
	}
}

// TransactionList returns a transaction list for the user.
func (c HistoryCtrl) TransactionList(ctx context.Context, userId domain.ID, filter domain.TransactionFilter) (domain.TransactionListResult, error) {
	logger.LogCtx(ctx).Debugf("[HistoryCtrl:TransactionList][userID: %+v][filter: %+v]", userId, filter)

	transactions, total, err := c.transactions.TransactionList(ctx, userId, filter)
	if err != nil {
		return domain.TransactionListResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:TransactionList]%w", err)),
		)
	}

	return domain.TransactionListResult{
		List:       transactions,
		Pagination: pagination.GetPaginationFromParams(filter.Limit, filter.Offset, len(transactions), total),
	}, nil
}

// WithdrawalList returns a withdrawal list for the user.
func (c HistoryCtrl) WithdrawalList(ctx context.Context, userId domain.ID, filter domain.WithdrawalFilter) (domain.WithdrawalRequestListResult, error) {
	logger.LogCtx(ctx).Debugf("[HistoryCtrl:WithdrawalList][userID: %+v][filter: %+v]", userId, filter)

	withdrawals, total, err := c.withdrawalRequests.WithdrawalList(ctx, userId, filter)
	if err != nil {
		return domain.WithdrawalRequestListResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:WithdrawalList]%w", err)),
		)
	}

	return domain.WithdrawalRequestListResult{
		List:       withdrawals,
		Pagination: pagination.GetPaginationFromParams(filter.Limit, filter.Offset, len(withdrawals), total),
	}, nil
}

// DepositList returns a deposit list for the user.
func (c HistoryCtrl) DepositList(ctx context.Context, userId domain.ID, filter domain.DepositFilter) (domain.DepositRequestListResult, error) {
	logger.LogCtx(ctx).Debugf("[HistoryCtrl:DepositList][userID: %+v][filter: %+v]", userId, filter)

	deposits, total, err := c.depositRequests.DepositList(ctx, userId, filter)
	if err != nil {
		return domain.DepositRequestListResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:DepositList]%w", err)),
		)
	}

	return domain.DepositRequestListResult{
		List:       deposits,
		Pagination: pagination.GetPaginationFromParams(filter.Limit, filter.Offset, len(deposits), total),
	}, nil
}

// TransferList returns a transfer list for the user.
func (c HistoryCtrl) TransferList(ctx context.Context, userID domain.ID, filter domain.TransferFilter) (domain.TransferRequestListResult, error) {
	logger.LogCtx(ctx).Debugf("[HistoryCtrl:TransferList][userID: %+v][filter: %+v]", userID, filter)

	transfers, total, err := c.transferRequests.TransferList(ctx, userID, filter)
	if err != nil {
		return domain.TransferRequestListResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:TransferList]%w", err)),
		)
	}

	return domain.TransferRequestListResult{
		List:       transfers,
		Pagination: pagination.GetPaginationFromParams(filter.Limit, filter.Offset, len(transfers), total),
	}, nil
}

// WithdrawalDetails returns withdrawal details for the user.
func (c HistoryCtrl) WithdrawalDetails(ctx context.Context, userId domain.ID, withdrawalId domain.ID) (domain.WithdrawalRequestWithTransactionDetails, error) {
	logger.LogCtx(ctx).Debugf("[HistoryCtrl:WithdrawalDetails][userID: %d][withdrawalId: %d]", userId, withdrawalId)

	withdrawalRequest, errGet := c.withdrawalRequests.GetByID(ctx, withdrawalId)
	if errGet != nil {
		if stdErrors.Is(errGet, domain.ErrRecordNotFound) {
			return domain.WithdrawalRequestWithTransactionDetails{}, svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[HistoryCtrl:WithdrawalDetails]%w", errGet)),
			)
		}
		return domain.WithdrawalRequestWithTransactionDetails{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:WithdrawalDetails]%w", errGet)),
		)
	}

	if withdrawalRequest.UserID != userId {
		return domain.WithdrawalRequestWithTransactionDetails{}, svcErrors.New(
			appErrors.AccessForbiddenError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:WithdrawalDetails][err: the request is not assigned to the user]")),
		)
	}

	result := domain.WithdrawalRequestWithTransactionDetails{
		Withdrawal: withdrawalRequest,
	}

	if withdrawalRequest.BlockchainTxID != nil {
		blockchainTx, errTx := c.blockchainTxs.GetByID(ctx, *withdrawalRequest.BlockchainTxID)
		if errTx != nil {
			logger.LogCtx(ctx).Errorf("[HistoryCtrl:WithdrawalDetails]%s", errTx)
		} else {
			result.Transaction = blockchainTx
		}
	}

	return result, nil
}

// DepositDetails returns deposit details for the user.
func (c HistoryCtrl) DepositDetails(ctx context.Context, userId domain.ID, depositId domain.ID) (domain.DepositRequestWithTransactionDetails, error) {
	logger.LogCtx(ctx).Debugf("[HistoryCtrl:DepositDetails][userID: %d][depositId: %d]", userId, depositId)

	depositRequest, errGet := c.depositRequests.GetByID(ctx, depositId)
	if errGet != nil {
		if stdErrors.Is(errGet, domain.ErrRecordNotFound) {
			return domain.DepositRequestWithTransactionDetails{}, svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[HistoryCtrl:DepositDetails]%w", errGet)),
			)
		}
		return domain.DepositRequestWithTransactionDetails{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:DepositDetails]%w", errGet)),
		)
	}

	if depositRequest.UserID != userId {
		return domain.DepositRequestWithTransactionDetails{}, svcErrors.New(
			appErrors.AccessForbiddenError,
			appErrors.WithCause(fmt.Errorf("[HistoryCtrl:DepositDetails][err: the request is not assigned to the user]")),
		)
	}

	result := domain.DepositRequestWithTransactionDetails{
		Deposit: depositRequest,
	}

	if depositRequest.BlockchainTxID != nil {
		blockchainTx, errTx := c.blockchainTxs.GetByID(ctx, *depositRequest.BlockchainTxID)
		if errTx != nil {
			logger.LogCtx(ctx).Errorf("[HistoryCtrl:DepositDetails]%s", errTx)
		} else {
			result.Transaction = blockchainTx
		}
	}

	return result, nil
}
