package controllers

import (
	"context"
	stdErrors "errors"
	"fmt"
	"strings"
	"time"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/cache"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
)

type BlockchainCtrl struct {
	eventCtrl domain.EventController
	cacheCtrl domain.CacheController

	chainConfigs       domain.ChainConfigRepository
	addressAssignments domain.AddressAssignmentRepository
	blockchainTxs      domain.BlockchainTxRepository

	node     domain.NodeService
	kyt      domain.KYTService
	currency domain.CurrencyService

	processingLocker     domain.Locker
	dbTransactionManager domain.TransactionManager
}

// NewBlockchainController creates a new blockchain gateway controller
func NewBlockchainController(
	eventCtrl domain.EventController,
	cacheCtrl domain.CacheController,

	addressAssignments domain.AddressAssignmentRepository,
	blockchainTxs domain.BlockchainTxRepository,
	chainConfigs domain.ChainConfigRepository,

	node domain.NodeService,
	kyt domain.KYTService,
	currency domain.CurrencyService,

	processingLocker domain.Locker,
	dbTransactionManager domain.TransactionManager,
) *BlockchainCtrl {
	return &BlockchainCtrl{
		eventCtrl: eventCtrl,
		cacheCtrl: cacheCtrl,

		addressAssignments: addressAssignments,
		blockchainTxs:      blockchainTxs,
		chainConfigs:       chainConfigs,

		node:     node,
		kyt:      kyt,
		currency: currency,

		processingLocker:     processingLocker,
		dbTransactionManager: dbTransactionManager,
	}
}

// GetChainConfigs retrieves all chain configurations list
func (c BlockchainCtrl) GetChainConfigs(ctx context.Context) (domain.ChainConfigs, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:GetChainConfigs]")

	chainConfigs, errResp := c.chainConfigs.GetAll(ctx)
	if errResp != nil {
		return domain.ChainConfigs{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:GetChainConfigs]%w", errResp)),
		)
	}

	return chainConfigs, nil
}

// AllocateAddress allocates a new address for a user
func (c BlockchainCtrl) AllocateAddress(ctx context.Context, params domain.AllocateAddressParams) (domain.AllocateAddressResult, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:AllocateAddress][params:%+v]", params)

	// Generate new address from node service
	generateAddressResult, err := c.node.GenerateAddress(ctx, domain.GenerateBlockchainAddressParams{
		AssetCode: params.ChainConfig.AssetCode,
		Network:   params.ChainConfig.Network,
	})
	if err != nil {
		return domain.AllocateAddressResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:AllocateAddress]%w", err)),
		)
	}

	// Create address assignment
	newAssignment := domain.AddressAssignment{
		OwnerID:   params.OwnerID,
		AssetCode: params.ChainConfig.AssetCode,
		Network:   params.ChainConfig.Network,
		Purpose:   params.Purpose,
		Address:   generateAddressResult.Address,
		Status:    domain.AddressStatusAllocated,
	}
	if err = c.addressAssignments.Create(ctx, &newAssignment); err != nil {
		return domain.AllocateAddressResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:AllocateAddress]%w", err)),
		)
	}

	return domain.AllocateAddressResult{
		AddressAssignment: newAssignment,
	}, nil
}

// RefreshAddress allocates a new address for a user
func (c BlockchainCtrl) RefreshAddress(ctx context.Context, params domain.RefreshAddressParams) error {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:RefreshAddress][params:%+v]", params)

	if _, err := c.node.GenerateAddress(ctx, domain.GenerateBlockchainAddressParams{
		AssetCode:      params.ChainConfig.AssetCode,
		Network:        params.ChainConfig.Network,
		RefreshAddress: &params.Address,
	}); err != nil {
		return svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:RefreshAddress]%w", err)),
		)
	}

	return nil
}

// screenSender returns the KYT verdict for a transaction's counterparty.
//
// Only inbound transactions are screened. An outbound one had its destination
// checked in WithdrawalCtrl.SendToBlockchain before the funds were sent, so
// tracking the resulting transaction has no address left to check — it says so
// with NotChecked rather than leaving the verdict unset, because an unset
// verdict is what the allow-list in calculateBlockchainTxStatus refuses.
//
// A transport failure returns the zero verdict alongside the error. Callers
// must not use it: "we never got an answer" is not a verdict, and the zero
// value is the one the allow-list denies.
func (c BlockchainCtrl) screenSender(ctx context.Context, params domain.ProcessBlockchainTxParams) (domain.KYTVerdict, error) {
	if params.Type != domain.BlockchainTxTypeInbound {
		return domain.KYTVerdictNotChecked, nil
	}

	result, err := c.kyt.CheckAddress(ctx, params.FromAddress, params.Network.String())
	if err != nil {
		return 0, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:screenSender]%w", err)),
		)
	}

	verdict := result.Verdict()
	if verdict == domain.KYTVerdictInconclusive {
		logger.LogCtx(ctx).
			With("kyt", verdict.String(), "external_request_id", params.ExternalRequestID).
			Warnf("[BlockchainCtrl:screenSender][externalRequestID:%s][kytStatus:%d][msg: no usable KYT verdict, holding below confirmed]",
				params.ExternalRequestID, result.Status)
	}

	return verdict, nil
}

// verifyBlockchainTxIdentity refuses a callback whose immutable fields disagree
// with the row already stored under the same external_request_id.
//
// Those fields describe which on-chain transaction this is; only status,
// confirmations and the hash ever move. UpdateStatus never writes them, so a
// mismatched callback would previously be applied silently: the reported status
// would land on a row describing a different transfer. That is either a
// node-service defect or a forged callback, and both want an operator, not a
// quiet write.
func verifyBlockchainTxIdentity(ctx context.Context, stored domain.BlockchainTx, params domain.ProcessBlockchainTxParams) error {
	mismatch := ""
	switch {
	case stored.Type != params.Type:
		mismatch = fmt.Sprintf("type %s != %s", stored.Type.String(), params.Type.String())
	case stored.AssetCode != params.AssetCode:
		mismatch = fmt.Sprintf("asset %s != %s", stored.AssetCode.String(), params.AssetCode.String())
	case stored.Network != params.Network:
		mismatch = fmt.Sprintf("network %s != %s", stored.Network.String(), params.Network.String())
	case stored.FromAddress != params.FromAddress:
		mismatch = fmt.Sprintf("from_address %s != %s", stored.FromAddress, params.FromAddress)
	case stored.ToAddress != params.ToAddress:
		mismatch = fmt.Sprintf("to_address %s != %s", stored.ToAddress, params.ToAddress)
	case !stored.Amount.Equal(params.GetQuantizedAmount()):
		// Equal, not !=: decimal carries its exponent, so 1.50 and 1.5 are
		// different structs and the same amount.
		mismatch = fmt.Sprintf("amount %s != %s", stored.Amount.String(), params.GetQuantizedAmount().String())
	}
	if mismatch == "" {
		return nil
	}

	logger.LogCtx(ctx).
		With("blockchain_tx", "identity_mismatch", "external_request_id", params.ExternalRequestID, "blockchain_tx_id", stored.ID).
		Errorf("[BlockchainCtrl:verifyBlockchainTxIdentity][externalRequestID:%s][err: %s]", params.ExternalRequestID, mismatch)

	return svcErrors.New(
		appErrors.InvalidRequestError,
		appErrors.WithCause(fmt.Errorf(
			"[verifyBlockchainTxIdentity][externalRequestID:%s][blockchainTxID:%d][err: callback describes a different transaction: %s]",
			params.ExternalRequestID, stored.ID, mismatch)),
	)
}

// reconcileAgainstStored keeps a stale or reordered callback from undoing what
// the chain already settled.
//
// Two things are held: a terminal transaction is not moved back to a
// non-terminal status, and the confirmation count does not decrease. Delivery is
// at-least-once and unordered, so an old "new, 0 confirmations" callback can
// arrive after the confirmed one and would otherwise rewrite the row — losing
// the record that it ever confirmed, and publishing a lifecycle event for it.
//
// Terminal to terminal stays allowed: a reorg that turns a confirmed
// transaction into a failed one is real, and the operator needs it recorded.
func reconcileAgainstStored(
	ctx context.Context,
	stored domain.BlockchainTx,
	newStatus domain.BlockchainTxStatus,
	confirmations uint8,
) (domain.BlockchainTxStatus, uint8) {
	if stored.Status.IsTerminal() && !newStatus.IsTerminal() {
		logger.LogCtx(ctx).
			With("blockchain_tx", "stale_callback", "external_request_id", stored.ExternalRequestID, "blockchain_tx_id", stored.ID).
			Warnf("[BlockchainCtrl:reconcileAgainstStored][blockchainTxID:%d][msg: keeping terminal %s over reported %s]",
				stored.ID, stored.Status.String(), newStatus.String())
		newStatus = stored.Status
	}

	if confirmations < stored.Confirmations {
		confirmations = stored.Confirmations
	}

	return newStatus, confirmations
}

// ProcessTransaction processes a blockchain transaction
func (c BlockchainCtrl) ProcessTransaction(ctx context.Context, params domain.ProcessBlockchainTxParams) error {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:ProcessTransaction][params:%+v]", params)

	if err := c.processingLocker.TryLock(ctx, params.ExternalRequestID); err != nil {
		if !stdErrors.Is(err, cache.KeyAlreadyLockedErr) {
			return svcErrors.New(
				appErrors.CacheError,
				appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:ProcessTransaction]%w", err)),
			)
		}
		return svcErrors.New(
			svcErrors.BlockchainTxInProgressError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:ProcessTransaction]%w", err)),
		)
	}
	defer func() {
		if errUnlock := c.processingLocker.Unlock(ctx, params.ExternalRequestID); errUnlock != nil {
			logger.LogCtx(ctx).Errorf("[BlockchainCtrl:ProcessTransaction]%s", errUnlock)
		}
	}()

	chainConfig, errGet := c.cacheCtrl.GetChainConfig(ctx, params.AssetCode, params.Network)
	if errGet != nil {
		if er, ok := appErrors.AsErrorResponse(errGet); ok {
			er.SetCause(fmt.Errorf("[BlockchainCtrl:ProcessTransaction]%w", er.Cause))
			return er
		}
		return svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:ProcessTransaction]%w", errGet)),
		)
	}

	verdict, errScreen := c.screenSender(ctx, params)
	if errScreen != nil {
		return errScreen
	}

	newStatus := calculateBlockchainTxStatus(calculateBlockchainTxStatusParams{
		Type:             params.Type,
		Status:           params.Status,
		Amount:           params.GetQuantizedAmount(),
		DustThreshold:    chainConfig.DustThreshold,
		MinConfirmations: chainConfig.MinConfirmations,
		Confirmations:    params.Confirmations,
		KYTVerdict:       verdict,
	})

	blockchainTx, err := c.blockchainTxs.GetByExternalRequestID(ctx, params.ExternalRequestID)
	if err != nil && !stdErrors.Is(err, domain.ErrRecordNotFound) {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:ProcessTransaction]%w", err)),
		)
	}

	needsCreate := blockchainTx.ID == 0

	confirmations := params.Confirmations
	if !needsCreate {
		if errIdentity := verifyBlockchainTxIdentity(ctx, blockchainTx, params); errIdentity != nil {
			return errIdentity
		}

		newStatus, confirmations = reconcileAgainstStored(ctx, blockchainTx, newStatus, confirmations)
	}

	needsTxIDUpdate := !needsCreate &&
		params.TxID != nil && blockchainTx.TxID == nil
	needsUpdate := !needsCreate && (blockchainTx.Status != newStatus || blockchainTx.Confirmations != confirmations || needsTxIDUpdate)
	if !needsCreate && !needsUpdate {
		return nil
	}

	if needsCreate {
		blockchainTx = domain.BlockchainTx{
			ExternalRequestID: params.ExternalRequestID,
			TxID:              params.TxID,
			AssetCode:         params.AssetCode,
			Network:           params.Network,
			Type:              params.Type,
			FromAddress:       params.FromAddress,
			ToAddress:         params.ToAddress,
			Amount:            params.GetQuantizedAmount(),
			Confirmations:     params.Confirmations,
			Status:            newStatus,
		}
		if err = c.blockchainTxs.Create(ctx, &blockchainTx); err != nil {
			return svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:ProcessTransaction]%w", err)),
			)
		}
	} else if err = c.blockchainTxs.UpdateStatus(ctx, blockchainTx.ID, newStatus, confirmations, params.TxID); err != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:ProcessTransaction]%w", err)),
		)
	}

	// Publish after the DB write so consumers never observe an event for a
	// missing BlockchainTx. Publish is best-effort here: if RabbitMQ is down,
	// deposit and withdrawal reconciliation jobs recover the missed lifecycle
	// processing from the persisted BlockchainTx/WithdrawalRequest state.
	if err = c.eventCtrl.PublishBlockchainTransactionUpdate(
		ctx,
		domain.BlockchainTransactionUpdateEvent{
			ID:   blockchainTx.ID.Uint64(),
			Type: blockchainTx.Type.Uint8(),
		}); err != nil {
		logger.LogCtx(ctx).Errorf("[BlockchainCtrl:ProcessTransaction]%s", err)
	}

	return nil
}

// Withdrawal processes a withdrawal transaction
func (c BlockchainCtrl) Withdrawal(ctx context.Context, params domain.WithdrawalBlockchainTxParams) (domain.BlockchainTx, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:Withdrawal][params:%+v]", params)

	// Send transaction via node service
	result, err := c.node.Withdrawal(ctx, params)
	if err != nil {
		return domain.BlockchainTx{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:Withdrawal]%w", err)),
		)
	}

	// node-service is idempotent on the withdrawal idempotency key: a resend
	// returns the original request id instead of broadcasting a second time. The
	// matching row can therefore already exist — typically from an earlier
	// attempt whose local transaction never committed — and external_request_id
	// is unique, so inserting again would fail. Reuse the row instead. This is
	// the path the stuck-New reconcile pass depends on: failing here would leave
	// the withdrawal at New forever, retrying against the same constraint on
	// every tick.
	existing, errExisting := c.blockchainTxs.WithTxFromContext(ctx).GetByExternalRequestID(ctx, result.ExternalRequestID)
	if errExisting == nil {
		return c.reuseOutboundBlockchainTx(ctx, existing, params, result)
	}
	if !stdErrors.Is(errExisting, domain.ErrRecordNotFound) {
		return domain.BlockchainTx{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:Withdrawal]%w", errExisting)),
		)
	}

	// Create a blockchain transaction record
	blockchainTx := domain.BlockchainTx{
		ExternalRequestID: result.ExternalRequestID,
		TxID:              result.TxID,
		AssetCode:         params.AssetCode,
		Network:           params.Network,
		Type:              domain.BlockchainTxTypeOutbound,
		FromAddress:       result.FromAddress,
		ToAddress:         params.ToAddress,
		Amount:            params.GetQuantizedAmount(),
		Confirmations:     result.Confirmations,
		Status: calculateBlockchainTxStatus(calculateBlockchainTxStatusParams{
			Type:             domain.BlockchainTxTypeOutbound,
			Status:           result.Status,
			MinConfirmations: result.RequiredConfirmations,
			Confirmations:    result.Confirmations,
			// The destination was screened in SendToBlockchain before the funds
			// left; tracking the resulting transaction does not screen again.
			KYTVerdict: domain.KYTVerdictNotChecked,
		}),
	}
	if err = c.blockchainTxs.WithTxFromContext(ctx).Create(ctx, &blockchainTx); err != nil {
		if stdErrors.Is(err, domain.ErrRecordAlreadyExists) {
			// A concurrent caller inserted the same request id between the
			// lookup above and this insert.
			existing, errExisting = c.blockchainTxs.WithTxFromContext(ctx).GetByExternalRequestID(ctx, result.ExternalRequestID)
			if errExisting != nil {
				return domain.BlockchainTx{}, svcErrors.New(
					appErrors.DatabaseError,
					appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:Withdrawal][lookup_after_duplicate]%w", errExisting)),
				)
			}
			return c.reuseOutboundBlockchainTx(ctx, existing, params, result)
		}

		return domain.BlockchainTx{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:Withdrawal]%w", err)),
		)
	}

	return blockchainTx, nil
}

// reuseOutboundBlockchainTx adopts an already-persisted blockchain transaction
// for a withdrawal that node-service answered with a request id we have seen.
//
// Nothing is written here. ProcessTransaction backfills tx_id once the callback
// arrives, and PollTransactionStatus already falls back to the request id while
// the hash is still unknown, so the reuse path has no state of its own to keep
// current.
//
// The identity checks are not defensive noise: adopting a row means the
// withdrawal will be settled against whatever transaction that row describes,
// so a row that is inbound or points somewhere else must not be adopted. That
// should never happen, and if it does an operator needs to see it rather than
// have a withdrawal quietly attached to an unrelated transaction.
func (c BlockchainCtrl) reuseOutboundBlockchainTx(
	ctx context.Context,
	existing domain.BlockchainTx,
	params domain.WithdrawalBlockchainTxParams,
	result domain.WithdrawalBlockchainTxResult,
) (domain.BlockchainTx, error) {
	if existing.Type != domain.BlockchainTxTypeOutbound || existing.ToAddress != params.ToAddress {
		return domain.BlockchainTx{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf(
				"[BlockchainCtrl:reuseOutboundBlockchainTx][externalRequestID:%s][blockchainTxID:%d][err: existing transaction does not belong to this withdrawal]",
				result.ExternalRequestID, existing.ID)),
		)
	}

	logger.LogCtx(ctx).
		With("blockchain_tx", "reused", "external_request_id", result.ExternalRequestID).
		Infof("[BlockchainCtrl:reuseOutboundBlockchainTx][blockchainTxID:%d][status:%s][msg: node returned a known request id, reusing existing transaction]",
			existing.ID, existing.Status.String())

	return existing, nil
}

// GetTransactionByID retrieves a blockchain transaction by its ID
func (c BlockchainCtrl) GetTransactionByID(ctx context.Context, id domain.ID) (domain.BlockchainTx, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:GetTransactionByID][id:%d]", id)

	blockchainTx, err := c.blockchainTxs.GetByID(ctx, id)
	if err != nil {
		return domain.BlockchainTx{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:GetTransactionByID]%w", err)),
		)
	}

	return blockchainTx, nil
}

// FindStuckInboundForReconcile delegates to the BlockchainTx repository.
// Used by DepositCtrl.ReconcileStuck.
func (c BlockchainCtrl) FindStuckInboundForReconcile(ctx context.Context, olderThan time.Duration, limit int) ([]domain.BlockchainTx, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:FindStuckInboundForReconcile][olderThan:%s][limit:%d]", olderThan, limit)
	return c.blockchainTxs.FindStuckInboundForReconcile(ctx, olderThan, limit)
}

// PollTransactionStatus loads a BlockchainTx and polls node-service for its
// fresh on-chain status. Used by the withdrawal reconciliation cron when the
// blockchain.transaction.event callback was lost. Read-only; the actual DB
// transition still flows through ProcessTransaction.
//
// Dual-path strategy:
//   - If blockchain_txs.tx_id is already set, polls by tx hash via
//     GET /api/wallet/transaction-status/:txid — the canonical on-chain
//     identifier, reliable for confirmed transactions.
//   - If tx_id is not yet set (multisig pre-broadcast state), polls by
//     ExternalRequestID via GET /api/wallet/transaction-status/by-request/:requestId
//     — node-service returns the tx_id once the transaction is broadcast.
func (c BlockchainCtrl) PollTransactionStatus(ctx context.Context, id domain.ID) (domain.TransactionStatusResult, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:PollTransactionStatus][id:%d]", id)

	blockchainTx, err := c.blockchainTxs.GetByID(ctx, id)
	if err != nil {
		return domain.TransactionStatusResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:PollTransactionStatus]%w", err)),
		)
	}

	var result domain.TransactionStatusResult
	if blockchainTx.TxID != nil && *blockchainTx.TxID != "" {
		// Fast path: tx hash is known — use the definitive on-chain identifier.
		result, err = c.node.GetTransactionStatus(ctx, blockchainTx.Network, *blockchainTx.TxID)
	} else {
		// Multisig pre-broadcast: hash not yet available, poll by request ID.
		result, err = c.node.GetTransactionStatusByRequestID(ctx, blockchainTx.Network, blockchainTx.ExternalRequestID)
	}
	if err != nil {
		return domain.TransactionStatusResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:PollTransactionStatus]%w", err)),
		)
	}
	return result, nil
}

// CreateDefaultChainConfigs creates default chain configurations
func (c BlockchainCtrl) CreateDefaultChainConfigs(ctx context.Context) error {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:CreateDefaultChainConfigs]")

	chainConfigs := domain.DefaultChainConfigs()
	for _, config := range chainConfigs {
		if err := c.chainConfigs.Create(ctx, &config); err != nil {
			return svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:CreateDefaultChainConfigs]%w", err)),
			)
		}
	}

	return nil
}

// GetFees retrieves a blockchain gas fee list
func (c BlockchainCtrl) GetFees(ctx context.Context, params domain.GetFeesParams) (domain.GetBlockchainFeeResult, error) {
	logger.LogCtx(ctx).Debugf("[BlockchainCtrl:GetFees][params: %+v]", params)

	rates, errRates := c.currency.GetRateMap(ctx)
	if errRates != nil {
		return domain.GetBlockchainFeeResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:GetFees]%w", errRates)),
		)
	}

	assetRate := decimal.Zero
	direction := strings.ToUpper(fmt.Sprintf("%s_%s", params.Network, params.AssetCode))
	rate, rateOk := rates[direction]
	if rateOk {
		assetRate = rate.Rate
	}

	feeList, err := c.node.GetFees(ctx, params.Network)
	if err != nil {
		return domain.GetBlockchainFeeResult{}, svcErrors.New(
			appErrors.ExternalServiceError,
			appErrors.WithCause(fmt.Errorf("[BlockchainCtrl:GetFees]%w", err)),
		)
	}
	feeList.AssetRate = assetRate

	return feeList, nil
}

type calculateBlockchainTxStatusParams struct {
	Type             domain.BlockchainTxType
	Status           domain.BlockchainTxStatus
	Amount           decimal.Decimal
	DustThreshold    decimal.Decimal
	MinConfirmations uint8
	Confirmations    uint8
	KYTVerdict       domain.KYTVerdict
}

// calculateBlockchainTxStatus calculates the status of a blockchain transaction.
//
// KYTVerdict has no usable zero value: callers state which verdict applies,
// and an unset one is treated as a reason to hold rather than as a clean check.
func calculateBlockchainTxStatus(params calculateBlockchainTxStatusParams) domain.BlockchainTxStatus {
	if params.KYTVerdict == domain.KYTVerdictFlagged {
		return domain.BlockchainTxStatusBlocked
	}
	if params.Status == domain.BlockchainTxStatusFailed {
		return domain.BlockchainTxStatusFailed
	}
	if params.Status == domain.BlockchainTxPendingSignatures {
		return domain.BlockchainTxPendingSignatures
	}
	if params.Type == domain.BlockchainTxTypeInbound {
		if params.Amount.LessThan(params.DustThreshold) {
			return domain.BlockchainTxStatusIgnored
		}
	}
	if params.Confirmations > 0 {
		if params.Status == domain.BlockchainTxStatusConfirmed && params.Confirmations >= params.MinConfirmations {
			// Confirmed is the status the deposit flow credits on, so only a
			// verdict that accounts for the address may reach it. Holding at
			// Pending keeps the row actionable for the deposit reconcile cron,
			// which re-runs this path — and the address check with it — on the
			// next tick.
			if !params.KYTVerdict.PermitsCredit() {
				return domain.BlockchainTxStatusPending
			}
			return domain.BlockchainTxStatusConfirmed
		}
		return domain.BlockchainTxStatusPending
	}

	return domain.BlockchainTxStatusNew
}
