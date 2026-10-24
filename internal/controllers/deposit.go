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

type DepositCtrl struct {
	blockchainCtrl  domain.BlockchainController
	eventCtrl       domain.EventController
	cacheCtrl       domain.CacheController
	transactionCtrl domain.TransactionController

	addressAssignments domain.AddressAssignmentRepository
	depositRequests    domain.DepositRequestRepository
	outboxRepo         domain.OutboxRepository

	restriction domain.RestrictionGRPCService
	user        domain.UserService

	depositLocker    domain.Locker
	processingLocker domain.Locker

	dbTransactionManager domain.TransactionManager
}

// NewDepositController creates a new DepositCtrl
func NewDepositController(
	blockchainCtrl domain.BlockchainController,
	eventCtrl domain.EventController,
	cacheCtrl domain.CacheController,
	transactionCtrl domain.TransactionController,

	addressAssignments domain.AddressAssignmentRepository,
	depositRequests domain.DepositRequestRepository,
	outboxRepo domain.OutboxRepository,

	restriction domain.RestrictionGRPCService,
	user domain.UserService,

	depositLocker domain.Locker,
	processingLocker domain.Locker,

	dbTransactionManager domain.TransactionManager,
) *DepositCtrl {
	return &DepositCtrl{
		blockchainCtrl:  blockchainCtrl,
		eventCtrl:       eventCtrl,
		cacheCtrl:       cacheCtrl,
		transactionCtrl: transactionCtrl,

		addressAssignments: addressAssignments,
		depositRequests:    depositRequests,
		outboxRepo:         outboxRepo,

		restriction: restriction,
		user:        user,

		depositLocker:    depositLocker,
		processingLocker: processingLocker,

		dbTransactionManager: dbTransactionManager,
	}
}

// Init initializes a deposit by allocating an address for the user
func (c DepositCtrl) Init(ctx context.Context, params domain.InitDepositParams) (domain.InitDepositResult, error) {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:Init][params:%+v]", params)

	if err := c.depositLocker.TryLock(ctx, params.UserID().String()); err != nil {
		if !stdErrors.Is(err, cache.KeyAlreadyLockedErr) {
			return domain.InitDepositResult{}, svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[DepositCtrl:Init]%w", err)),
			)
		}
		return domain.InitDepositResult{}, svcErrors.New(
			svcErrors.DepositAlreadyInProgressError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Init][userID: %d][err: deposit already in progress]", params.User.ID)),
		)
	}
	defer func() {
		if err := c.depositLocker.Unlock(ctx, params.UserID().String()); err != nil {
			logger.LogCtx(ctx).Errorf("[DepositCtrl:Init][userID: %d]%s", params.User.ID, err)
		}
	}()

	chainConfig, err := c.cacheCtrl.GetChainConfig(ctx, params.AssetCode, params.Network)
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[DepositCtrl:Init]%w", er.Cause))
			return domain.InitDepositResult{}, er
		}
		return domain.InitDepositResult{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Init]%w", err)),
		)
	}

	// Check restrictions
	checkLimitResult, err := c.restriction.CheckLimit(ctx, restriction.CheckLimitParams{
		UserID:        params.User.ID,
		AssetCode:     params.AssetCode,
		Amount:        domain.MinDepositAmount,
		OperationType: restriction.LimitOperationTypeDeposit,
		Country:       params.User.Country,
	})
	if err != nil {
		return domain.InitDepositResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Init]%w", err)),
		)
	}
	if !checkLimitResult.IsAllowed() {
		return domain.InitDepositResult{}, svcErrors.New(
			appErrors.RestrictedOperationError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Init][userID:%d][err: operation restricted]", params.User.ID)),
			appErrors.WithData(checkLimitResult.GetErrorResponseData()),
		)
	}

	// first try to find an existing address assignment
	existingAssignment, err := c.addressAssignments.GetActive(ctx, domain.GetActiveAddressParams{
		OwnerID:   params.NullableUserID(),
		Purpose:   domain.PurposeAddressDeposit,
		AssetCode: chainConfig.AssetCode,
		Network:   chainConfig.Network,
	})
	if err != nil && !stdErrors.Is(err, domain.ErrRecordNotFound) {
		return domain.InitDepositResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Init]%w", err)),
		)
	}
	if existingAssignment.ID != 0 {
		if errRefresh := c.blockchainCtrl.RefreshAddress(ctx, domain.RefreshAddressParams{
			Address:     existingAssignment.Address,
			ChainConfig: chainConfig,
		}); errRefresh != nil {
			logger.LogCtx(ctx).Errorf("[DepositCtrl:Init]%s", errRefresh)
		}

		return domain.InitDepositResult{
			AddressAssignment: existingAssignment,
			ChainConfig:       chainConfig,
		}, nil
	}

	newAssigment, err := c.blockchainCtrl.AllocateAddress(ctx, domain.AllocateAddressParams{
		OwnerID:     params.NullableUserID(),
		Purpose:     domain.PurposeAddressDeposit,
		ChainConfig: chainConfig,
	})
	if err != nil {
		return domain.InitDepositResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Init]%w", err)),
		)
	}

	return domain.InitDepositResult{
		AddressAssignment: newAssigment.AddressAssignment,
		ChainConfig:       chainConfig,
	}, nil
}

// Process updates the deposit status based on blockchain transaction status
func (c DepositCtrl) Process(ctx context.Context, params domain.ProcessDepositParams) error {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:Process][params:%+v]", params)

	if err := c.processingLocker.TryLock(ctx, params.BlockchainTxID.String()); err != nil {
		if !stdErrors.Is(err, cache.KeyAlreadyLockedErr) {
			return svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[DepositCtrl:Process]%w", err)),
			)
		}
		return svcErrors.New(
			svcErrors.DepositRequestInProgressError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Process]%w", err)),
		)
	}
	defer func() {
		if errUnlock := c.processingLocker.Unlock(ctx, params.BlockchainTxID.String()); errUnlock != nil {
			logger.LogCtx(ctx).Errorf("[DepositCtrl:Process]%s", errUnlock)
		}
	}()

	// Get blockchain transaction
	blockchainTx, errGet := c.blockchainCtrl.GetTransactionByID(ctx, params.BlockchainTxID)
	if errGet != nil {
		if stdErrors.Is(errGet, domain.ErrRecordNotFound) {
			return svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[DepositCtrl:Process]%w", errGet)),
			)
		}
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Process]%w", errGet)),
		)
	}
	if blockchainTx.Type != domain.BlockchainTxTypeInbound {
		logger.LogCtx(ctx).Errorf("[DepositCtrl:Process][blockchainTx:%+v][err: invalid transaction type]", blockchainTx)
		return nil
	}

	var (
		newStatus      = blockchainTxStatusToDepositStatus(blockchainTx.Status)
		depositRequest domain.DepositRequest
		err            error
	)

	depositRequest, err = c.depositRequests.GetByBlockchainID(ctx, blockchainTx.ID)
	if err != nil && !stdErrors.Is(err, domain.ErrRecordNotFound) {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:Process]%w", err)),
		)
	}

	if depositRequest.ID == 0 {
		if errCreate := c.createAndProcessDepositRequest(ctx, blockchainTx, newStatus); errCreate != nil {
			errCreate.SetCause(fmt.Errorf("[DepositCtrl:Process]%w", errCreate.Cause))
			return errCreate
		}
		return nil
	}

	// Only update if current status allows forward transition
	if depositRequest.Status != domain.DepositStatusNew && depositRequest.Status != domain.DepositStatusPending {
		return nil
	}

	// if the new status is the same as the current status, we don't need to update the deposit request'
	if newStatus == depositRequest.Status {
		return nil
	}

	depositRequest.Status = newStatus
	if errUpdate := c.updateAndProcessDepositRequest(ctx, depositRequest); errUpdate != nil {
		errUpdate.SetCause(fmt.Errorf("[DepositCtrl:Process]%w", errUpdate.Cause))
		return errUpdate
	}

	return nil
}

// Approve approves a deposit request that is in manual review
func (c DepositCtrl) Approve(ctx context.Context, depositRequestID domain.ID) error {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:Approve][depositRequestID:%d]", depositRequestID)

	if err := c.approveDepositRequest(ctx, depositRequestID); err != nil {
		err.SetCause(fmt.Errorf("[DepositCtrl:Approve]%w", err.Cause))
		return err
	}

	return nil
}

// AutomaticManualApprove approves all deposit requests that are in manual review for the user
func (c DepositCtrl) AutomaticManualApprove(ctx context.Context, userID domain.ID) error {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:AutomaticManualApprove][userID: %d]", userID)

	depositRequests, errGet := c.depositRequests.GetManualApprove(ctx, userID)
	if errGet != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[DepositCtrl:AutomaticManualApprove]%w", errGet)),
		)
	}

	for _, depositRequest := range depositRequests {
		if err := c.approveDepositRequest(ctx, depositRequest.ID); err != nil {
			logger.LogCtx(ctx).Errorf("[DepositCtrl:AutomaticManualApprove][depositRequestID:%d]%s", depositRequest.ID, err)
		}
	}

	return nil
}

// createAndProcessDepositRequest creates a new deposit request based on the blockchain transaction.
func (c DepositCtrl) createAndProcessDepositRequest(ctx context.Context, blockchainTx domain.BlockchainTx, depositRequestStatus domain.DepositStatus) *appErrors.ErrorResponse {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:createAndProcessDepositRequest][blockchainTx:%+v][depositRequestStatus:%s]", blockchainTx, depositRequestStatus.String())

	// Find address assignment
	addressAssignment, errGetActive := c.addressAssignments.GetActive(ctx, domain.GetActiveAddressParams{
		Address:   &blockchainTx.ToAddress,
		Purpose:   domain.PurposeAddressDeposit,
		AssetCode: blockchainTx.AssetCode,
		Network:   blockchainTx.Network,
	})
	if errGetActive != nil {
		if stdErrors.Is(errGetActive, domain.ErrRecordNotFound) {
			// TODO : What to do if address assignment not found? Save in special table or ignore for now
			logger.LogCtx(ctx).Warnf("[DepositCtrl:createAndProcessDepositRequest][blockchainTxID:%d][err: address assignment not found]", blockchainTx.ID)
			return nil
		}

		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[createAndProcessDepositRequest]%w", errGetActive)),
		)
	}

	if addressAssignment.OwnerID == nil {
		// We can't proceed without owner ID because deposit request must be linked to a user
		logger.LogCtx(ctx).Warnf("[DepositCtrl:createAndProcessDepositRequest][blockchainTxID:%d][addressAssignmentID:%d][err: address assignment owner id is nil]", blockchainTx.ID, addressAssignment.ID)
		return nil
	}

	needsAddressStatusUpdate := addressAssignment.Status == domain.AddressStatusAllocated
	if needsAddressStatusUpdate {
		addressAssignment.Status = domain.AddressStatusInUse
		if depositRequestStatus == domain.DepositStatusBlocked {
			addressAssignment.Status = domain.AddressStatusBlocked
		}
	}

	fee, errFee := c.getDepositFee(ctx, blockchainTx.AssetCode, blockchainTx.Network, blockchainTx.GetAmountInBaseUnits())
	if errFee != nil {
		errFee.SetCause(fmt.Errorf("[createAndProcessDepositRequest]%w", errFee.Cause))
		return errFee
	}

	depositRequest := domain.DepositRequest{
		UserID:             *addressAssignment.OwnerID,
		AssetCode:          blockchainTx.AssetCode,
		Network:            blockchainTx.Network,
		AddressAssigmentID: &addressAssignment.ID,
		BlockchainTxID:     &blockchainTx.ID,
		Amount:             blockchainTx.Amount,
		Status:             depositRequestStatus,
		Fee:                fee,
	}
	if shouldCheckRestrictionForDepositStatus(depositRequestStatus) {
		checkLimitResult, errCheck := c.restriction.CheckLimit(ctx, restriction.CheckLimitParams{
			UserID:        depositRequest.UserID.Uint64(),
			AssetCode:     depositRequest.AssetCode,
			Amount:        depositRequest.Amount,
			OperationType: restriction.LimitOperationTypeDeposit,
		})
		if errCheck != nil {
			if er, ok := appErrors.AsErrorResponse(errCheck); ok {
				er.SetCause(fmt.Errorf("[createAndProcessDepositRequest]%w", er.Cause))
				return er
			}
			return svcErrors.New(
				appErrors.ExternalServiceError,
				appErrors.WithCause(fmt.Errorf("[createAndProcessDepositRequest]%w", errCheck)),
			)
		} else if !checkLimitResult.IsAllowed() {
			depositRequest.Status = domain.DepositStatusManualReview
			decisionWithReason := "Decision: " + checkLimitResult.Decision.String()
			if checkLimitResult.Reason != nil {
				decisionWithReason += ", Reason: " + *checkLimitResult.Reason
			}
			depositRequest.Description = tools.ToPtr(decisionWithReason)
		}
	}

	var errTransaction *appErrors.ErrorResponse
	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTransaction != nil {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[DepositCtrl:createAndProcessDepositRequest]%s", errRollback)
			}
		}
	}()

	if needsAddressStatusUpdate {
		if errUpdate := c.addressAssignments.WithTxFromContext(ctx).UpdateStatus(ctx, addressAssignment.ID, addressAssignment.Status); errUpdate != nil {
			errTransaction = svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[createAndProcessDepositRequest]%w", errUpdate)),
			)
			return errTransaction
		}
	}

	if depositRequest.Status == domain.DepositStatusSuccess {
		now := time.Now().UTC()
		depositRequest.ConfirmedAt = &now
	}

	if errCreate := c.depositRequests.WithTxFromContext(ctx).Create(ctx, &depositRequest); errCreate != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[createAndProcessDepositRequest]%w", errCreate)),
		)
		return errTransaction
	}

	if _, errTransaction = c.applyDepositSuccess(ctx, depositRequest); errTransaction != nil {
		errTransaction.SetCause(fmt.Errorf("[createAndProcessDepositRequest]%w", errTransaction.Cause))
		return errTransaction
	}

	if errOutbox := c.enqueueDepositCounter(ctx, depositRequest); errOutbox != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[createAndProcessDepositRequest][outbox]%w", errOutbox)),
		)
		return errTransaction
	}

	if errOutbox := c.enqueueDepositNotificationPush(ctx, depositRequest); errOutbox != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[createAndProcessDepositRequest][outbox]%w", errOutbox)),
		)
		return errTransaction
	}

	// Commit Transaction
	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[createAndProcessDepositRequest]%w", errCommit)),
		)
		return errTransaction
	}

	c.processDepositSideEffects(ctx, depositRequest)

	return nil
}

// updateAndProcessDepositRequest
func (c DepositCtrl) updateAndProcessDepositRequest(ctx context.Context, depositRequest domain.DepositRequest) (errTransaction *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:updateAndProcessDepositRequest][depositRequest:%+v]", depositRequest)

	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTransaction != nil {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[DepositCtrl:updateAndProcessDepositRequest]%s", errRollback)
			}
		}
	}()

	if depositRequest.Status == domain.DepositStatusSuccess {
		now := time.Now().UTC()
		depositRequest.ConfirmedAt = &now
	}
	if errUpdate := c.depositRequests.WithTxFromContext(ctx).UpdateStatus(
		ctx,
		depositRequest.ID,
		domain.UpdateDepositRequestStatusParams{
			Status:      depositRequest.Status,
			ConfirmedAt: depositRequest.ConfirmedAt,
		},
	); errUpdate != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[updateAndProcessDepositRequest]%w", errUpdate)),
		)
	}

	if _, errTransaction = c.applyDepositSuccess(ctx, depositRequest); errTransaction != nil {
		errTransaction.SetCause(fmt.Errorf("[updateAndProcessDepositRequest]%w", errTransaction.Cause))
		return errTransaction
	}

	if errOutbox := c.enqueueDepositCounter(ctx, depositRequest); errOutbox != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[updateAndProcessDepositRequest][outbox]%w", errOutbox)),
		)
	}

	if errOutbox := c.enqueueDepositNotificationPush(ctx, depositRequest); errOutbox != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[updateAndProcessDepositRequest][outbox]%w", errOutbox)),
		)
	}

	// Commit Transaction
	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[updateAndProcessDepositRequest]%w", errCommit)),
		)
	}

	c.processDepositSideEffects(ctx, depositRequest)

	return nil
}

// getDepositFee calculates the total commission for the deposit
func (c DepositCtrl) getDepositFee(ctx context.Context, assetCode currency.AssetCode, network domain.ChainNetwork, amount decimal.Decimal) (decimal.Decimal, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:getDepositFee][assetCode: %s][network: %s][amount: %v]", assetCode, network.String(), amount)

	chainConfig, err := c.cacheCtrl.GetChainConfig(ctx, assetCode, network)
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[getDepositFee]%w", er.Cause))
			return decimal.Zero, er
		}
		return decimal.Zero, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[getDepositFee]%w", err)),
		)
	}

	totalFee := decimal.Zero
	if chainConfig.DepositFeeType == domain.FeeTypePercent {
		totalFee = amount.Mul(chainConfig.DepositFee).Div(decimal.NewFromInt(100))
	} else if chainConfig.DepositFeeType == domain.FeeTypeFixAmount {
		totalFee = chainConfig.DepositFee
	}

	logger.LogCtx(ctx).Debugf("[DepositCtrl:getDepositFee][assetCode: %s][network: %s][amount: %s][totalFee: %s]", assetCode, network.String(), amount.String(), totalFee.String())

	return totalFee.Shift(currency.DefaultAssetScale), nil
}

// approveDepositRequest approves a deposit request within a transaction.
// It acquires a row lock on the deposit request to prevent concurrent approvals.
func (c DepositCtrl) approveDepositRequest(ctx context.Context, depositRequestID domain.ID) (errTransaction *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:approveDepositRequest][depositRequestID:%d]", depositRequestID)

	var depositRequest domain.DepositRequest

	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if errTransaction != nil {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[DepositCtrl:approveDepositRequest]%s", errRollback)
			}
		}
	}()

	var errGet error
	depositRequest, errGet = c.depositRequests.WithTxFromContext(ctx).GetByIDForUpdate(ctx, depositRequestID)
	if errGet != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[approveDepositRequest]%w", errGet)),
		)
		return errTransaction
	}
	if depositRequest.Status != domain.DepositStatusManualReview {
		errTransaction = svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[approveDepositRequest][depositRequestID:%d][err: deposit request not in manual review]", depositRequestID)),
		)
		return errTransaction
	}

	depositRequest.Status = domain.DepositStatusSuccess
	confirmedAt := time.Now().UTC()
	depositRequest.ConfirmedAt = &confirmedAt

	if errUpdate := c.depositRequests.WithTxFromContext(ctx).UpdateStatus(ctx, depositRequest.ID, domain.UpdateDepositRequestStatusParams{
		Status:      depositRequest.Status,
		ConfirmedAt: &confirmedAt,
	}); errUpdate != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[approveDepositRequest]%w", errUpdate)),
		)
		return errTransaction
	}

	if _, errTransaction = c.applyDepositSuccess(ctx, depositRequest); errTransaction != nil {
		errTransaction.SetCause(fmt.Errorf("[approveDepositRequest]%w", errTransaction.Cause))
		return errTransaction
	}

	if errOutbox := c.enqueueDepositCounter(ctx, depositRequest); errOutbox != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[approveDepositRequest][outbox]%w", errOutbox)),
		)
		return errTransaction
	}

	if errOutbox := c.enqueueDepositNotificationPush(ctx, depositRequest); errOutbox != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[approveDepositRequest][outbox]%w", errOutbox)),
		)
		return errTransaction
	}

	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		errTransaction = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[approveDepositRequest]%w", errCommit)),
		)
		return errTransaction
	}

	c.processDepositSideEffects(ctx, depositRequest)

	return nil
}

// applyDepositSuccess credits the user balance when the deposit status is
// Success. It is a no-op for any other status.
func (c DepositCtrl) applyDepositSuccess(ctx context.Context, depositRequest domain.DepositRequest) (txResult domain.MakeTxResult, errResp *appErrors.ErrorResponse) {
	if depositRequest.Status != domain.DepositStatusSuccess {
		return
	}

	txResult, errResp = c.performDepositSuccessTransaction(ctx, depositRequest)
	if errResp != nil {
		errResp.SetCause(fmt.Errorf("[applyDepositSuccess]%w", errResp.Cause))
		return
	}

	return
}

// performDepositSuccessTransaction performs the deposit transaction
func (c DepositCtrl) performDepositSuccessTransaction(ctx context.Context, depositRequest domain.DepositRequest) (domain.MakeTxResult, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:performDepositSuccessTransaction][depositRequest:%+v]", depositRequest)

	txResult, err := c.transactionCtrl.MakeTransaction(ctx, domain.MakeTxParams{
		Type:           domain.JournalBizTypeDeposit,
		AssetCode:      depositRequest.AssetCode,
		UserID:         depositRequest.UserID,
		Amount:         depositRequest.Amount,
		Fee:            depositRequest.Fee,
		IdempotencyKey: tools.ToPtr(depositRequest.ID.String()),
	})
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[performDepositSuccessTransaction]%w", er.Cause))
			return txResult, er
		}
		return txResult, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[performDepositSuccessTransaction]%w", err)),
		)
	}

	return txResult, nil
}

// shouldCheckRestrictionForDepositStatus determines if restriction check should be performed based on deposit status
func shouldCheckRestrictionForDepositStatus(status domain.DepositStatus) bool {
	return status == domain.DepositStatusNew || status == domain.DepositStatusPending || status == domain.DepositStatusSuccess
}

// blockchainTxStatusToDepositStatus maps a blockchain transaction status to a deposit request status.
func blockchainTxStatusToDepositStatus(status domain.BlockchainTxStatus) domain.DepositStatus {
	switch status {
	case domain.BlockchainTxStatusPending:
		return domain.DepositStatusPending
	case domain.BlockchainTxStatusConfirmed:
		return domain.DepositStatusSuccess
	case domain.BlockchainTxStatusFailed, domain.BlockchainTxStatusIgnored:
		return domain.DepositStatusFailed
	case domain.BlockchainTxStatusBlocked:
		return domain.DepositStatusBlocked
	default:
		return domain.DepositStatusNew
	}
}

// enqueueDepositCounter writes a counter-update outbox row for a successful deposit
// inside the active DB transaction (ctx must carry a transaction via TransactionManager.Begin).
// The outbox worker will call restriction-service CountersUpdate with the same
// idempotency key the service already uses, so retries are safe.
func (c DepositCtrl) enqueueDepositCounter(ctx context.Context, depositRequest domain.DepositRequest) error {
	if depositRequest.Status != domain.DepositStatusSuccess {
		return nil
	}

	occurredAt := time.Now().UTC()
	if depositRequest.ConfirmedAt != nil {
		occurredAt = *depositRequest.ConfirmedAt
	}

	return c.outboxRepo.WithTxFromContext(ctx).Create(ctx, domain.NewCounterOutboxParams(domain.CounterOutboxParams{
		AggregateType: domain.OutboxAggregateDeposit,
		AggregateID:   depositRequest.ID,
		Operation:     domain.OutboxCounterOperationDeposit,
		UserID:        depositRequest.UserID.Uint64(),
		OperationType: restriction.LimitOperationTypeDeposit.Uint8(),
		AssetCode:     depositRequest.AssetCode.String(),
		Amount:        depositRequest.Amount,
		OccurredAt:    occurredAt,
	}))
}

// enqueueDepositNotificationPush writes a notification push outbox row for the deposit status.
// Only DepositStatusSuccess and DepositStatusManualReview produce a push; other statuses are no-ops.
func (c DepositCtrl) enqueueDepositNotificationPush(ctx context.Context, depositRequest domain.DepositRequest) error {
	var purpose domain.NotificationPurpose
	var suffix string
	switch depositRequest.Status {
	case domain.DepositStatusSuccess:
		purpose = domain.NotificationPurposePushSuccessfulDeposit
		suffix = domain.OutboxPushSuffixSuccess
	case domain.DepositStatusManualReview:
		purpose = domain.NotificationPurposePushRequiredVerificationDeposit
		suffix = domain.OutboxPushSuffixManualReview
	default:
		return nil
	}
	return c.outboxRepo.WithTxFromContext(ctx).Create(ctx, domain.NewNotificationPushOutboxParams(
		domain.OutboxAggregateDeposit,
		depositRequest.ID,
		domain.OutboxCounterOperationDeposit,
		suffix,
		domain.PushMessage{
			Purpose: purpose,
			Data: domain.PushData{
				"amount": depositRequest.GetAmountInBaseUnits(),
				"asset":  depositRequest.AssetCode.String(),
				"userId": depositRequest.UserID,
			},
		},
	))
}

// processDepositSideEffects runs best-effort post-commit side effects for a deposit request.
// All operations here are fire-and-forget: the DB commit has already succeeded, so errors
// are logged for alerting but never returned.
func (c DepositCtrl) processDepositSideEffects(ctx context.Context, depositRequest domain.DepositRequest) {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:processDepositSideEffects][depositRequest:%+v]", depositRequest)

	switch depositRequest.Status {
	case domain.DepositStatusBlocked:
		if errBlockUser := c.user.BlockUser(ctx, depositRequest.UserID.Uint64()); errBlockUser != nil {
			logger.LogCtx(ctx).Errorf("[DepositCtrl:processDepositSideEffects][depositRequestID:%d][userID:%d][err: failed to block user after commit]%s",
				depositRequest.ID, depositRequest.UserID, errBlockUser)
		}
	case domain.DepositStatusManualReview:
		if errSetVerification := c.restriction.SetRequiredVerification(ctx, depositRequest.UserID.Uint64(), true); errSetVerification != nil {
			logger.LogCtx(ctx).Errorf("[DepositCtrl:processDepositSideEffects][depositRequestID:%d][userID:%d][err: failed to set required verification after commit]%s",
				depositRequest.ID, depositRequest.UserID, errSetVerification)
		}
	}
}

// ReconcileStuck runs one deposit reconciliation tick.
//
// The cron finds inbound BlockchainTx rows older than Threshold whose
// deposit_request is not yet at terminal Success and, for each, does two
// things:
//
//  1. re-screens the sender against the chain's current status
//     (rescreenFromChain), which releases a transaction held back by an
//     inconclusive KYT verdict once the provider answers again;
//  2. re-runs the same Process() path the queue consumer uses, which recovers
//     a lost blockchain.transaction.event.
//
// Idempotent — Process has its own per-BlockchainTxID lock and status guards,
// ProcessTransaction has its own per-request-id lock and no-ops when nothing
// changed, and the query already excludes rows that are at Success
// (BlockchainTxRepo.FindStuckInboundForReconcile NOT EXISTS clause).
//
// Out of scope: deposits that never reached account-service at all (no
// BlockchainTx). That recovery belongs to node-service's monitoring loop and
// transactionCallbackDispatcher; duplicating the source-of-truth check here
// would risk drift. If a deposit is missing on our side and node never
// re-emits, it is a node-service incident, not an account-service gap.
//
// Observability: each call to reconcileStuckOne emits a structured log event
// with fields reconcile=deposit, outcome=<result>. Use Grafana Loki queries on
// these events instead of Prometheus counters (e.g. count_over_time on
// outcome="recovered" or outcome="db_error").
func (c DepositCtrl) ReconcileStuck(ctx context.Context, params domain.ReconcileStuckDepositParams) error {
	logger.LogCtx(ctx).Debugf("[DepositCtrl:ReconcileStuck][params:%+v]", params)

	if params.BatchSize <= 0 {
		return fmt.Errorf("[DepositCtrl:ReconcileStuck][err: BatchSize must be > 0]")
	}

	rows, err := c.blockchainCtrl.FindStuckInboundForReconcile(ctx, params.Threshold, params.BatchSize)
	if err != nil {
		logger.LogCtx(ctx).With("reconcile", "deposit", "outcome", "query_error").
			Errorf("[DepositCtrl:ReconcileStuck]%s", err)
		return nil
	}

	for _, row := range rows {
		c.reconcileStuckOne(ctx, row)
	}
	return nil
}

// rescreenFromChain re-runs the blockchain-side decision for a stuck inbound
// transaction so its sender is screened again.
//
// The stored status cannot drive this. A transaction held back by an
// inconclusive KYT verdict carries account-service's own capped Pending rather
// than the chain's answer, so re-deriving a status from the stored row would
// only cap it a second time and the hold could never clear. node-service still
// holds the chain's answer, and feeding that through ProcessTransaction is what
// makes the re-check meaningful — that call is also the only path that screens
// the address at all.
//
// Best effort: on a node or screening failure the row is left exactly as it
// was, and the caller still runs Process, which is what recovers the lost
// callback event this cron was originally written for.
func (c DepositCtrl) rescreenFromChain(ctx context.Context, bcTx domain.BlockchainTx) {
	log := logger.LogCtx(ctx).With("reconcile", "deposit", "blockchain_tx_id", bcTx.ID)

	statusResult, errPoll := c.blockchainCtrl.PollTransactionStatus(ctx, bcTx.ID)
	if errPoll != nil {
		log.With("outcome", "node_unreachable").
			Warnf("[DepositCtrl:rescreenFromChain][blockchainTxID:%d]%s", bcTx.ID, errPoll)
		return
	}

	errProcess := c.blockchainCtrl.ProcessTransaction(ctx, domain.ProcessBlockchainTxParams{
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
	if errProcess == nil {
		return
	}

	if er, ok := appErrors.AsErrorResponse(errProcess); ok && er.Code == svcErrors.BlockchainTxInProgressError {
		// A callback for the same request id is running concurrently and
		// screens the address itself. Step out of its way.
		log.With("outcome", "lock_busy").
			Debugf("[DepositCtrl:rescreenFromChain][blockchainTxID:%d] blockchain tx lock busy, skipping", bcTx.ID)
		return
	}

	log.With("outcome", "rescreen_error").
		Errorf("[DepositCtrl:rescreenFromChain][blockchainTxID:%d]%s", bcTx.ID, errProcess)
}

// reconcileStuckOne handles a single inbound BlockchainTx: it re-screens the
// sender, then delegates to the standard Process() path. Errors are logged but
// never abort the tick.
//
// Each branch emits a structured log event with reconcile=deposit and
// outcome=<result> for Loki-based monitoring.
func (c DepositCtrl) reconcileStuckOne(ctx context.Context, bcTx domain.BlockchainTx) {
	log := logger.LogCtx(ctx).With("reconcile", "deposit", "blockchain_tx_id", bcTx.ID)

	// Re-screen before advancing the deposit. Process reads the stored
	// BlockchainTx, so on its own it would never revisit a transaction held
	// back by an inconclusive KYT verdict.
	c.rescreenFromChain(ctx, bcTx)

	processErr := c.Process(ctx, domain.ProcessDepositParams{BlockchainTxID: bcTx.ID})
	if processErr == nil {
		log.With("outcome", "recovered").Infof("[DepositCtrl:reconcileStuckOne][blockchainTxID:%d] recovered", bcTx.ID)
		return
	}

	er, ok := appErrors.AsErrorResponse(processErr)
	if !ok {
		log.With("outcome", "process_fault").Errorf("[DepositCtrl:reconcileStuckOne][blockchainTxID:%d]%s", bcTx.ID, processErr)
		return
	}
	switch er.Code {
	case svcErrors.DepositRequestInProgressError:
		// Process's own processingLocker is held by another worker — the
		// real consumer or a parallel cron tick. Both will advance the
		// row; we just step out of their way.
		log.With("outcome", "lock_busy").Debugf("[DepositCtrl:reconcileStuckOne][blockchainTxID:%d] lock busy, skipping", bcTx.ID)
	case appErrors.RecordNotFoundError:
		// BlockchainTx disappeared between scan and Process — extremely
		// rare. Treat as DB drift.
		log.With("outcome", "not_found").Warnf("[DepositCtrl:reconcileStuckOne][blockchainTxID:%d]%s", bcTx.ID, processErr)
	case appErrors.DatabaseError:
		log.With("outcome", "db_error").Errorf("[DepositCtrl:reconcileStuckOne][blockchainTxID:%d]%s", bcTx.ID, processErr)
	default:
		log.With("outcome", "process_fault", "error_code", er.Code).Errorf("[DepositCtrl:reconcileStuckOne][blockchainTxID:%d][code:%v]%s", bcTx.ID, er.Code, processErr)
	}
}
