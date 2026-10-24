package controllers

import (
	"context"
	stdErrors "errors"
	"fmt"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/whagency/core-common/cache"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/restriction"
	"github.com/whagency/core-common/tools"
)

// TransferCtrl handles transfers between users.
type TransferCtrl struct {
	accountCtrl     domain.AccountController
	transactionCtrl domain.TransactionController

	transferRequests domain.TransferRequestRepository

	restriction domain.RestrictionService
	userGRPC    domain.UserGRPCService

	outboxRepo domain.OutboxRepository

	transferLocker       domain.Locker
	dbTransactionManager domain.TransactionManager
}

// NewTransferController creates a new TransferCtrl instance.
func NewTransferController(
	accountCtrl domain.AccountController,
	transactionCtrl domain.TransactionController,

	transferRequests domain.TransferRequestRepository,
	outboxRepo domain.OutboxRepository,

	restriction domain.RestrictionService,
	userGRPC domain.UserGRPCService,

	transferLocker domain.Locker,
	dbTransactionManager domain.TransactionManager,
) *TransferCtrl {
	return &TransferCtrl{
		accountCtrl:     accountCtrl,
		transactionCtrl: transactionCtrl,

		transferRequests: transferRequests,
		outboxRepo:       outboxRepo,

		restriction:          restriction,
		userGRPC:             userGRPC,
		transferLocker:       transferLocker,
		dbTransactionManager: dbTransactionManager,
	}
}

// Init performs a transfer between users.
func (c TransferCtrl) Init(ctx context.Context, params domain.TransferParams) (domain.TransferResult, error) {
	logger.LogCtx(ctx).Debugf("[TransferCtrl:Init][params:%+v]", params)

	if err := c.transferLocker.TryLock(ctx, params.UserID().String()); err != nil {
		if !stdErrors.Is(err, cache.KeyAlreadyLockedErr) {
			return domain.TransferResult{}, svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init]%w", err)),
			)
		}
		return domain.TransferResult{}, svcErrors.New(
			svcErrors.TransferAlreadyInProgressError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init][userID: %d][err: transfer already in progress]", params.User.ID)),
		)
	}
	defer func() {
		if err := c.transferLocker.Unlock(ctx, params.UserID().String()); err != nil {
			logger.LogCtx(ctx).Errorf("[TransferCtrl:Init][userID: %d]%s", params.User.ID, err)
		}
	}()

	// Get and check the user balance
	balance, errGetBalance := c.accountCtrl.GetBalance(ctx, domain.GetBalanceParams{
		OwnerID:     params.NullableUserID(),
		AssetCode:   params.AssetCode,
		AccountType: domain.AccountTypeLiability,
	})
	if errGetBalance != nil {
		return domain.TransferResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init]%w", errGetBalance)),
		)
	}
	// Both sides quantized: GetBalance returns the stored value and
	// AmountQuantized scales the request, which arrives in display units.
	// Comparing against the raw params.Amount would read a balance 10^scale
	// times too large and let every request through.
	//
	// JournalBizTypeTransfer uses FeeModeAdd, so the sender is debited
	// amount + fee. There is no fee to add here yet: TransferParams carries
	// none and TransferRequest.Fee is never assigned. Whoever introduces a
	// transfer fee has to widen this check with it, or it will pass requests
	// the ledger then rejects.
	if balance.LessThan(params.AmountQuantized()) {
		return domain.TransferResult{}, svcErrors.New(
			appErrors.InsufficientFundsError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init][err: insufficient funds]")),
		)
	}

	// Check restrictions
	checkLimitResult, errLimit := c.restriction.CheckLimit(ctx, restriction.CheckLimitParams{
		UserID:        params.User.ID,
		AssetCode:     params.AssetCode,
		Amount:        params.AmountQuantized(),
		OperationType: restriction.LimitOperationTypeTransfer,
		Country:       params.User.Country,
	})
	if errLimit != nil {
		return domain.TransferResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init]%w", errLimit)),
		)
	}
	// Check if the operation is allowed
	if !checkLimitResult.IsAllowed() {
		return domain.TransferResult{}, svcErrors.New(
			appErrors.RestrictedOperationError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init][userID: %d][err: operation restricted]", params.User.ID)),
			appErrors.WithData(checkLimitResult.GetErrorResponseData()),
		)
	}

	// Get To User ID by number
	toUser, toUserErr := c.userGRPC.GetUserByNumber(ctx, params.ToUserNumber.Uint64())
	if toUserErr != nil {
		return domain.TransferResult{}, svcErrors.New(
			svcErrors.UserNumberNotFoundError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init]%w", toUserErr)),
		)
	}

	// Prevent self-transfer
	if toUser.ID == params.User.ID {
		return domain.TransferResult{}, svcErrors.New(
			svcErrors.SelfPaymentError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init][err: self-transfer is not allowed]")),
		)
	}

	transferRequest := domain.TransferRequest{
		UserFromID:   domain.NewIDFromUint64(params.User.ID),
		UserToID:     domain.NewIDFromUint64(toUser.ID),
		AssetCode:    params.AssetCode,
		Amount:       params.AmountQuantized(),
		ToUserNumber: params.ToUserNumber, // internal field
	}

	var (
		txResult       domain.MakeTxResult
		errTransaction *appErrors.ErrorResponse
	)

	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTransaction != nil {
			logger.LogCtx(ctx).Debugf("[Rollback]%s", errTransaction)

			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[TransferCtrl:Init]%s", errRollback)
			}
		}
	}()

	if errCreate := c.transferRequests.WithTxFromContext(ctx).Create(ctx, &transferRequest); errCreate != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init]%w", errCreate)),
		)
		return domain.TransferResult{}, errTransaction
	}

	txResult, errTransaction = c.performTransferTransaction(ctx, transferRequest)
	if errTransaction != nil {
		errTransaction.SetCause(fmt.Errorf("[TransferCtrl:Init]%w", errTransaction.Cause))
		return domain.TransferResult{}, errTransaction
	}

	// Enqueue restriction counter update and push notifications inside the transaction.
	if errOutbox := c.enqueueTransferCounter(ctx, transferRequest); errOutbox != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init][outbox]%w", errOutbox)),
		)
		return domain.TransferResult{}, errTransaction
	}

	if errOutbox := c.enqueueTransferNotificationPush(ctx, transferRequest); errOutbox != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init][outbox]%w", errOutbox)),
		)
		return domain.TransferResult{}, errTransaction
	}

	// Commit Transaction
	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransferCtrl:Init]%w", errCommit)),
		)
		return domain.TransferResult{}, errTransaction
	}

	return domain.TransferResult{JournalID: txResult.JournalID, Status: txResult.Status}, nil
}

// performTransferTransaction performs the transfer transaction
func (c TransferCtrl) performTransferTransaction(ctx context.Context, transferRequest domain.TransferRequest) (domain.MakeTxResult, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[TransferCtrl:performTransferTransaction][transferRequestID:%d]", transferRequest.ID)

	txResult, err := c.transactionCtrl.MakeTransaction(ctx, domain.MakeTxParams{
		Type:           domain.JournalBizTypeTransfer,
		AssetCode:      transferRequest.AssetCode,
		UserID:         transferRequest.UserFromID,
		ToUserID:       &transferRequest.UserToID,
		Amount:         transferRequest.Amount,
		Fee:            transferRequest.Fee,
		IdempotencyKey: tools.ToPtr(transferRequest.ID.String()),
	})
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[performTransferTransaction]%w", er.Cause))
			return domain.MakeTxResult{}, er
		}
		return domain.MakeTxResult{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[performTransferTransaction]%w", err)),
		)
	}

	return txResult, nil
}

// enqueueTransferCounter inserts an outbox row for the restriction counter update.
func (c TransferCtrl) enqueueTransferCounter(ctx context.Context, transferRequest domain.TransferRequest) error {
	return c.outboxRepo.WithTxFromContext(ctx).Create(ctx, domain.NewCounterOutboxParams(domain.CounterOutboxParams{
		AggregateType: domain.OutboxAggregateTransfer,
		AggregateID:   transferRequest.ID,
		Operation:     domain.OutboxCounterOperationTransfer,
		UserID:        transferRequest.UserFromID.Uint64(),
		OperationType: restriction.LimitOperationTypeTransfer.Uint8(),
		AssetCode:     transferRequest.AssetCode.String(),
		Amount:        transferRequest.Amount,
		OccurredAt:    transferRequest.CreatedAt,
	}))
}

// enqueueTransferNotificationPush writes outbox rows for outgoing and incoming transfer push notifications.
func (c TransferCtrl) enqueueTransferNotificationPush(ctx context.Context, transferRequest domain.TransferRequest) error {
	outgoing := domain.NewNotificationPushOutboxParams(
		domain.OutboxAggregateTransfer,
		transferRequest.ID,
		domain.OutboxCounterOperationTransfer,
		domain.OutboxPushSuffixOutgoing,
		domain.PushMessage{
			Purpose: domain.NotificationPurposePushSuccessfulOutgoingTransfer,
			Data: domain.PushData{
				"amount":       transferRequest.GetAmountInBaseUnits(),
				"asset":        transferRequest.AssetCode.String(),
				"toUserNumber": transferRequest.ToUserNumber.String(),
				"userId":       transferRequest.UserFromID,
			},
		},
	)
	incoming := domain.NewNotificationPushOutboxParams(
		domain.OutboxAggregateTransfer,
		transferRequest.ID,
		domain.OutboxCounterOperationTransfer,
		domain.OutboxPushSuffixIncoming,
		domain.PushMessage{
			Purpose: domain.NotificationPurposePushSuccessfulIncomingTransfer,
			Data: domain.PushData{
				"amount": transferRequest.GetAmountInBaseUnits(),
				"asset":  transferRequest.AssetCode.String(),
				"userId": transferRequest.UserToID,
			},
		},
	)
	repo := c.outboxRepo.WithTxFromContext(ctx)
	if err := repo.Create(ctx, outgoing); err != nil {
		return err
	}
	return repo.Create(ctx, incoming)
}
