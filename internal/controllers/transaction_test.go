package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"account-service/internal/domain"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/db"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/testkit/dbtest"
	"gorm.io/gorm"
)

func TestTransactionCtrlMakeTransaction_SuccessMovesBalancesAndCreatesPostings(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	idempotencyKey := "transfer-1"

	result, err := fixture.ctrl.MakeTransaction(context.Background(), domain.MakeTxParams{
		Type:           domain.JournalBizTypeTransfer,
		AssetCode:      currency.DefaultAccountAssetCode,
		UserID:         fixture.senderID,
		ToUserID:       &fixture.receiverID,
		Amount:         decimal.NewFromInt(100),
		Fee:            decimal.NewFromInt(5),
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.JournalStatusCompleted {
		t.Fatalf("unexpected transaction status: got %s, want %s", result.Status.String(), domain.JournalStatusCompleted.String())
	}
	if result.JournalID == 0 {
		t.Fatal("expected journal id")
	}
	if fixture.txManager.BeginCalls != 1 {
		t.Fatalf("unexpected begin calls: got %d, want 1", fixture.txManager.BeginCalls)
	}
	if fixture.txManager.CommitCalls != 1 {
		t.Fatalf("unexpected commit calls: got %d, want 1", fixture.txManager.CommitCalls)
	}
	if fixture.txManager.RollbackCalls != 0 {
		t.Fatalf("unexpected rollback calls: got %d, want 0", fixture.txManager.RollbackCalls)
	}

	journal, ok := fixture.journals.entries[result.JournalID]
	if !ok {
		t.Fatalf("journal %d not found", result.JournalID)
	}
	if journal.BizType != domain.JournalBizTypeTransfer {
		t.Fatalf("unexpected journal biz type: got %s, want %s", journal.BizType.String(), domain.JournalBizTypeTransfer.String())
	}
	if journal.ExternalIdempotencyKey == nil || *journal.ExternalIdempotencyKey != "transfer_transfer-1" {
		t.Fatalf("unexpected journal idempotency key: %+v", journal.ExternalIdempotencyKey)
	}

	postings := fixture.transactions.byJournal[result.JournalID]
	if len(postings) != 3 {
		t.Fatalf("unexpected postings count: got %d, want 3", len(postings))
	}

	assertPosting(t, postings[0], fixture.senderLiabilityAccountID, domain.TransactionTypeDebit, decimal.NewFromInt(100))
	assertPosting(t, postings[1], fixture.senderLiabilityAccountID, domain.TransactionTypeFee, decimal.NewFromInt(5))
	assertPosting(t, postings[2], fixture.receiverLiabilityAccountID, domain.TransactionTypeCredit, decimal.NewFromInt(100))

	assertBalance(t, fixture.accounts.accounts[fixture.senderLiabilityAccountID], decimal.NewFromInt(895))
	assertBalance(t, fixture.accounts.accounts[fixture.receiverLiabilityAccountID], decimal.NewFromInt(300))
}

func TestTransactionCtrlMakeTransaction_IdempotentReplayDoesNotMoveBalancesTwice(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	idempotencyKey := "transfer-replay"
	params := domain.MakeTxParams{
		Type:           domain.JournalBizTypeTransfer,
		AssetCode:      currency.DefaultAccountAssetCode,
		UserID:         fixture.senderID,
		ToUserID:       &fixture.receiverID,
		Amount:         decimal.NewFromInt(100),
		Fee:            decimal.NewFromInt(5),
		IdempotencyKey: &idempotencyKey,
	}

	first, err := fixture.ctrl.MakeTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected first error: %v", err)
	}

	second, err := fixture.ctrl.MakeTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected second error: %v", err)
	}

	if second.JournalID != first.JournalID {
		t.Fatalf("unexpected replay journal id: got %d, want %d", second.JournalID, first.JournalID)
	}
	if second.Status != domain.JournalStatusCompleted {
		t.Fatalf("unexpected replay status: got %s, want %s", second.Status.String(), domain.JournalStatusCompleted.String())
	}
	if fixture.txManager.CommitCalls != 1 {
		t.Fatalf("unexpected commit calls after replay: got %d, want 1", fixture.txManager.CommitCalls)
	}
	if len(fixture.journals.entries) != 1 {
		t.Fatalf("unexpected journal count after replay: got %d, want 1", len(fixture.journals.entries))
	}
	if len(fixture.transactions.byJournal[first.JournalID]) != 3 {
		t.Fatalf("unexpected postings count after replay: got %d, want 3", len(fixture.transactions.byJournal[first.JournalID]))
	}

	assertBalance(t, fixture.accounts.accounts[fixture.senderLiabilityAccountID], decimal.NewFromInt(895))
	assertBalance(t, fixture.accounts.accounts[fixture.receiverLiabilityAccountID], decimal.NewFromInt(300))
}

func TestTransactionCtrlMakeTransaction_ExistingTxDoesNotOwnCommit(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	ctx := db.NewTxInContext(context.Background(), &gorm.DB{})

	result, err := fixture.ctrl.MakeTransaction(ctx, domain.MakeTxParams{
		Type:      domain.JournalBizTypeTransfer,
		AssetCode: currency.DefaultAccountAssetCode,
		UserID:    fixture.senderID,
		ToUserID:  &fixture.receiverID,
		Amount:    decimal.NewFromInt(100),
		Fee:       decimal.NewFromInt(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JournalID == 0 {
		t.Fatal("expected journal id")
	}
	if fixture.txManager.BeginCalls != 0 {
		t.Fatalf("unexpected begin calls: got %d, want 0", fixture.txManager.BeginCalls)
	}
	if fixture.txManager.CommitCalls != 0 {
		t.Fatalf("unexpected commit calls: got %d, want 0", fixture.txManager.CommitCalls)
	}
	if fixture.txManager.RollbackCalls != 0 {
		t.Fatalf("unexpected rollback calls: got %d, want 0", fixture.txManager.RollbackCalls)
	}

	assertBalance(t, fixture.accounts.accounts[fixture.senderLiabilityAccountID], decimal.NewFromInt(895))
	assertBalance(t, fixture.accounts.accounts[fixture.receiverLiabilityAccountID], decimal.NewFromInt(300))
}

func TestTransactionCtrlRollbackTransaction_ReversesBalancesAndCreatesReversalJournal(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	idempotencyKey := "transfer-rollback"

	makeResult, err := fixture.ctrl.MakeTransaction(context.Background(), domain.MakeTxParams{
		Type:           domain.JournalBizTypeTransfer,
		AssetCode:      currency.DefaultAccountAssetCode,
		UserID:         fixture.senderID,
		ToUserID:       &fixture.receiverID,
		Amount:         decimal.NewFromInt(100),
		Fee:            decimal.NewFromInt(5),
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("unexpected make error: %v", err)
	}

	if err := fixture.ctrl.RollbackTransaction(context.Background(), makeResult.JournalID); err != nil {
		t.Fatalf("unexpected rollback error: %v", err)
	}

	if fixture.txManager.CommitCalls != 2 {
		t.Fatalf("unexpected commit calls: got %d, want 2", fixture.txManager.CommitCalls)
	}
	if fixture.txManager.RollbackCalls != 0 {
		t.Fatalf("unexpected rollback calls: got %d, want 0", fixture.txManager.RollbackCalls)
	}
	if len(fixture.journals.entries) != 2 {
		t.Fatalf("unexpected journal count after rollback: got %d, want 2", len(fixture.journals.entries))
	}

	original := fixture.journals.entries[makeResult.JournalID]
	if original.Status != domain.JournalStatusReversed {
		t.Fatalf("unexpected original journal status: got %s, want %s", original.Status.String(), domain.JournalStatusReversed.String())
	}
	if original.RelatedEntryID == nil || *original.RelatedEntryID == 0 {
		t.Fatal("expected original journal related entry id to be set")
	}
	if original.ExternalIdempotencyKey == nil || !strings.HasPrefix(*original.ExternalIdempotencyKey, "transfer_transfer-rollback_") {
		t.Fatalf("unexpected updated original idempotency key: %+v", original.ExternalIdempotencyKey)
	}

	reversal := fixture.journals.entries[*original.RelatedEntryID]
	if reversal.ReversalOfID == nil || *reversal.ReversalOfID != makeResult.JournalID {
		t.Fatalf("unexpected reversal link: %+v", reversal.ReversalOfID)
	}
	if reversal.ExternalIdempotencyKey == nil || !strings.HasSuffix(*reversal.ExternalIdempotencyKey, "R") {
		t.Fatalf("unexpected reversal idempotency key: %+v", reversal.ExternalIdempotencyKey)
	}

	reversalPostings := fixture.transactions.byJournal[reversal.ID]
	if len(reversalPostings) != 3 {
		t.Fatalf("unexpected reversal postings count: got %d, want 3", len(reversalPostings))
	}

	assertPosting(t, reversalPostings[0], fixture.senderLiabilityAccountID, domain.TransactionTypeCredit, decimal.NewFromInt(100))
	assertPosting(t, reversalPostings[1], fixture.senderLiabilityAccountID, domain.TransactionTypeFee, decimal.NewFromInt(5))
	assertPosting(t, reversalPostings[2], fixture.receiverLiabilityAccountID, domain.TransactionTypeDebit, decimal.NewFromInt(100))

	assertBalance(t, fixture.accounts.accounts[fixture.senderLiabilityAccountID], decimal.NewFromInt(1000))
	assertBalance(t, fixture.accounts.accounts[fixture.receiverLiabilityAccountID], decimal.NewFromInt(200))
}

func TestTransactionCtrlMakeTransaction_CommitFails_ReturnsError(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	fixture.txManager.CommitErr = errors.New("db commit failed")

	_, err := fixture.ctrl.MakeTransaction(context.Background(), domain.MakeTxParams{
		Type:      domain.JournalBizTypeTransfer,
		AssetCode: currency.DefaultAccountAssetCode,
		UserID:    fixture.senderID,
		ToUserID:  &fixture.receiverID,
		Amount:    decimal.NewFromInt(100),
		Fee:       decimal.NewFromInt(5),
	})
	if err == nil {
		t.Fatal("expected error on commit failure, got nil")
	}
	if fixture.txManager.BeginCalls != 1 {
		t.Fatalf("unexpected begin calls: got %d, want 1", fixture.txManager.BeginCalls)
	}
	if fixture.txManager.CommitCalls != 1 {
		t.Fatalf("unexpected commit calls: got %d, want 1", fixture.txManager.CommitCalls)
	}
}

func TestTransactionCtrlRollbackTransaction_JournalNotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	nonExistentID := domain.NewIDFromUint64(9999)

	err := fixture.ctrl.RollbackTransaction(context.Background(), nonExistentID)
	if err == nil {
		t.Fatal("expected error for non-existent journal, got nil")
	}
}

func TestTransactionCtrlRollbackTransaction_AlreadyReversedIsNoOp(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	idempotencyKey := "transfer-reversed"

	makeResult, err := fixture.ctrl.MakeTransaction(context.Background(), domain.MakeTxParams{
		Type:           domain.JournalBizTypeTransfer,
		AssetCode:      currency.DefaultAccountAssetCode,
		UserID:         fixture.senderID,
		ToUserID:       &fixture.receiverID,
		Amount:         decimal.NewFromInt(100),
		Fee:            decimal.NewFromInt(5),
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("unexpected make error: %v", err)
	}
	if err := fixture.ctrl.RollbackTransaction(context.Background(), makeResult.JournalID); err != nil {
		t.Fatalf("unexpected first rollback error: %v", err)
	}

	journalCount := len(fixture.journals.entries)
	rollbackCount := fixture.txManager.RollbackCalls
	commitCount := fixture.txManager.CommitCalls

	if err := fixture.ctrl.RollbackTransaction(context.Background(), makeResult.JournalID); err != nil {
		t.Fatalf("unexpected second rollback error: %v", err)
	}

	if len(fixture.journals.entries) != journalCount {
		t.Fatalf("unexpected journal count after second rollback: got %d, want %d", len(fixture.journals.entries), journalCount)
	}
	if fixture.txManager.RollbackCalls != rollbackCount {
		t.Fatalf("unexpected rollback calls after second rollback: got %d, want %d", fixture.txManager.RollbackCalls, rollbackCount)
	}
	if fixture.txManager.CommitCalls != commitCount+1 {
		t.Fatalf("unexpected commit calls after second rollback: got %d, want %d", fixture.txManager.CommitCalls, commitCount+1)
	}

	assertBalance(t, fixture.accounts.accounts[fixture.senderLiabilityAccountID], decimal.NewFromInt(1000))
	assertBalance(t, fixture.accounts.accounts[fixture.receiverLiabilityAccountID], decimal.NewFromInt(200))
}

func assertPosting(t *testing.T, tx domain.Transaction, accountID domain.ID, txType domain.TransactionType, amount decimal.Decimal) {
	t.Helper()
	if tx.AccountID != accountID {
		t.Fatalf("unexpected posting account id: got %d, want %d", tx.AccountID, accountID)
	}
	if tx.Type != txType {
		t.Fatalf("unexpected posting type: got %s, want %s", tx.Type.String(), txType.String())
	}
	if tx.Amount.Cmp(amount) != 0 {
		t.Fatalf("unexpected posting amount: got %s, want %s", tx.Amount.String(), amount.String())
	}
}

func assertBalance(t *testing.T, account domain.Account, want decimal.Decimal) {
	t.Helper()
	if account.Balance.Cmp(want) != 0 {
		t.Fatalf("unexpected account balance: got %s, want %s", account.Balance.String(), want.String())
	}
}

type transactionFixture struct {
	ctrl *TransactionCtrl

	accounts     *inMemoryAccountRepo
	journals     *inMemoryJournalRepo
	transactions *inMemoryTransactionRepo
	txManager    *dbtest.ObservableTxManager

	senderID                   domain.ID
	receiverID                 domain.ID
	senderLiabilityAccountID   domain.ID
	receiverLiabilityAccountID domain.ID
}

// newTransactionFixtureWithAccounts builds the fixture around caller-supplied
// accounts, so a test can control how account ids relate to owner ids.
func newTransactionFixtureWithAccounts(senderAccount, receiverAccount domain.Account, senderID, receiverID domain.ID) *transactionFixture {
	accounts := newInMemoryAccountRepo(senderAccount, receiverAccount)
	journals := newInMemoryJournalRepo()
	transactions := newInMemoryTransactionRepo()
	txManager := &dbtest.ObservableTxManager{}
	cache := &mockCacheController{}
	cache.getAssetFunc = func(_ context.Context, _ currency.AssetCode) (currency.Asset, error) {
		return currency.Asset{Code: currency.DefaultAccountAssetCode, Scale: currency.DefaultAssetScale}, nil
	}

	return &transactionFixture{
		ctrl: NewTransactionController(
			&mockAccountController{}, cache, accounts, journals, transactions,
			testIntegrityService{}, txManager,
		),
		accounts:                   accounts,
		journals:                   journals,
		transactions:               transactions,
		txManager:                  txManager,
		senderID:                   senderID,
		receiverID:                 receiverID,
		senderLiabilityAccountID:   senderAccount.ID,
		receiverLiabilityAccountID: receiverAccount.ID,
	}
}

func newTransactionFixture() *transactionFixture {
	senderID := domain.NewIDFromUint64(1001)
	receiverID := domain.NewIDFromUint64(2002)

	senderAccount := newTestAccount(domain.NewIDFromUint64(10), &senderID, currency.DefaultAccountAssetCode, domain.AccountTypeLiability, decimal.NewFromInt(1000))
	receiverAccount := newTestAccount(domain.NewIDFromUint64(20), &receiverID, currency.DefaultAccountAssetCode, domain.AccountTypeLiability, decimal.NewFromInt(200))

	accounts := newInMemoryAccountRepo(senderAccount, receiverAccount)
	journals := newInMemoryJournalRepo()
	transactions := newInMemoryTransactionRepo()
	txManager := &dbtest.ObservableTxManager{}
	integrity := testIntegrityService{}
	cache := &mockCacheController{}
	cache.getAssetFunc = func(_ context.Context, _ currency.AssetCode) (currency.Asset, error) {
		return currency.Asset{Code: currency.DefaultAccountAssetCode, Scale: currency.DefaultAssetScale}, nil
	}
	accountCtrl := &mockAccountController{}

	ctrl := NewTransactionController(
		accountCtrl,
		cache,
		accounts,
		journals,
		transactions,
		integrity,
		txManager,
	)

	return &transactionFixture{
		ctrl:                       ctrl,
		accounts:                   accounts,
		journals:                   journals,
		transactions:               transactions,
		txManager:                  txManager,
		senderID:                   senderID,
		receiverID:                 receiverID,
		senderLiabilityAccountID:   senderAccount.ID,
		receiverLiabilityAccountID: receiverAccount.ID,
	}
}

func newTestAccount(id domain.ID, ownerID *domain.ID, assetCode currency.AssetCode, accountType domain.AccountType, balance decimal.Decimal) domain.Account {
	return domain.Account{
		ID:                  id,
		OwnerID:             ownerID,
		AssetCode:           assetCode,
		Type:                accountType,
		Balance:             balance,
		IntegrityCheckValue: fmt.Sprintf("hash:%d:%s", id, balance.String()),
		CreatedAt:           time.Now().UTC(),
	}
}

type testIntegrityService struct{}

func (testIntegrityService) Compute(account domain.Account, balance decimal.Decimal) (string, error) {
	return fmt.Sprintf("hash:%d:%s", account.ID, balance.String()), nil
}

func (testIntegrityService) Verify(domain.Account) error {
	return nil
}

type inMemoryAccountRepo struct {
	accounts map[domain.ID]domain.Account

	// lockOrder records the account ids as GetForUpdate takes them, which is
	// what the deadlock regression test asserts on.
	lockOrder []domain.ID
}

func newInMemoryAccountRepo(accounts ...domain.Account) *inMemoryAccountRepo {
	result := &inMemoryAccountRepo{accounts: make(map[domain.ID]domain.Account, len(accounts))}
	for i := range accounts {
		result.accounts[accounts[i].ID] = accounts[i]
	}
	return result
}

func (r *inMemoryAccountRepo) CreateBatch(context.Context, []domain.Account) error {
	panic("unexpected call: CreateBatch")
}

func (r *inMemoryAccountRepo) GetForUpdate(_ context.Context, params domain.GetForUpdateParams) (domain.Account, error) {
	for _, account := range r.accounts {
		var ownerID domain.ID
		if account.OwnerID != nil {
			ownerID = *account.OwnerID
		}
		if ownerID == params.OwnerID && account.AssetCode == params.Asset && account.Type == params.Type {
			r.lockOrder = append(r.lockOrder, account.ID)
			return account, nil
		}
	}
	return domain.Account{}, domain.ErrRecordNotFound
}

func (r *inMemoryAccountRepo) UpdateBalance(_ context.Context, params domain.UpdateBalanceParams) error {
	account, ok := r.accounts[params.AccountID]
	if !ok {
		return domain.ErrRecordNotFound
	}
	if account.IntegrityCheckValue != params.ExpectedHash {
		return fmt.Errorf("unexpected integrity hash: got %s want %s", params.ExpectedHash, account.IntegrityCheckValue)
	}
	account.Balance = params.NewBalance
	account.IntegrityCheckValue = params.NewHash
	r.accounts[params.AccountID] = account
	return nil
}

func (r *inMemoryAccountRepo) GetBalancesByOwnerID(context.Context, *domain.ID) (domain.GetBalancesResult, error) {
	panic("unexpected call: GetBalancesByOwnerID")
}

func (r *inMemoryAccountRepo) GetAllPositiveUserBalanceSnapshots(context.Context, []domain.AccountType) ([]domain.PositiveBalanceSnapshot, error) {
	panic("unexpected call: GetAllPositiveUserBalanceSnapshots")
}

func (r *inMemoryAccountRepo) GetBalance(context.Context, domain.GetBalanceParams) (decimal.Decimal, error) {
	panic("unexpected call: GetBalance")
}

func (r *inMemoryAccountRepo) GetByIDsForUpdate(_ context.Context, ids []domain.ID) (map[domain.ID]domain.Account, error) {
	result := make(map[domain.ID]domain.Account, len(ids))
	for i := range ids {
		if account, ok := r.accounts[ids[i]]; ok {
			result[ids[i]] = account
		}
	}
	return result, nil
}

// GetByIDs mirrors GetByIDsForUpdate without the lock. RollbackTransaction uses
// it only to learn each account's (owner, asset, type) key before locking in
// the service-wide order, so the returned copies are never used for values.
func (r *inMemoryAccountRepo) GetByIDs(_ context.Context, ids []domain.ID) (map[domain.ID]domain.Account, error) {
	result := make(map[domain.ID]domain.Account, len(ids))
	for i := range ids {
		if account, ok := r.accounts[ids[i]]; ok {
			result[ids[i]] = account
		}
	}
	return result, nil
}

func (r *inMemoryAccountRepo) WithTxFromContext(context.Context) domain.AccountRepository {
	return r
}

type inMemoryJournalRepo struct {
	nextID  uint64
	entries map[domain.ID]domain.JournalEntry
	byKey   map[string]domain.ID
}

func newInMemoryJournalRepo() *inMemoryJournalRepo {
	return &inMemoryJournalRepo{
		nextID:  1,
		entries: make(map[domain.ID]domain.JournalEntry),
		byKey:   make(map[string]domain.ID),
	}
}

func (r *inMemoryJournalRepo) Create(_ context.Context, entry *domain.JournalEntry) error {
	entry.ID = domain.NewIDFromUint64(r.nextID)
	r.nextID++
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = &now
	if entry.Status == domain.JournalStatusCompleted {
		entry.CompletedAt = &now
	}
	r.entries[entry.ID] = *entry
	if entry.ExternalIdempotencyKey != nil {
		r.byKey[*entry.ExternalIdempotencyKey] = entry.ID
	}
	return nil
}

func (r *inMemoryJournalRepo) GetByIDForUpdate(_ context.Context, id domain.ID) (domain.JournalEntry, error) {
	entry, ok := r.entries[id]
	if !ok {
		return domain.JournalEntry{}, domain.ErrRecordNotFound
	}
	return entry, nil
}

func (r *inMemoryJournalRepo) GetByIdempotencyKey(_ context.Context, key string) (domain.JournalEntry, error) {
	id, ok := r.byKey[key]
	if !ok {
		return domain.JournalEntry{}, domain.ErrRecordNotFound
	}
	return r.entries[id], nil
}

func (r *inMemoryJournalRepo) UpdateIdempotencyKey(_ context.Context, id domain.ID, key string) error {
	entry, ok := r.entries[id]
	if !ok {
		return domain.ErrRecordNotFound
	}
	if entry.ExternalIdempotencyKey != nil {
		delete(r.byKey, *entry.ExternalIdempotencyKey)
	}
	entry.ExternalIdempotencyKey = &key
	now := time.Now().UTC()
	entry.UpdatedAt = &now
	r.entries[id] = entry
	r.byKey[key] = id
	return nil
}

func (r *inMemoryJournalRepo) MarkReversed(_ context.Context, id, relatedID domain.ID) error {
	entry, ok := r.entries[id]
	if !ok {
		return domain.ErrRecordNotFound
	}
	entry.Status = domain.JournalStatusReversed
	entry.RelatedEntryID = &relatedID
	now := time.Now().UTC()
	entry.UpdatedAt = &now
	r.entries[id] = entry
	return nil
}

func (r *inMemoryJournalRepo) WithTxFromContext(context.Context) domain.JournalEntryRepository {
	return r
}

type inMemoryTransactionRepo struct {
	nextID    uint64
	byJournal map[domain.ID][]domain.Transaction
}

func newInMemoryTransactionRepo() *inMemoryTransactionRepo {
	return &inMemoryTransactionRepo{
		nextID:    1,
		byJournal: make(map[domain.ID][]domain.Transaction),
	}
}

func (r *inMemoryTransactionRepo) CreateBatch(_ context.Context, postings []domain.Transaction) error {
	if len(postings) == 0 {
		return nil
	}
	created := make([]domain.Transaction, len(postings))
	for i := range postings {
		created[i] = postings[i]
		created[i].ID = domain.NewIDFromUint64(r.nextID)
		created[i].CreatedAt = time.Now().UTC()
		r.nextID++
	}
	r.byJournal[postings[0].JournalID] = append(r.byJournal[postings[0].JournalID], created...)
	return nil
}

func (r *inMemoryTransactionRepo) TransactionList(context.Context, domain.ID, domain.TransactionFilter) ([]domain.TransactionDetails, int64, error) {
	panic("unexpected call: TransactionList")
}

func (r *inMemoryTransactionRepo) GetByJournalID(_ context.Context, journalID domain.ID) ([]domain.Transaction, error) {
	return append([]domain.Transaction(nil), r.byJournal[journalID]...), nil
}

func (r *inMemoryTransactionRepo) WithTxFromContext(context.Context) domain.TransactionRepository {
	return r
}

// TestTransactionCtrlFindJournalByIdempotencyKey covers the read-only lookup used
// by compensation flows in pay-service.
//
// The critical property is that the lookup never creates a journal. MakeTransaction
// returns an existing entry for a matching key but creates one otherwise, so a
// caller trying to find out whether a posting exists would create the very posting
// it was asking about.
func TestTransactionCtrlFindJournalByIdempotencyKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// seed, when true, runs a MakeTransaction first so a journal exists.
		seed      bool
		lookupKey string
		// lookupType is the business type the caller claims the posting had.
		lookupType domain.JournalBizType
		wantFound  bool
	}{
		{
			name:       "existing key is found",
			seed:       true,
			lookupKey:  "checkout_payment:order-1",
			lookupType: domain.JournalBizTypeTransfer,
			wantFound:  true,
		},
		{
			name:       "unknown key is not found",
			lookupKey:  "checkout_payment:never-happened",
			lookupType: domain.JournalBizTypeTransfer,
			wantFound:  false,
		},
		{
			// The stored key is derived from type + external key, so the same
			// external key under a different type is a different journal. A caller
			// that guessed the type wrong must not get someone else's posting.
			name:       "same key under a different type is not found",
			seed:       true,
			lookupKey:  "checkout_payment:order-1",
			lookupType: domain.JournalBizTypeCheckoutPayment,
			wantFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newTransactionFixture()

			var seededJournalID domain.ID
			if tt.seed {
				key := "checkout_payment:order-1"
				res, err := fixture.ctrl.MakeTransaction(context.Background(), domain.MakeTxParams{
					Type:           domain.JournalBizTypeTransfer,
					AssetCode:      currency.DefaultAccountAssetCode,
					UserID:         fixture.senderID,
					ToUserID:       &fixture.receiverID,
					Amount:         decimal.NewFromInt(10),
					IdempotencyKey: &key,
				})
				if err != nil {
					t.Fatalf("seeding MakeTransaction failed: %v", err)
				}
				seededJournalID = res.JournalID
			}

			journalsBefore := len(fixture.journals.entries)

			got, err := fixture.ctrl.FindJournalByIdempotencyKey(context.Background(), domain.FindJournalParams{
				Type:           tt.lookupType,
				IdempotencyKey: tt.lookupKey,
			})
			if err != nil {
				t.Fatalf("FindJournalByIdempotencyKey returned error: %v", err)
			}

			if got.Found != tt.wantFound {
				t.Fatalf("expected found=%t, got %t", tt.wantFound, got.Found)
			}
			if tt.wantFound && got.JournalID != seededJournalID {
				t.Fatalf("expected journal id %d, got %d", seededJournalID, got.JournalID)
			}

			// The whole point of the RPC: looking is not writing.
			if after := len(fixture.journals.entries); after != journalsBefore {
				t.Fatalf("lookup created a journal: %d entries before, %d after", journalsBefore, after)
			}
		})
	}
}

// TestTransactionCtrlFindJournalByIdempotencyKey_ReversedIsNotFound pins the
// behaviour compensation depends on.
//
// RollbackTransaction renames the original key so the business operation can be
// re-attempted. A reversed posting is therefore invisible under its original key —
// which is the correct answer to "is there a live posting to compensate?".
func TestTransactionCtrlFindJournalByIdempotencyKey_ReversedIsNotFound(t *testing.T) {
	t.Parallel()

	fixture := newTransactionFixture()
	key := "checkout_payment:order-reversed"

	res, err := fixture.ctrl.MakeTransaction(context.Background(), domain.MakeTxParams{
		Type:           domain.JournalBizTypeTransfer,
		AssetCode:      currency.DefaultAccountAssetCode,
		UserID:         fixture.senderID,
		ToUserID:       &fixture.receiverID,
		Amount:         decimal.NewFromInt(10),
		IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("MakeTransaction returned error: %v", err)
	}

	found, err := fixture.ctrl.FindJournalByIdempotencyKey(context.Background(), domain.FindJournalParams{
		Type:           domain.JournalBizTypeTransfer,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("FindJournalByIdempotencyKey returned error: %v", err)
	}
	if !found.Found {
		t.Fatal("expected the posting to be found before rollback")
	}

	if errRollback := fixture.ctrl.RollbackTransaction(context.Background(), res.JournalID); errRollback != nil {
		t.Fatalf("RollbackTransaction returned error: %v", errRollback)
	}

	afterRollback, err := fixture.ctrl.FindJournalByIdempotencyKey(context.Background(), domain.FindJournalParams{
		Type:           domain.JournalBizTypeTransfer,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("FindJournalByIdempotencyKey returned error: %v", err)
	}
	if afterRollback.Found {
		t.Fatalf("expected a reversed posting to be invisible under its original key, got journal %d", afterRollback.JournalID)
	}
}

// ============================================================
// Lock ordering — deadlock regression
// ============================================================

// TestRollbackTransaction_LocksInSameOrderAsMakeTransaction is the deadlock
// regression.
//
// RollbackTransaction used to lock accounts by primary key while
// MakeTransaction locks by (owner, asset, type). Those two agree only while
// account ids happen to be created in owner order — which holds for accounts
// created at registration, and breaks for any account created lazily at first
// use. Once they disagree, a rollback running against a posting for the same
// pair of accounts takes the locks in the opposite order and PostgreSQL aborts
// one transaction with SQLSTATE 40P01.
//
// The fixture inverts the two orders deliberately: the lower owner id holds the
// higher account id, so locking by id and locking by owner produce opposite
// sequences. Asserting that both paths lock the same sequence is what catches a
// future path that reaches for GetByIDsForUpdate again.
func TestRollbackTransaction_LocksInSameOrderAsMakeTransaction(t *testing.T) {
	t.Parallel()

	senderID := domain.NewIDFromUint64(1001)
	receiverID := domain.NewIDFromUint64(2002)

	// Inverted on purpose: sender sorts first by owner, last by id.
	senderAccount := newTestAccount(domain.NewIDFromUint64(90), &senderID, currency.DefaultAccountAssetCode, domain.AccountTypeLiability, decimal.NewFromInt(1000))
	receiverAccount := newTestAccount(domain.NewIDFromUint64(10), &receiverID, currency.DefaultAccountAssetCode, domain.AccountTypeLiability, decimal.NewFromInt(200))

	fixture := newTransactionFixtureWithAccounts(senderAccount, receiverAccount, senderID, receiverID)

	idempotencyKey := "deadlock-order"
	makeResult, err := fixture.ctrl.MakeTransaction(context.Background(), domain.MakeTxParams{
		Type:           domain.JournalBizTypeTransfer,
		AssetCode:      currency.DefaultAccountAssetCode,
		UserID:         senderID,
		ToUserID:       &receiverID,
		Amount:         decimal.NewFromInt(100),
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("unexpected make error: %v", err)
	}

	makeOrder := append([]domain.ID(nil), fixture.accounts.lockOrder...)
	fixture.accounts.lockOrder = nil

	if err = fixture.ctrl.RollbackTransaction(context.Background(), makeResult.JournalID); err != nil {
		t.Fatalf("unexpected rollback error: %v", err)
	}
	rollbackOrder := fixture.accounts.lockOrder

	if len(makeOrder) == 0 || len(rollbackOrder) == 0 {
		t.Fatalf("expected both paths to take locks: make=%v rollback=%v", makeOrder, rollbackOrder)
	}

	// Guard the fixture itself: if ids ever stop disagreeing with owner order,
	// this test would pass without proving anything.
	if senderAccount.ID < receiverAccount.ID {
		t.Fatal("fixture no longer inverts id order against owner order")
	}

	if len(makeOrder) != len(rollbackOrder) {
		t.Fatalf("different number of locks: make=%v rollback=%v", makeOrder, rollbackOrder)
	}
	for i := range makeOrder {
		if makeOrder[i] != rollbackOrder[i] {
			t.Fatalf("lock order diverged — this is the deadlock:\n  MakeTransaction: %v\n  Rollback:        %v", makeOrder, rollbackOrder)
		}
	}
}

// TestSortAccountLookups_TotalOrder pins the comparator both paths share.
func TestSortAccountLookups_TotalOrder(t *testing.T) {
	t.Parallel()

	other := currency.NewAssetCode("AAAA")
	system := domain.GetForUpdateParams{Asset: currency.DefaultAccountAssetCode, Type: domain.AccountTypeLiability}
	userLowAsset := domain.GetForUpdateParams{OwnerID: domain.NewIDFromUint64(5), Asset: other, Type: domain.AccountTypeLiability}
	userLiability := domain.GetForUpdateParams{OwnerID: domain.NewIDFromUint64(5), Asset: currency.DefaultAccountAssetCode, Type: domain.AccountTypeLiability}
	userHold := domain.GetForUpdateParams{OwnerID: domain.NewIDFromUint64(5), Asset: currency.DefaultAccountAssetCode, Type: domain.AccountTypeHold}
	otherUser := domain.GetForUpdateParams{OwnerID: domain.NewIDFromUint64(9), Asset: currency.DefaultAccountAssetCode, Type: domain.AccountTypeLiability}

	lookups := []domain.GetForUpdateParams{otherUser, userHold, userLiability, userLowAsset, system}
	sortAccountLookups(lookups)

	// System accounts carry owner 0, so they sort first — which matches their
	// place in the id sequence too, since bootstrap creates them first.
	want := []domain.GetForUpdateParams{system, userLowAsset, userLiability, userHold, otherUser}
	for i := range want {
		if lookups[i] != want[i] {
			t.Fatalf("unexpected order at %d: got %+v, want %+v\nfull: %+v", i, lookups[i], want[i], lookups)
		}
	}
}
