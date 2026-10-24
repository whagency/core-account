package controllers

import (
	"context"
	stdErrors "errors"
	"fmt"
	"sort"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/db"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tools"
	"gorm.io/gorm"
)

type TransactionCtrl struct {
	accountCtrl domain.AccountController
	cacheCtrl   domain.CacheController

	accounts     domain.AccountRepository
	journals     domain.JournalEntryRepository
	transactions domain.TransactionRepository

	integrity domain.AccountIntegrityService

	dbTransactionManager domain.TransactionManager
}

func NewTransactionController(
	accountCtrl domain.AccountController,
	cacheCtrl domain.CacheController,

	accounts domain.AccountRepository,
	journals domain.JournalEntryRepository,
	transactions domain.TransactionRepository,

	integrity domain.AccountIntegrityService,
	dbTransactionManager domain.TransactionManager,
) *TransactionCtrl {
	return &TransactionCtrl{
		accountCtrl: accountCtrl,
		cacheCtrl:   cacheCtrl,

		accounts:     accounts,
		journals:     journals,
		transactions: transactions,

		integrity: integrity,

		dbTransactionManager: dbTransactionManager,
	}
}

// MakeTransaction performs a transaction between accounts
func (c TransactionCtrl) MakeTransaction(ctx context.Context, in domain.MakeTxParams) (domain.MakeTxResult, error) {
	logger.LogCtx(ctx).Debugf("[TransactionCtrl:MakeTransaction][in: %+v]", in)

	// check asset validity
	if _, errAsset := c.cacheCtrl.GetAsset(ctx, in.AssetCode); errAsset != nil {
		if er, ok := appErrors.AsErrorResponse(errAsset); ok {
			er.SetCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", er.Cause))
			return domain.MakeTxResult{}, er
		}
		return domain.MakeTxResult{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errAsset)),
		)
	}

	if in.IdempotencyKey != nil {
		// generate idempotent key based on type and key
		in.IdempotencyKey = generateIdempotencyKey(in.Type, *in.IdempotencyKey)

		// check for existing journal entry with the same idempotency key
		if journalEntry, errIdem := c.journals.GetByIdempotencyKey(ctx, *in.IdempotencyKey); errIdem == nil {
			return domain.MakeTxResult{JournalID: journalEntry.ID, Status: journalEntry.Status}, nil
		} else if !stdErrors.Is(errIdem, domain.ErrRecordNotFound) {
			return domain.MakeTxResult{}, svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errIdem)),
			)
		}
	}

	spec, ok := domain.TxRegistry[in.Type]
	if !ok {
		return domain.MakeTxResult{}, svcErrors.New(
			appErrors.InvalidRequestError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction][err: unsupported transaction type %d]", in.Type)),
		)
	}

	plan, errPlan := spec.Assemble(in)
	if errPlan != nil {
		return domain.MakeTxResult{}, svcErrors.New(
			appErrors.InvalidRequestError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errPlan)),
		)
	}

	var err error
	ownsTx := db.GetTxFromContext(ctx) == nil
	if ownsTx {
		ctx = c.dbTransactionManager.Begin(ctx)
	}

	defer func() {
		if ownsTx && err != nil {
			logger.LogCtx(ctx).Debugf("[Rollback]%s", err)
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[TransactionCtrl:MakeTransaction]%s", errRollback)
			}
		}
	}()

	accounts, errAccounts := c.lockAndBindAccounts(ctx, &plan)
	if errAccounts != nil {
		errAccounts.SetCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errAccounts))
		err = errAccounts
		return domain.MakeTxResult{}, err
	}

	deltas, errDeltas := plan.BalanceDeltas()
	if errDeltas != nil {
		err = svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errDeltas)),
		)
		return domain.MakeTxResult{}, err
	}

	if errBalances := plan.CheckResultingBalances(accounts, deltas); errBalances != nil {
		err = svcErrors.New(
			appErrors.InsufficientFundsError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errBalances)),
		)
		return domain.MakeTxResult{}, err
	}

	journalEntry := plan.JournalEntryForCreation()
	if errJournal := c.journals.WithTxFromContext(ctx).Create(ctx, journalEntry); errJournal != nil {
		// Concurrent requests with the same idempotency key can race between
		// pre-check and insert. Treat unique-key conflicts as idempotent success.
		if ownsTx && in.IdempotencyKey != nil && stdErrors.Is(errJournal, gorm.ErrDuplicatedKey) {
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				err = svcErrors.New(
					appErrors.DatabaseError,
					appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction][rollback_after_duplicate_idempotency]%w", errRollback)),
				)
				return domain.MakeTxResult{}, err
			}

			existingJournal, errIdem := c.journals.GetByIdempotencyKey(ctx, *in.IdempotencyKey)
			if errIdem != nil {
				return domain.MakeTxResult{}, svcErrors.New(
					appErrors.DatabaseError,
					appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction][idem_lookup_after_duplicate]%w", errIdem)),
				)
			}

			return domain.MakeTxResult{
				JournalID: existingJournal.ID,
				Status:    existingJournal.Status,
			}, nil
		}

		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errJournal)),
		)
		return domain.MakeTxResult{}, err
	}

	if errPersist := c.persistTransactionsAndBalances(ctx, plan, journalEntry.ID, deltas, accounts); errPersist != nil {
		errPersist.SetCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", errPersist))
		err = errPersist
		return domain.MakeTxResult{}, err
	}

	if ownsTx {
		if err = c.dbTransactionManager.Commit(ctx); err != nil {
			err = svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[TransactionCtrl:MakeTransaction]%w", err)),
			)
			return domain.MakeTxResult{}, err
		}
	}

	return domain.MakeTxResult{
		JournalID: journalEntry.ID,
		Status:    domain.JournalStatusCompleted,
	}, nil
}

// FindJournalByIdempotencyKey reports whether a live journal exists for the given
// business type and caller-supplied idempotency key.
//
// This is the read-only counterpart to MakeTransaction's idempotency check.
// MakeTransaction returns an existing journal for a matching key but creates one
// when there is no match, so it cannot be used to probe: callers that need to
// find out whether a posting exists — compensation and reconciliation flows —
// would create the very posting they were asking about.
//
// The key is derived exactly as it is on write, so callers pass the same value
// they passed to MakeTransaction.
func (c TransactionCtrl) FindJournalByIdempotencyKey(
	ctx context.Context,
	params domain.FindJournalParams,
) (domain.FindJournalResult, error) {
	logger.LogCtx(ctx).Debugf(
		"[TransactionCtrl:FindJournalByIdempotencyKey][type: %s][key: %s]",
		params.Type.String(), params.IdempotencyKey,
	)

	storedKey := generateIdempotencyKey(params.Type, params.IdempotencyKey)

	entry, errGet := c.journals.GetByIdempotencyKey(ctx, *storedKey)
	if errGet != nil {
		if stdErrors.Is(errGet, domain.ErrRecordNotFound) {
			// Not found covers two cases that call for the same action: the
			// operation never happened, or it was rolled back and its key renamed
			// so the business operation could be re-attempted.
			return domain.FindJournalResult{Found: false}, nil
		}
		return domain.FindJournalResult{}, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:FindJournalByIdempotencyKey]%w", errGet)),
		)
	}

	return domain.FindJournalResult{
		Found:     true,
		JournalID: entry.ID,
		Status:    entry.Status,
	}, nil
}

// RollbackTransaction reverts the effects of a previously completed journal entry.
func (c TransactionCtrl) RollbackTransaction(ctx context.Context, journalID domain.ID) error {
	logger.LogCtx(ctx).Debugf("[TransactionCtrl:RollbackTransaction][journalID: %d]", journalID)

	var err error
	ctx = c.dbTransactionManager.Begin(ctx)
	defer func() {
		if err != nil {
			logger.LogCtx(ctx).Debugf("[Rollback]%s", err)
			if errRollback := c.dbTransactionManager.Rollback(ctx); errRollback != nil {
				logger.LogCtx(ctx).Errorf("[TransactionCtrl:RollbackTransaction]%s", errRollback)
			}
		}
	}()

	entry, errGet := c.journals.WithTxFromContext(ctx).GetByIDForUpdate(ctx, journalID)
	if errGet != nil {
		if stdErrors.Is(errGet, domain.ErrRecordNotFound) {
			err = svcErrors.New(
				appErrors.RecordNotFoundError,
				appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errGet)),
			)
		} else {
			err = svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errGet)),
			)
		}
		return err
	}
	if entry.Status == domain.JournalStatusReversed {
		logger.LogCtx(ctx).Warnf("[TransactionCtrl:RollbackTransaction][journalID: %d][msg: already reversed]", journalID)
		if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
			return svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errCommit)),
			)
		}
		return nil
	}

	// Make rollback idempotency key (and update original if needed)
	rollbackIdempotencyKey, errRollback := c.updateJournalEntryIdempotencyKeyAndGetRollbackKey(ctx, &entry)
	if errRollback != nil {
		errRollback.SetCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errRollback.Cause))
		err = errRollback
		return err
	}

	postings, errPostings := c.transactions.WithTxFromContext(ctx).GetByJournalID(ctx, journalID)
	if errPostings != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errPostings)),
		)
		return err
	}
	if len(postings) == 0 {
		err = svcErrors.New(
			appErrors.InvalidRequestError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction][err: no postings found for journal %d]", journalID)),
		)
		return err
	}

	accountsByID, errAccounts := c.lockAccountsForReversal(ctx, uniqueAccountIDsFromPostings(postings))
	if errAccounts != nil {
		errAccounts.SetCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errAccounts.Cause))
		err = errAccounts
		return err
	}

	reversalTransactions := make([]domain.Transaction, 0, len(postings))
	reversalDeltas := make(map[domain.ID]decimal.Decimal, len(accountsByID))

	for i := range postings {
		account, ok := accountsByID[postings[i].AccountID]
		if !ok {
			err = svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction][err: account %d not loaded]", postings[i].AccountID)),
			)
			return err
		}

		originalDelta := postings[i].Type.SignedAmount(postings[i].Amount)
		reversalDelta := originalDelta.Neg()

		reversalDeltas[account.ID] = reversalDeltas[account.ID].Add(reversalDelta)

		reversalType := postings[i].Type
		if postings[i].Type != domain.TransactionTypeFee {
			// For non-fee, we normalize type to debit/credit based on sign
			if reversalDelta.IsNegative() {
				reversalType = domain.TransactionTypeDebit
			} else {
				reversalType = domain.TransactionTypeCredit
			}
		}

		reversalTransactions = append(reversalTransactions, domain.Transaction{
			AccountID: account.ID,
			Type:      reversalType,
			AssetCode: postings[i].AssetCode,
			Amount:    reversalDelta.Abs(),
		})
	}

	if errApplyDeltas := c.applyBalanceDeltas(ctx, accountsByID, reversalDeltas); errApplyDeltas != nil {
		errApplyDeltas.SetCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errApplyDeltas.Cause))
		err = errApplyDeltas
		return err
	}

	reversalEntry := &domain.JournalEntry{
		BizType:                entry.BizType,
		Status:                 domain.JournalStatusCompleted,
		ExternalIdempotencyKey: rollbackIdempotencyKey,
		ReversalOfID:           &journalID,
	}

	if errCreate := c.journals.WithTxFromContext(ctx).Create(ctx, reversalEntry); errCreate != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errCreate)),
		)
		return err
	}

	for i := range reversalTransactions {
		reversalTransactions[i].JournalID = reversalEntry.ID
	}

	if errCreate := c.transactions.WithTxFromContext(ctx).CreateBatch(ctx, reversalTransactions); errCreate != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[AccountCtrl:RollbackTransaction]%w", errCreate)),
		)
		return err
	}

	if errMark := c.journals.WithTxFromContext(ctx).MarkReversed(ctx, entry.ID, reversalEntry.ID); errMark != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errMark)),
		)
		return err
	}

	if errCommit := c.dbTransactionManager.Commit(ctx); errCommit != nil {
		err = svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[TransactionCtrl:RollbackTransaction]%w", errCommit)),
		)
		return err
	}

	return nil
}

// lockAccountsForReversal takes row locks on the accounts a reversal touches,
// in the same order MakeTransaction uses.
//
// The postings only carry account ids, and locking by id is precisely the
// second order that made this deadlock: MakeTransaction locks by
// (owner, asset, type), and the two agree only while account ids happen to be
// created in owner order. So the rows are read unlocked first — solely to learn
// their lookup keys — and then locked through sortAccountLookups.
//
// Reading unlocked costs nothing in correctness here. Accounts are never
// deleted, so a row seen in the first read still exists for the second, and
// every value used afterwards comes from the locked read: the unlocked copy is
// discarded.
func (c TransactionCtrl) lockAccountsForReversal(ctx context.Context, ids []domain.ID) (map[domain.ID]domain.Account, *appErrors.ErrorResponse) {
	unlocked, errRead := c.accounts.WithTxFromContext(ctx).GetByIDs(ctx, ids)
	if errRead != nil {
		return nil, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[lockAccountsForReversal]%w", errRead)),
		)
	}

	lookups := make([]domain.GetForUpdateParams, 0, len(unlocked))
	for _, account := range unlocked {
		lookups = append(lookups, domain.NewAccountParamsForUpdate(account.OwnerID, account.AssetCode, account.Type))
	}
	sortAccountLookups(lookups)

	locked := make(map[domain.ID]domain.Account, len(lookups))
	for i := range lookups {
		account, errLock := c.accounts.WithTxFromContext(ctx).GetForUpdate(ctx, lookups[i])
		if errLock != nil {
			return nil, svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[lockAccountsForReversal]%w", errLock)),
			)
		}
		locked[account.ID] = account
	}

	if len(locked) != len(ids) {
		return nil, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf(
				"[lockAccountsForReversal][err: locked %d accounts for %d postings]", len(locked), len(ids))),
		)
	}

	return locked, nil
}

// persistTransactionsAndBalances persists transactions and updates account balances
func (c TransactionCtrl) persistTransactionsAndBalances(
	ctx context.Context,
	plan domain.TransactionPlan,
	journalID domain.ID,
	deltas map[domain.ID]decimal.Decimal,
	accounts map[domain.ID]domain.Account,
) *appErrors.ErrorResponse {
	logger.LogCtx(ctx).Debugf("[TransactionCtrl:persistTransactionsAndBalances][plan: %+v]", plan)

	transactions, errTransactions := plan.Transactions(journalID)
	if errTransactions != nil {
		return svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[persistTransactionsAndBalances]%w", errTransactions)),
		)
	}

	if err := c.transactions.WithTxFromContext(ctx).CreateBatch(ctx, transactions); err != nil {
		return svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[persistTransactionsAndBalances]%w", err)),
		)
	}

	if err := c.applyBalanceDeltas(ctx, accounts, deltas); err != nil {
		err.SetCause(fmt.Errorf("[persistTransactionsAndBalances]%w", err.Cause))
		return err
	}

	return nil
}

// sortAccountLookups puts account lookups into the one order this service takes
// row locks on accounts in. Every path that locks accounts must use it.
//
// Two paths locking the same rows in two orders is a deadlock, and PostgreSQL
// resolves it by aborting one of them with SQLSTATE 40P01 — not something the
// caller chose or can steer. Rollback used to lock by primary key while this
// path locked by (owner, asset, type); the two agree only while account ids
// happen to be created in owner order, which lazily created accounts break.
//
// The key is (owner, asset, type), which is UNIQUE on accounts, so this is a
// total order. Primary key order would serve equally well in the abstract, but
// MakeTransaction has to resolve — and sometimes create — an account by this
// key before any id exists, so this is the order both paths can share.
func sortAccountLookups(lookups []domain.GetForUpdateParams) {
	sort.Slice(lookups, func(i, j int) bool {
		if lookups[i].OwnerID != lookups[j].OwnerID {
			return lookups[i].OwnerID < lookups[j].OwnerID
		}
		if lookups[i].Asset != lookups[j].Asset {
			return lookups[i].Asset.String() < lookups[j].Asset.String()
		}
		return lookups[i].Type < lookups[j].Type
	})
}

// lockAndBindAccounts locks and binds accounts to the transaction legs
func (c TransactionCtrl) lockAndBindAccounts(ctx context.Context, plan *domain.TransactionPlan) (map[domain.ID]domain.Account, *appErrors.ErrorResponse) {
	accountsByParams := make(map[domain.GetForUpdateParams]domain.Account, len(plan.Legs))
	uniqueLookups := make([]domain.GetForUpdateParams, 0, len(plan.Legs))

	for i := range plan.Legs {
		lookup := plan.Legs[i].AccountLookupParams(plan.AssetCode)
		if _, exists := accountsByParams[lookup]; !exists {
			// Placeholder marks lookup as already collected for deterministic locking.
			accountsByParams[lookup] = domain.Account{}
			uniqueLookups = append(uniqueLookups, lookup)
		}
	}

	// Acquire row locks in a deterministic order to avoid deadlocks
	// between concurrent transfers locking the same accounts in opposite directions.
	sortAccountLookups(uniqueLookups)

	accountsByID := make(map[domain.ID]domain.Account, len(plan.Legs))

	for i := range uniqueLookups {
		accountDB, errAccount := c.lockOrCreateAndLockAccount(ctx, uniqueLookups[i])
		if errAccount != nil {
			errAccount.SetCause(fmt.Errorf("[lockAndBindAccounts]%w", errAccount))
			return accountsByID, errAccount
		}
		accountsByParams[uniqueLookups[i]] = accountDB
		accountsByID[accountDB.ID] = accountDB
	}

	for i := range plan.Legs {
		lookup := plan.Legs[i].AccountLookupParams(plan.AssetCode)
		account, exists := accountsByParams[lookup]
		if !exists || account.ID == 0 {
			return nil, svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[lockAndBindAccounts][err: account not loaded for lookup %+v]", lookup)),
			)
		}
		if errBind := plan.Legs[i].BindAccount(account, plan.AssetCode); errBind != nil {
			return nil, svcErrors.New(
				appErrors.InvalidRequestError,
				appErrors.WithCause(fmt.Errorf("[lockAndBindAccounts]%w", errBind)),
			)
		}
	}

	return accountsByID, nil
}

// lockOrCreateAndLockAccount locks an account for update, creating it if necessary
func (c TransactionCtrl) lockOrCreateAndLockAccount(ctx context.Context, lookup domain.GetForUpdateParams) (domain.Account, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[TransactionCtrl:lockOrCreateAndLockAccount][lookup: %+v]", lookup)

	account, err := c.accounts.WithTxFromContext(ctx).GetForUpdate(ctx, lookup)
	if err != nil {
		if stdErrors.Is(err, domain.ErrRecordNotFound) {
			if lookup.OwnerID != 0 {
				ownerID := lookup.OwnerID
				if errCreate := c.accountCtrl.CreateAccount(ctx, &ownerID, lookup.Asset, lookup.Type); errCreate != nil {
					if er, ok := appErrors.AsErrorResponse(errCreate); ok {
						er.SetCause(fmt.Errorf("[lockOrCreateAndLockAccount]%w", er.Cause))
						return domain.Account{}, er
					}
					return domain.Account{}, svcErrors.New(
						appErrors.InternalServerError,
						appErrors.WithCause(fmt.Errorf("[lockOrCreateAndLockAccount]%w", errCreate)),
					)
				}
				account, err = c.accounts.WithTxFromContext(ctx).GetForUpdate(ctx, lookup)
				if err != nil {
					return domain.Account{}, svcErrors.New(
						appErrors.DatabaseError,
						appErrors.WithCause(fmt.Errorf("[lockOrCreateAndLockAccount]%w", err)),
					)
				}
			} else {
				return domain.Account{}, svcErrors.New(
					appErrors.RecordNotFoundError,
					appErrors.WithCause(fmt.Errorf("[lockOrCreateAndLockAccount]%w", err)),
				)
			}
		} else {
			return domain.Account{}, svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[lockOrCreateAndLockAccount]%w", err)),
			)
		}
	}

	if errIntegrity := c.integrity.Verify(account); errIntegrity != nil {
		return domain.Account{}, svcErrors.New(
			svcErrors.AccountIntegrityError,
			appErrors.WithCause(fmt.Errorf("[lockOrCreateAndLockAccount]%w", errIntegrity)),
		)
	}

	return account, nil
}

// applyBalanceDeltas applies balance deltas to locked accounts
func (c TransactionCtrl) applyBalanceDeltas(
	ctx context.Context,
	accounts map[domain.ID]domain.Account,
	deltas map[domain.ID]decimal.Decimal,
) *appErrors.ErrorResponse {
	for accountID, delta := range deltas {
		account, ok := accounts[accountID]
		if !ok {
			return svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[applyBalanceDeltas][err: account %d not locked]", accountID)),
			)
		}

		newBalance := account.Balance.Add(delta)
		newHash, errHash := c.integrity.Compute(account, newBalance)
		if errHash != nil {
			return svcErrors.New(
				appErrors.InternalServerError,
				appErrors.WithCause(fmt.Errorf("[applyBalanceDeltas]%w", errHash)),
			)
		}

		if errBalance := c.accounts.WithTxFromContext(ctx).UpdateBalance(ctx, domain.UpdateBalanceParams{
			AccountID:    accountID,
			NewBalance:   newBalance,
			ExpectedHash: account.IntegrityCheckValue,
			NewHash:      newHash,
		}); errBalance != nil {
			return svcErrors.New(
				appErrors.DatabaseError,
				appErrors.WithCause(fmt.Errorf("[applyBalanceDeltas]%w", errBalance)),
			)
		}

		account.Balance = newBalance
		account.IntegrityCheckValue = newHash
		accounts[accountID] = account
	}
	return nil
}

// generateIdempotencyKey generates a unique idempotency key based on transaction type and provided key
func generateIdempotencyKey(txType domain.JournalBizType, key string) *string {
	generated := fmt.Sprintf("%s_%s", txType, key)
	return &generated
}

// uniqueAccountIDsFromPostings extracts unique account IDs from transaction postings
func uniqueAccountIDsFromPostings(postings []domain.Transaction) []domain.ID {
	accountIDs := make([]domain.ID, 0, len(postings))
	seenAccounts := make(map[domain.ID]struct{})
	for i := range postings {
		if _, exists := seenAccounts[postings[i].AccountID]; !exists {
			seenAccounts[postings[i].AccountID] = struct{}{}
			accountIDs = append(accountIDs, postings[i].AccountID)
		}
	}

	return accountIDs
}

// updateJournalEntryIdempotencyKeyAndGetRollbackKey updates the idempotency key of a journal entry and returns a rollback key
func (c TransactionCtrl) updateJournalEntryIdempotencyKeyAndGetRollbackKey(
	ctx context.Context,
	entry *domain.JournalEntry,
) (*string, *appErrors.ErrorResponse) {
	if entry.ExternalIdempotencyKey == nil {
		return nil, nil
	}

	updatedIdempotencyKey := fmt.Sprintf("%s_%s", *entry.ExternalIdempotencyKey, tools.GenerateRandomString(domain.IdempotencySuffixLength))
	if err := c.journals.WithTxFromContext(ctx).UpdateIdempotencyKey(ctx, entry.ID, updatedIdempotencyKey); err != nil {
		return nil, svcErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(fmt.Errorf("[updateJournalEntryIdempotencyKeyAndGetRollbackKey]%w", err)),
		)
	}

	entry.ExternalIdempotencyKey = &updatedIdempotencyKey
	rollbackIdempotencyKey := fmt.Sprintf("%sR", updatedIdempotencyKey)
	return &rollbackIdempotencyKey, nil
}
