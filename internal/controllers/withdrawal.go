package controllers

import (
	"context"
	stdErrors "errors"
	"fmt"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/cache"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/services/restriction"
	"github.com/whagency/core-common/tools"
)

type WithdrawalCtrl struct {
	accountCtrl     domain.AccountController
	blockchainCtrl  domain.BlockchainController
	eventCtrl       domain.EventController
	cacheCtrl       domain.CacheController
	transactionCtrl domain.TransactionController

	withdrawalRequests domain.WithdrawalRequestRepository
	outboxRepo         domain.OutboxRepository

	restriction domain.RestrictionService
	user        domain.UserService
	kyt         domain.KYTService
	currency    domain.CurrencyService

	withdrawalLocker     domain.Locker
	processingLocker     domain.Locker
	dbTransactionManager domain.TransactionManager
}

// NewWithdrawalController creates a new WithdrawalCtrl
func NewWithdrawalController(
	accountCtrl domain.AccountController,
	blockchainCtrl domain.BlockchainController,
	eventCtrl domain.EventController,
	cacheCtrl domain.CacheController,
	transactionCtrl domain.TransactionController,

	withdrawalRequests domain.WithdrawalRequestRepository,
	outboxRepo domain.OutboxRepository,

	restriction domain.RestrictionService,
	user domain.UserService,
	kyt domain.KYTService,
	currency domain.CurrencyService,

	withdrawalLocker domain.Locker,
	processingLocker domain.Locker,
	dbTransactionManager domain.TransactionManager,
) *WithdrawalCtrl {
	return &WithdrawalCtrl{
		accountCtrl:     accountCtrl,
		blockchainCtrl:  blockchainCtrl,
		eventCtrl:       eventCtrl,
		cacheCtrl:       cacheCtrl,
		transactionCtrl: transactionCtrl,

		withdrawalRequests: withdrawalRequests,
		outboxRepo:         outboxRepo,

		restriction: restriction,
		user:        user,
		kyt:         kyt,
		currency:    currency,

		withdrawalLocker:     withdrawalLocker,
		processingLocker:     processingLocker,
		dbTransactionManager: dbTransactionManager,
	}
}

// Init initializes a withdrawal request
func (c WithdrawalCtrl) Init(ctx context.Context, params domain.InitWithdrawalParams) error {
	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:Init][params:%+v]", params)

	if err := c.withdrawalLocker.TryLock(ctx, params.UserID().String()); err != nil {
		if !stdErrors.Is(err, cache.KeyAlreadyLockedErr) {
			return svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", err)),
			)
		}
		return svcErrors.New(
			svcErrors.WithdrawalAlreadyInProgressError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init][userID: %d][err: withdrawal already in progress]", params.User.ID)),
		)
	}
	defer func() {
		if err := c.withdrawalLocker.Unlock(ctx, params.UserID().String()); err != nil {
			logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:Init][userID: %d]%s", params.User.ID, err)
		}
	}()

	chainConfig, err := c.cacheCtrl.GetChainConfig(ctx, params.AssetCode, params.Network)
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", er.Cause))
			return er
		}
		return svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", err)),
		)
	}

	if int(chainConfig.Decimals) < int(params.Amount.Exponent())*-1 {
		return svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init][err: maximum number of fractional digits must not exceed %d]", chainConfig.Decimals)),
			appErrors.WithValidationEntry(
				"amount",
				appErrors.ValidationTagMaxFractionalDigits,
				fmt.Sprintf("%d", chainConfig.Decimals),
			),
		)
	}

	// Get and check the user balance
	balance, errGetBalance := c.accountCtrl.GetBalance(ctx, domain.GetBalanceParams{
		OwnerID:     params.NullableUserID(),
		AssetCode:   params.AssetCode,
		AccountType: domain.AccountTypeLiability,
	})
	if errGetBalance != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", errGetBalance)),
		)
	}

	// Both sides quantized: GetBalance returns the stored value and
	// AmountQuantized scales the request, which arrives in display units.
	// Comparing against the raw params.Amount would read a balance 10^scale
	// times too large and let every request through.
	if balance.LessThan(params.AmountQuantized()) {
		return svcErrors.New(
			appErrors.InsufficientFundsError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init][err: insufficient funds]")),
		)
	}

	// Check restrictions
	checkLimitResult, errLimit := c.restriction.CheckLimit(ctx, restriction.CheckLimitParams{
		UserID:        params.User.ID,
		AssetCode:     params.AssetCode,
		Amount:        params.AmountQuantized(),
		OperationType: restriction.LimitOperationTypeWithdrawal,
		Country:       params.User.Country,
	})
	if errLimit != nil {
		return svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", errLimit)),
		)
	}
	// Check if the operation is allowed
	if !checkLimitResult.IsAllowed() {
		return svcErrors.New(
			appErrors.RestrictedOperationError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init][userID:%d][err: operation restricted]", params.User.ID)),
			appErrors.WithData(checkLimitResult.GetErrorResponseData()),
		)
	}

	fee, errFee := c.getWithdrawalFee(ctx, params.Amount, params.FeeTier, chainConfig)
	if errFee != nil {
		errFee.SetCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", errFee.Cause))
		return errFee
	}

	if params.Amount.LessThanOrEqual(fee) {
		return svcErrors.New(
			svcErrors.FeeMoreThanAmountError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init][err: commission is greater or equal than the requested amount]")),
		)
	}

	// Create withdrawal request
	withdrawalRequest := domain.WithdrawalRequest{
		UserID:    params.UserID(),
		AssetCode: chainConfig.AssetCode,
		Network:   chainConfig.Network,
		ToAddress: params.ToAddress,
		Amount:    params.AmountQuantized(),
		Fee:       fee.Shift(currency.DefaultAssetScale),
		FeeTier:   params.FeeTier,
		Status:    domain.WithdrawalStatusNew,
	}
	if withdrawalRequest.GetAmountInBaseUnits().GreaterThanOrEqual(chainConfig.MinAmountWithdrawalReview) {
		withdrawalRequest.Status = domain.WithdrawalStatusManualReview
	}

	var errTransaction *appErrors.ErrorResponse
	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTransaction != nil {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:Init]%s", errRollback)
			}
		}
	}()

	if errCreate := c.withdrawalRequests.WithTxFromContext(ctx).Create(ctx, &withdrawalRequest); errCreate != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", errCreate)),
		)
		return errTransaction
	}

	if _, errTransaction = c.performWithdrawalTransaction(ctx, &withdrawalRequest); errTransaction != nil {
		errTransaction.SetCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", errTransaction.Cause))
		return errTransaction
	}

	if errOutbox := c.enqueueWithdrawalCounter(ctx, withdrawalRequest, false); errOutbox != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init][outbox]%w", errOutbox)),
		)
		return errTransaction
	}

	// Commit Transaction
	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Init]%w", errCommit)),
		)
		return errTransaction
	}

	if withdrawalRequest.Status == domain.WithdrawalStatusNew {
		if errPush := c.eventCtrl.PublishWithdrawalCreated(
			ctx,
			domain.WithdrawalRequestCreatedEvent{
				ID: withdrawalRequest.ID.Uint64(),
			}); errPush != nil {
			logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:Init]%s", errPush)
		}
	}

	return nil
}

// SendToBlockchain sends the withdrawal request to the blockchain
func (c WithdrawalCtrl) SendToBlockchain(ctx context.Context, params domain.SendWithdrawalToBlockchain) error {
	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:SendToBlockchain][params:%+v]", params)

	if err := c.processingLocker.TryLock(ctx, params.WithdrawalRequestID.String()); err != nil {
		if !stdErrors.Is(err, cache.KeyAlreadyLockedErr) {
			return svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", err)),
			)
		}
		return svcErrors.New(
			svcErrors.WithdrawalRequestInProgressError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain][withdrawalRequestID:%d][err: processing already in progress]", params.WithdrawalRequestID)),
		)
	}
	defer func() {
		if errUnlock := c.processingLocker.Unlock(ctx, params.WithdrawalRequestID.String()); errUnlock != nil {
			logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:SendToBlockchain]%s", errUnlock)
		}
	}()

	withdrawalRequest, err := c.withdrawalRequests.GetByID(ctx, params.WithdrawalRequestID)
	if err != nil {
		if stdErrors.Is(err, domain.ErrRecordNotFound) {
			return svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", err)),
			)
		}
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", err)),
		)
	}
	if withdrawalRequest.Status != domain.WithdrawalStatusNew {
		return svcErrors.New(
			appErrors.InvalidRequestError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain][withdrawalRequestID:%d][err: invalid status]", withdrawalRequest.ID)),
		)
	}

	dataAddress, errAddress := c.kyt.CheckAddress(ctx, withdrawalRequest.ToAddress, withdrawalRequest.Network.String())
	if errAddress != nil {
		return svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", errAddress)))
	}

	switch dataAddress.Verdict() {
	case domain.KYTVerdictFlagged:
		return c.blockWithdrawalRequest(ctx, withdrawalRequest)
	case domain.KYTVerdictInconclusive:
		// Never broadcast on an undecided verdict. The request stays New, so both
		// the consumer's retry budget and the stuck-New reconcile pass re-run
		// this check once the provider answers. Blocking the user instead would
		// punish them for the provider's outage.
		return svcErrors.New(
			svcErrors.KYTVerdictUnavailableError,
			appErrors.WithCause(fmt.Errorf(
				"[WithdrawalCtrl:SendToBlockchain][withdrawalRequestID:%d][kytStatus:%d][err: no usable KYT verdict, not sending]",
				withdrawalRequest.ID, dataAddress.Status)),
		)
	}

	blockchainTx, errSend := c.blockchainCtrl.Withdrawal(ctx, domain.WithdrawalBlockchainTxParams{
		AssetCode:      withdrawalRequest.AssetCode,
		Network:        withdrawalRequest.Network,
		ToAddress:      withdrawalRequest.ToAddress,
		Amount:         withdrawalRequest.GetAmountInBaseUnits().Sub(withdrawalRequest.GetFeeInBaseUnits()),
		FeeTier:        withdrawalRequest.FeeTier,
		IdempotencyKey: withdrawalRequest.GetIdempotencyKeyForBlockchain(),
	})
	if errSend != nil {
		return svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", errSend)),
		)
	}

	var errTx error
	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTx != nil {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:SendToBlockchain]%s", errRollback)
			}
		}
	}()

	withdrawalRequest, errTx = c.withdrawalRequests.WithTxFromContext(ctx).GetByIDForUpdate(ctx, params.WithdrawalRequestID)
	if errTx != nil {
		if stdErrors.Is(errTx, domain.ErrRecordNotFound) {
			errTx = svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", errTx)),
			)
			return errTx
		}
		errTx = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", errTx)),
		)
		return errTx
	}
	if withdrawalRequest.Status != domain.WithdrawalStatusNew {
		errTx = svcErrors.New(
			appErrors.InvalidRequestError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain][withdrawalRequestID:%d][err: status changed before commit]", withdrawalRequest.ID)),
		)
		return errTx
	}

	if _, errProcess := c.process(ctx, &withdrawalRequest, blockchainTx); errProcess != nil {
		errProcess.SetCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", errProcess.Cause))
		errTx = errProcess
		return errTx
	}

	if errOutbox := c.enqueueWithdrawalNotificationPush(ctx, withdrawalRequest); errOutbox != nil {
		errTx = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain][outbox]%w", errOutbox)),
		)
		return errTx
	}

	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		errTx = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:SendToBlockchain]%w", errCommit)),
		)
		return errTx
	}

	return nil
}

// blockWithdrawalRequest marks the withdrawal as blocked (KYT failed) and blocks the user post-commit.
func (c WithdrawalCtrl) blockWithdrawalRequest(ctx context.Context, withdrawalRequest domain.WithdrawalRequest) error {
	withdrawalRequest.Status = domain.WithdrawalStatusBlocked
	withdrawalRequest.Description = tools.ToPtr("Blocked by KYT service")

	var errTx error
	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTx != nil {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:blockWithdrawalRequest]%s", errRollback)
			}
		}
	}()

	// Re-validate under row lock before writing
	locked, errGet := c.withdrawalRequests.WithTxFromContext(ctx).GetByIDForUpdate(ctx, withdrawalRequest.ID)
	if errGet != nil {
		errTx = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:blockWithdrawalRequest]%w", errGet)),
		)
		return errTx
	}
	if locked.Status != domain.WithdrawalStatusNew {
		errTx = svcErrors.New(
			appErrors.InvalidRequestError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:blockWithdrawalRequest][withdrawalRequestID:%d][err: status changed before commit]", withdrawalRequest.ID)),
		)
		return errTx
	}

	if errUpdate := c.withdrawalRequests.WithTxFromContext(ctx).Update(ctx, withdrawalRequest.ID, domain.UpdateWithdrawalRequestParams{
		Status:      withdrawalRequest.Status,
		Description: withdrawalRequest.Description,
	}); errUpdate != nil {
		errTx = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:blockWithdrawalRequest]%w", errUpdate)),
		)
		return errTx
	}

	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		errTx = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:blockWithdrawalRequest]%w", errCommit)),
		)
		return errTx
	}

	if errBlockUser := c.user.BlockUser(ctx, withdrawalRequest.UserID.Uint64()); errBlockUser != nil {
		logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:blockWithdrawalRequest][withdrawalRequestID:%d][userID:%d][err: failed to block user after commit]%s",
			withdrawalRequest.ID, withdrawalRequest.UserID, errBlockUser)
	}

	return nil
}

// Process updates the status of a withdrawal request based on blockchain transaction status
func (c WithdrawalCtrl) Process(ctx context.Context, params domain.ProcessWithdrawalParams) error {
	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:Process][params:%+v]", params)

	// Get blockchain transaction
	blockchainTx, errGet := c.blockchainCtrl.GetTransactionByID(ctx, params.BlockchainTxID)
	if errGet != nil {
		if stdErrors.Is(errGet, domain.ErrRecordNotFound) {
			return svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Process]%w", errGet)),
			)
		}
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Process]%w", errGet)),
		)
	}
	if blockchainTx.Type != domain.BlockchainTxTypeOutbound {
		logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:Process][blockchainTx:%+v][err: invalid transaction type]", blockchainTx)
		return nil
	}

	var committed bool
	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if !committed {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:Process]%s", errRollback)
			}
		}
	}()

	withdrawalRequest, err := c.withdrawalRequests.WithTxFromContext(ctx).GetByBlockchainIDForUpdate(ctx, blockchainTx.ID)
	if err != nil {
		if stdErrors.Is(err, domain.ErrRecordNotFound) {
			return svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Process]%w", err)),
			)
		}
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Process]%w", err)),
		)
	}
	if withdrawalRequest.Status != domain.WithdrawalStatusPending {
		logger.LogCtx(ctx).Warnf("[WithdrawalCtrl:Process][withdrawalRequest:%+v][err: withdrawal request is not pending]", withdrawalRequest)
		return nil
	}

	if _, errProcess := c.process(ctx, &withdrawalRequest, blockchainTx); errProcess != nil {
		errProcess.SetCause(fmt.Errorf("[WithdrawalCtrl:Process]%w", errProcess.Cause))
		return errProcess
	}

	if errOutbox := c.enqueueWithdrawalNotificationPush(ctx, withdrawalRequest); errOutbox != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Process][outbox]%w", errOutbox)),
		)
	}

	// Commit Transaction
	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Process]%w", errCommit)),
		)
	}
	committed = true

	return nil
}

// process processes the withdrawal request based on the blockchain transaction status
func (c WithdrawalCtrl) process(ctx context.Context, withdrawalRequest *domain.WithdrawalRequest, blockchainTx domain.BlockchainTx) (domain.MakeTxResult, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:process][withdrawalRequest:%+v][blockchainTx:%+v]", withdrawalRequest, blockchainTx)

	amountMismatch := !withdrawalRequest.Amount.Sub(withdrawalRequest.Fee).Equal(blockchainTx.Amount)
	if amountMismatch {
		// Detected, deliberately not blocking. The ledger settles the requested
		// amount either way, and there is no approval path that could release a
		// withdrawal parked for review — resend requires status New — so parking
		// it here would strand it. Alert on withdrawal=amount_mismatch and
		// reconcile by hand.
		//
		// This cannot fire for a row this service created: the amount is written
		// from the request itself and never updated afterwards. It fires when the
		// row came from a node-service callback that won the race against
		// BlockchainCtrl.Withdrawal, which is the one case where the two amounts
		// have independent origins.
		logger.LogCtx(ctx).
			With(
				"withdrawal", "amount_mismatch",
				"withdrawal_id", withdrawalRequest.ID,
				"blockchain_tx_id", blockchainTx.ID,
				"expected", withdrawalRequest.Amount.Sub(withdrawalRequest.Fee).String(),
				"on_chain", blockchainTx.Amount.String(),
			).
			Errorf("[WithdrawalCtrl:process][withdrawalRequestID:%d][blockchainTxID:%d][err: amount mismatch]",
				withdrawalRequest.ID, blockchainTx.ID)
		withdrawalRequest.Description = tools.ToPtr("Amount mismatch with blockchain transaction")
	}

	var withdrawalRequestStatus domain.WithdrawalStatus
	// Update withdrawal request status based on blockchain transaction status
	switch blockchainTx.Status {
	case domain.BlockchainTxStatusNew, domain.BlockchainTxStatusPending, domain.BlockchainTxPendingSignatures:
		withdrawalRequestStatus = domain.WithdrawalStatusPending
	case domain.BlockchainTxStatusConfirmed:
		withdrawalRequestStatus = domain.WithdrawalStatusSuccess
	case domain.BlockchainTxStatusFailed:
		withdrawalRequestStatus = domain.WithdrawalStatusFailed
	default:
		logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:process][withdrawalRequest:%+v][blockchainTx:%+v][err: unknown blockchain tx status]", withdrawalRequest, blockchainTx)
		return domain.MakeTxResult{}, nil
	}

	if withdrawalRequestStatus == withdrawalRequest.Status {
		// No status change
		return domain.MakeTxResult{}, nil
	}

	withdrawalRequest.BlockchainTxID = &blockchainTx.ID
	withdrawalRequest.Status = withdrawalRequestStatus

	if withdrawalRequest.Status != domain.WithdrawalStatusSuccess {
		if errUpdate := c.withdrawalRequests.WithTxFromContext(ctx).Update(ctx, withdrawalRequest.ID, domain.UpdateWithdrawalRequestParams{
			BlockchainTxID: withdrawalRequest.BlockchainTxID,
			Description:    withdrawalRequest.Description,
			Status:         withdrawalRequest.Status,
		}); errUpdate != nil {
			return domain.MakeTxResult{}, svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[process]%w", errUpdate)),
			)
		}
		// No further processing needed for non-successful withdrawals
		return domain.MakeTxResult{}, nil
	}

	txResult, err := c.performWithdrawalTransaction(ctx, withdrawalRequest)
	if err != nil {
		err.SetCause(fmt.Errorf("[process]%w", err))
		return domain.MakeTxResult{}, err
	}

	if errUpdate := c.withdrawalRequests.WithTxFromContext(ctx).Update(ctx, withdrawalRequest.ID, domain.UpdateWithdrawalRequestParams{
		BlockchainTxID: withdrawalRequest.BlockchainTxID,
		Description:    withdrawalRequest.Description,
		Status:         withdrawalRequest.Status,
		ConfirmedAt:    tools.ToPtr(time.Now().UTC()),
	}); errUpdate != nil {
		return txResult, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[process]%w", errUpdate)),
		)
	}

	return txResult, nil
}

// Reject rejects a withdrawal request
func (c WithdrawalCtrl) Reject(ctx context.Context, withdrawalRequestID domain.ID) (errTransaction error) {
	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:Reject][withdrawalRequestID:%d]", withdrawalRequestID)

	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTransaction != nil {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[WithdrawalCtrl:Reject]%s", errRollback)
			}
		}
	}()

	withdrawalRequest, errGet := c.withdrawalRequests.WithTxFromContext(ctx).GetByIDForUpdate(ctx, withdrawalRequestID)
	if errGet != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Reject]%w", errGet)),
		)
	}

	if !withdrawalRequest.CanBeRejected() {
		return svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Reject][withdrawalRequestID:%d][err: invalid status]", withdrawalRequest.ID)),
		)
	}

	withdrawalRequest.Status = domain.WithdrawalStatusRejected

	if _, errWithdrawal := c.performWithdrawalTransaction(ctx, &withdrawalRequest); errWithdrawal != nil {
		errWithdrawal.SetCause(fmt.Errorf("[WithdrawalCtrl:Reject]%w", errWithdrawal))
		return errWithdrawal
	}

	if errUpdate := c.withdrawalRequests.WithTxFromContext(ctx).Update(ctx, withdrawalRequest.ID, domain.UpdateWithdrawalRequestParams{
		Status: withdrawalRequest.Status,
	}); errUpdate != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Reject]%w", errUpdate)),
		)
	}

	if errOutbox := c.enqueueWithdrawalCounter(ctx, withdrawalRequest, true); errOutbox != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Reject][outbox]%w", errOutbox)),
		)
	}

	// Commit Transaction
	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[WithdrawalCtrl:Reject]%w", errCommit)),
		)
	}

	return nil
}

// getWithdrawalFee calculates the total commission for the withdrawal
func (c WithdrawalCtrl) getWithdrawalFee(ctx context.Context, amount decimal.Decimal, feeTier domain.FeeTier, chainConfig domain.ChainConfig) (decimal.Decimal, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:getWithdrawalFee][amount: %s][feeTier: %d][chainConfig: %+v]", amount.String(), feeTier, chainConfig)

	totalFee := decimal.Zero
	if chainConfig.WithdrawFeeType == domain.FeeTypePercent {
		totalFee = amount.Mul(chainConfig.WithdrawFee).Div(decimal.NewFromInt(100))
	} else if chainConfig.WithdrawFeeType == domain.FeeTypeFixAmount {
		totalFee = chainConfig.WithdrawFee
	}

	chainFee, err := c.blockchainCtrl.GetFees(ctx, domain.GetFeesParams{
		AssetCode: chainConfig.AssetCode,
		Network:   chainConfig.Network,
	})
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[getWithdrawalFee]%w", er.Cause))
			return decimal.Zero, er
		}
		return decimal.Zero, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[getWithdrawalFee]%w", err)),
		)
	}

	switch feeTier {
	case domain.FeeTierSlow:
		totalFee = totalFee.Add(chainFee.GetSlowFee())
	case domain.FeeTierFast:
		totalFee = totalFee.Add(chainFee.GetFastFee())
	default:
		totalFee = totalFee.Add(chainFee.GetStandardFee())
	}

	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:getWithdrawalFee][amount: %s][feeTier: %d][assetCode: %s][network: %s][totalFee: %s]", amount.String(), feeTier, chainConfig.AssetCode.String(), chainConfig.Network.String(), totalFee.String())

	return totalFee.Round(int32(chainConfig.Decimals)), nil
}

// performWithdrawalTransaction performs the withdrawal transaction
func (c WithdrawalCtrl) performWithdrawalTransaction(ctx context.Context, withdrawalRequest *domain.WithdrawalRequest) (domain.MakeTxResult, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:performWithdrawalTransaction][withdrawalRequestID:%d]", withdrawalRequest.ID)

	var (
		txType domain.JournalBizType
		fee    decimal.Decimal
	)
	switch withdrawalRequest.Status {
	case domain.WithdrawalStatusNew, domain.WithdrawalStatusManualReview:
		txType = domain.JournalBizTypeWithdrawalRequest
	case domain.WithdrawalStatusSuccess:
		txType = domain.JournalBizTypeWithdrawalComplete
		fee = withdrawalRequest.Fee
	case domain.WithdrawalStatusRejected:
		txType = domain.JournalBizTypeWithdrawalReject
	default:
		return domain.MakeTxResult{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[performWithdrawalTransaction][withdrawalRequestID:%d][status:%s][err: unsupported withdrawal status]", withdrawalRequest.ID, withdrawalRequest.Status.String())),
		)
	}

	txResult, err := c.transactionCtrl.MakeTransaction(ctx, domain.MakeTxParams{
		Type:           txType,
		AssetCode:      withdrawalRequest.AssetCode,
		UserID:         withdrawalRequest.UserID,
		Amount:         withdrawalRequest.Amount,
		Fee:            fee,
		IdempotencyKey: tools.ToPtr(withdrawalRequest.ID.String()),
	})
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[performWithdrawalTransaction]%w", er.Cause))
			return domain.MakeTxResult{}, er
		}
		return domain.MakeTxResult{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[performWithdrawalTransaction]%w", err)),
		)
	}

	return txResult, nil
}

// enqueueWithdrawalCounter writes a counter-update outbox row for a withdrawal
// inside the active DB transaction. isReverse=true for rollback/reject flows.
func (c WithdrawalCtrl) enqueueWithdrawalCounter(ctx context.Context, withdrawalRequest domain.WithdrawalRequest, isReverse bool) error {
	return c.outboxRepo.WithTxFromContext(ctx).Create(ctx, domain.NewCounterOutboxParams(domain.CounterOutboxParams{
		AggregateType: domain.OutboxAggregateWithdrawal,
		AggregateID:   withdrawalRequest.ID,
		Operation:     domain.OutboxCounterOperationWithdrawal,
		UserID:        withdrawalRequest.UserID.Uint64(),
		OperationType: restriction.LimitOperationTypeWithdrawal.Uint8(),
		AssetCode:     withdrawalRequest.AssetCode.String(),
		Amount:        withdrawalRequest.Amount,
		OccurredAt:    withdrawalRequest.CreatedAt,
		IsReverse:     isReverse,
	}))
}

// enqueueWithdrawalNotificationPush writes an outbox row for a successful withdrawal push notification.
// Only WithdrawalStatusSuccess produces a row; other statuses are no-ops.
func (c WithdrawalCtrl) enqueueWithdrawalNotificationPush(ctx context.Context, withdrawalRequest domain.WithdrawalRequest) error {
	if withdrawalRequest.Status != domain.WithdrawalStatusSuccess {
		return nil
	}
	return c.outboxRepo.WithTxFromContext(ctx).Create(ctx, domain.NewNotificationPushOutboxParams(
		domain.OutboxAggregateWithdrawal,
		withdrawalRequest.ID,
		domain.OutboxCounterOperationWithdrawal,
		domain.OutboxPushSuffixSuccess,
		domain.PushMessage{
			Purpose: domain.NotificationPurposePushSuccessfulWithdrawal,
			Data: domain.PushData{
				"amount": withdrawalRequest.GetAmountInBaseUnits(),
				"asset":  withdrawalRequest.AssetCode.String(),
				"userId": withdrawalRequest.UserID,
			},
		},
	))
}

// ReconcileStuck runs one reconciliation tick.
//
// Pass 1 (stuck Pending): for withdrawals that have been Pending longer than
// PendingThreshold, polls node-service for the on-chain status. If terminal
// (confirmed/failed), feeds the result through the same path the callback
// webhook uses (BlockchainController.ProcessTransaction) and then drives the
// withdrawal-side transition explicitly via Process. The explicit Process
// call is required because ProcessTransaction is a no-op when BlockchainTx
// is already at the right status — which can happen if the original
// blockchain.transaction.event was lost between callback and consumer.
//
// Pass 2 (stuck New): for withdrawals that have been New longer than
// NewThreshold, calls SendToBlockchain (the same method an operator would
// call). Safe to retry: SendToBlockchain has its own per-id processingLocker
// and status guard, and node-service is now idempotent on the
// withdrawal_request_<id>_<network>_<addr> key (Mongo unique index +
// findWithdrawalByIdempotencyKey lookup-before-send), so a duplicate call
// returns the existing txid without a second on-chain submission.
//
// Per-record errors are logged and counted but never abort the tick; the
// next tick will pick up the same records if they remain stuck.
func (c WithdrawalCtrl) ReconcileStuck(ctx context.Context, params domain.ReconcileStuckParams) error {
	logger.LogCtx(ctx).Debugf("[WithdrawalCtrl:ReconcileStuck][params:%+v]", params)

	if params.BatchSize <= 0 {
		return fmt.Errorf("[WithdrawalCtrl:ReconcileStuck][err: BatchSize must be > 0]")
	}

	c.reconcilePendingPass(ctx, params)
	c.reconcileNewPass(ctx, params)
	return nil
}

// reconcilePendingPass implements Pass 1: poll node, advance terminal txs.
func (c WithdrawalCtrl) reconcilePendingPass(ctx context.Context, params domain.ReconcileStuckParams) {
	rows, err := c.withdrawalRequests.FindStuckByStatus(ctx, domain.WithdrawalStatusPending, params.PendingThreshold, params.BatchSize)
	if err != nil {
		logger.LogCtx(ctx).With("reconcile", "withdrawal", "pass", "pending", "outcome", "query_error").
			Errorf("[WithdrawalCtrl:ReconcileStuck][pass:pending][query failed: %s]", err)
		return
	}
	for _, row := range rows {
		c.reconcilePendingOne(ctx, row)
	}
}

// reconcileNewPass implements Pass 2: resend stuck New withdrawals.
func (c WithdrawalCtrl) reconcileNewPass(ctx context.Context, params domain.ReconcileStuckParams) {
	rows, err := c.withdrawalRequests.FindStuckByStatus(ctx, domain.WithdrawalStatusNew, params.NewThreshold, params.BatchSize)
	if err != nil {
		logger.LogCtx(ctx).With("reconcile", "withdrawal", "pass", "new", "outcome", "query_error").
			Errorf("[WithdrawalCtrl:ReconcileStuck][pass:new][query failed: %s]", err)
		return
	}
	for _, row := range rows {
		c.reconcileNewOne(ctx, row)
	}
}

// reconcilePendingOne handles a single Pending withdrawal in Pass 1.
//
// Each branch emits a structured log event with reconcile=withdrawal, pass=pending,
// outcome=<result> for Loki-based monitoring.
func (c WithdrawalCtrl) reconcilePendingOne(ctx context.Context, withdrawalRequest domain.WithdrawalRequest) {
	log := logger.LogCtx(ctx).With("reconcile", "withdrawal", "pass", "pending", "withdrawal_id", withdrawalRequest.ID)
	log.Debugf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequest:%+v]", withdrawalRequest)

	if withdrawalRequest.BlockchainTxID == nil {
		// Pending without a blockchain tx is an invariant violation; this
		// withdrawal was either inserted directly into Pending (should not
		// happen) or BlockchainTx pointer was cleared. Operator review only.
		log.With("outcome", "invariant_violation").
			Errorf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d][err: pending without blockchain_tx_id]", withdrawalRequest.ID)
		return
	}

	statusResult, err := c.blockchainCtrl.PollTransactionStatus(ctx, *withdrawalRequest.BlockchainTxID)
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok && er.Code == appErrors.InvalidRequestError {
			// A Pending withdrawal can briefly point at a BlockchainTx before
			// node-service returns the on-chain hash (multisig pre-broadcast).
			// Leave it for the next reconcile tick.
			log.With("outcome", "no_tx_hash").
				Debugf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d] no on-chain hash yet, waiting for next tick", withdrawalRequest.ID)
			return
		}
		log.With("outcome", "node_unreachable").
			Errorf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d]%s", withdrawalRequest.ID, err)
		return
	}

	if statusResult.Status != domain.BlockchainTxStatusConfirmed && statusResult.Status != domain.BlockchainTxStatusFailed {
		// Node reports non-terminal; nothing to do this tick.
		return
	}

	// Reload BlockchainTx so ProcessTransaction params reflect the current
	// row (some fields like FromAddress are needed and don't change but we
	// avoid making assumptions).
	bcTx, errGet := c.blockchainCtrl.GetTransactionByID(ctx, *withdrawalRequest.BlockchainTxID)
	if errGet != nil {
		log.With("outcome", "db_error").
			Errorf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d]%s", withdrawalRequest.ID, errGet)
		return
	}

	// Drive the BlockchainTx update through the same path as the callback
	// webhook. Idempotent: if BlockchainTx is already at terminal status
	// (callback succeeded but consumer event was lost), this no-ops cleanly
	// and we still call Process below to advance the withdrawal.
	processErr := c.blockchainCtrl.ProcessTransaction(ctx, domain.ProcessBlockchainTxParams{
		ExternalRequestID:     bcTx.ExternalRequestID,
		TxID:                  statusResult.TxID,
		AssetCode:             bcTx.AssetCode,
		Network:               bcTx.Network,
		Type:                  bcTx.Type,
		FromAddress:           bcTx.FromAddress,
		ToAddress:             bcTx.ToAddress,
		Amount:                bcTx.GetAmountInBaseUnits(),
		Confirmations:         statusResult.Confirmations,
		RequiredConfirmations: statusResult.RequiredConfirmations,
		Status:                statusResult.Status,
	})
	if processErr != nil {
		// "BlockchainTxInProgressError" means the callback handler is
		// running concurrently — the callback path will publish the event
		// and the consumer (or our next tick) will advance the withdrawal.
		if er, ok := appErrors.AsErrorResponse(processErr); ok && er.Code == svcErrors.BlockchainTxInProgressError {
			log.With("outcome", "lock_busy").
				Debugf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d] blockchain tx lock busy, skipping", withdrawalRequest.ID)
			return
		}
		log.With("outcome", "process_tx_error").
			Errorf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d][ProcessTransaction]%s", withdrawalRequest.ID, processErr)
		return
	}

	// Drive the withdrawal-side transition explicitly. This call is what
	// recovers from a lost blockchain.transaction.event message: even if
	// ProcessTransaction was a no-op above, Process reads the now-correct
	// BlockchainTx and applies the Pending → Success/Failed transition.
	// Status guard inside Process makes it safe to call concurrently with
	// the queue consumer.
	if err = c.Process(ctx, domain.ProcessWithdrawalParams{BlockchainTxID: *withdrawalRequest.BlockchainTxID}); err != nil {
		log.With("outcome", "process_error").
			Errorf("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d][Process]%s", withdrawalRequest.ID, err)
		return
	}

	log.With("outcome", "recovered").
		Infof("[WithdrawalCtrl:reconcilePendingOne][withdrawalRequestID:%d] recovered", withdrawalRequest.ID)
}

// reconcileNewOne handles a single New withdrawal in Pass 2.
//
// Each branch emits a structured log event with reconcile=withdrawal, pass=new,
// outcome=<result> for Loki-based monitoring.
func (c WithdrawalCtrl) reconcileNewOne(ctx context.Context, withdrawalRequest domain.WithdrawalRequest) {
	log := logger.LogCtx(ctx).With("reconcile", "withdrawal", "pass", "new", "withdrawal_id", withdrawalRequest.ID)
	log.Debugf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequest:%+v]", withdrawalRequest)

	// Pre-check status before invoking SendToBlockchain so status changes are
	// visible separately from transient locker contention.
	fresh, err := c.withdrawalRequests.GetByID(ctx, withdrawalRequest.ID)
	if err != nil {
		log.With("outcome", "db_error").
			Errorf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d]%s", withdrawalRequest.ID, err)
		return
	}
	if fresh.Status != domain.WithdrawalStatusNew {
		// Queue consumer or operator advanced the withdrawal between scan and
		// our check — nothing to do.
		log.With("outcome", "status_changed").
			Debugf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d] status changed to %v, skipping", withdrawalRequest.ID, fresh.Status)
		return
	}

	sendErr := c.SendToBlockchain(ctx, domain.SendWithdrawalToBlockchain{WithdrawalRequestID: withdrawalRequest.ID})
	if sendErr == nil {
		log.With("outcome", "sent").
			Infof("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d] sent to blockchain", withdrawalRequest.ID)
		return
	}

	er, ok := appErrors.AsErrorResponse(sendErr)
	if !ok {
		log.With("outcome", "send_error").
			Errorf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d][SendToBlockchain]%s", withdrawalRequest.ID, sendErr)
		return
	}
	switch er.Code {
	case svcErrors.WithdrawalRequestInProgressError:
		// processingLocker inside SendToBlockchain is held by another worker.
		log.With("outcome", "lock_busy").
			Debugf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d] locker busy, skipping", withdrawalRequest.ID)
	case appErrors.ExternalServiceError:
		log.With("outcome", "node_unreachable").
			Errorf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d][SendToBlockchain]%s", withdrawalRequest.ID, sendErr)
	case svcErrors.KYTVerdictUnavailableError:
		// Held on purpose, not a fault: the provider owes us a verdict. Kept
		// distinct from node_unreachable so a KYT outage is visible as itself.
		log.With("outcome", "kyt_inconclusive").
			Warnf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d][SendToBlockchain]%s", withdrawalRequest.ID, sendErr)
	case appErrors.DatabaseError:
		log.With("outcome", "db_error").
			Errorf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d][SendToBlockchain]%s", withdrawalRequest.ID, sendErr)
	default:
		log.With("outcome", "send_error", "error_code", er.Code).
			Errorf("[WithdrawalCtrl:reconcileNewOne][withdrawalRequestID:%d][SendToBlockchain][code:%v]%s", withdrawalRequest.ID, er.Code, sendErr)
	}
}
