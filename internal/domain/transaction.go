package domain

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/pagination"
	"github.com/whagency/core-common/services/currency"
)

const IdempotencySuffixLength = 3

// FeeMode describes how the fee should be applied for a transaction.
type FeeMode uint8

const (
	FeeModeAdd FeeMode = iota + 1
	FeeModeDeduct
	FeeModeSeparate
)

// NewFeeMode creates a new FeeMode from a uint8 value.
func NewFeeMode(mode uint8) FeeMode {
	return FeeMode(mode)
}

// OperationSign describes how an amount affects the account balance.
type OperationSign int8

const (
	Credit OperationSign = +1
	Debit  OperationSign = -1
)

// String returns the string representation of the OperationSign.
func (op OperationSign) String() string {
	switch op {
	case Credit:
		return "credit"
	case Debit:
		return "debit"
	default:
		return "unknown"
	}
}

// Int8 returns the int8 representation of the OperationSign.
func (op OperationSign) Int8() int8 {
	return int8(op)
}

// Int32 returns the int32 representation of the OperationSign.
func (op OperationSign) Int32() int32 {
	return int32(op)
}

// Decimal returns the decimal representation of the OperationSign.
func (op OperationSign) Decimal() decimal.Decimal {
	return decimal.NewFromInt32(op.Int32())
}

// MakeTxParams contains data required to perform a transaction.
type MakeTxParams struct {
	Type           JournalBizType
	AssetCode      currency.AssetCode
	UserID         ID
	ToUserID       *ID
	Amount         decimal.Decimal
	Fee            decimal.Decimal
	IdempotencyKey *string
	Description    *string
}

// MakeTxResult describes the result of a MakeTransaction call.
type MakeTxResult struct {
	JournalID ID
	Status    JournalStatus
}

// FindJournalParams identifies a journal entry by the caller-supplied
// idempotency key. Type is required because the stored key is derived from both.
type FindJournalParams struct {
	Type           JournalBizType
	IdempotencyKey string
}

// FindJournalResult reports whether a live journal exists for the key.
//
// Found is false both when the operation never happened and when it was already
// rolled back — RollbackTransaction renames the key so the business operation can
// be re-attempted. Both answers mean the same thing to a caller asking whether
// there is a posting to compensate: there is not.
type FindJournalResult struct {
	Found     bool
	JournalID ID
	Status    JournalStatus
}

// TxLeg describes a single leg of a transaction.
type TxLeg struct {
	Role        TxLegRole
	AccountType AccountType
	AmountFrom  AmountFrom
	Sign        OperationSign

	accountID ID // filled in during processing
	amount    decimal.Decimal
}

// GetTransactionType returns the TransactionType based on the leg's properties.
func (t *TxLeg) GetTransactionType() TransactionType {
	if t.AmountFrom == AmountFromFee {
		return TransactionTypeFee
	}
	if t.Sign == Credit {
		return TransactionTypeCredit
	}
	return TransactionTypeDebit
}

// SetAccountID sets the account ID for the transaction leg.
func (t *TxLeg) SetAccountID(id ID) {
	t.accountID = id
}

// GetAccountID returns the account ID for the transaction leg.
func (t *TxLeg) GetAccountID() ID {
	return t.accountID
}

// GetAmount returns the amount for the transaction leg.
func (t *TxLeg) GetAmount() decimal.Decimal {
	return t.amount
}

// SetAmount sets the amount for the transaction leg.
func (t *TxLeg) SetAmount(amount decimal.Decimal) {
	t.amount = amount
}

type TxSpec struct {
	Type     JournalBizType
	UsesFee  bool
	FeeMode  FeeMode
	Legs     []TxLeg
	Validate func(in MakeTxParams) error
}

// TransactionPlan represents a fully materialised transaction specification.
//
// The plan is built from the combination of a TxSpec and MakeTxParams. It
// contains the postings that need to be persisted and the balance deltas that
// should be applied to each account. Before the plan can be executed each leg
// must be bound to a concrete account via BindAccount.
type TransactionPlan struct {
	BizType        JournalBizType
	AssetCode      currency.AssetCode
	IdempotencyKey *string
	Description    *string
	Amounts        TransactionAmounts
	Legs           []TransactionLegPlan
}

// TransactionLegPlan contains the resolved information for a single posting of
// a transaction. The AccountID is populated once the controller locks and binds
// the concrete account for the leg.
type TransactionLegPlan struct {
	Role            TxLegRole
	OwnerID         *ID
	AccountType     AccountType
	TransactionType TransactionType
	AmountFrom      AmountFrom
	Amount          decimal.Decimal
	Delta           decimal.Decimal

	accountID ID
}

// AccountID returns the identifier of the account bound to the leg. The bool
// result is false when the leg has not yet been bound.
func (l *TransactionLegPlan) AccountID() (ID, bool) {
	if l.accountID == 0 {
		return 0, false
	}
	return l.accountID, true
}

// AccountLookupParams returns the parameters required to lock the account that
// should back this leg.
func (l *TransactionLegPlan) AccountLookupParams(asset currency.AssetCode) GetForUpdateParams {
	return NewAccountParamsForUpdate(l.OwnerID, asset, l.AccountType)
}

// BindAccount associates the provided account with the transaction leg. The
// method performs defensive validations to ensure the account matches the
// resolved owner, asset and account type for the leg.
func (l *TransactionLegPlan) BindAccount(account Account, asset currency.AssetCode) error {
	if l.accountID != 0 && l.accountID != account.ID {
		return fmt.Errorf("[domain][TransactionLegPlan:BindAccount][err: leg already bound to account %d]", l.accountID)
	}

	if account.AssetCode != asset {
		return fmt.Errorf("[domain][TransactionLegPlan:BindAccount][err: account %d asset mismatch: expected %s got %s]", account.ID, asset, account.AssetCode)
	}

	if account.Type != l.AccountType {
		return fmt.Errorf("[domain][TransactionLegPlan:BindAccount][err: account %d type mismatch: expected %d got %d]", account.ID, l.AccountType, account.Type)
	}

	switch {
	case l.OwnerID == nil && account.OwnerID != nil:
		return fmt.Errorf("[domain][TransactionLegPlan:BindAccount][err: account %d is client owned but system account required]", account.ID)
	case l.OwnerID != nil && account.OwnerID == nil:
		return fmt.Errorf("[domain][TransactionLegPlan:BindAccount][err: account %d is system owned but client account required]", account.ID)
	case l.OwnerID != nil && account.OwnerID != nil && *account.OwnerID != *l.OwnerID:
		return fmt.Errorf(
			"[domain][TransactionLegPlan:BindAccount][err: account %d owner mismatch: expected %d got %d]",
			account.ID,
			uint64(*l.OwnerID),
			uint64(*account.OwnerID),
		)
	}

	l.accountID = account.ID
	return nil
}

// BalanceDelta returns the signed delta that should be applied to the bound
// account balance.
func (l *TransactionLegPlan) BalanceDelta() (decimal.Decimal, error) {
	if l.accountID == 0 {
		return decimal.Decimal{}, fmt.Errorf("transaction leg is not bound to an account")
	}

	return l.Delta, nil
}

// Assemble creates a TransactionPlan from the specification and input
// parameters. It validates the input, computes the net/fee amounts and produces
// the ledger postings with their balance deltas. The quantizer is responsible
// for enforcing asset scale constraints.
func (spec TxSpec) Assemble(in MakeTxParams) (TransactionPlan, error) {
	if spec.Validate != nil {
		if err := spec.Validate(in); err != nil {
			return TransactionPlan{}, fmt.Errorf("[domain][TxSpec:Assemble][validate][err: %w]", err)
		}
	}

	amounts, err := spec.computeTransactionAmounts(in)
	if err != nil {
		return TransactionPlan{}, fmt.Errorf("[domain][TxSpec:Assemble]%w", err)
	}

	plan := TransactionPlan{
		BizType:        spec.Type,
		AssetCode:      in.AssetCode,
		IdempotencyKey: in.IdempotencyKey,
		Description:    in.Description,
		Amounts:        amounts,
		Legs:           make([]TransactionLegPlan, 0, len(spec.Legs)),
	}

	for i := range spec.Legs {
		baseAmount := amounts.AmountFor(spec.Legs[i].AmountFrom)
		if baseAmount.IsZero() {
			continue
		}
		signedAmount := baseAmount.Mul(spec.Legs[i].Sign.Decimal())

		ownerID, errResolve := resolveAccountOwner(in, spec.Legs[i].Role)
		if errResolve != nil {
			return TransactionPlan{}, fmt.Errorf("[domain][TxSpec:Assemble]%w", err)
		}

		plan.Legs = append(plan.Legs, TransactionLegPlan{
			Role:            spec.Legs[i].Role,
			OwnerID:         ownerID,
			AccountType:     spec.Legs[i].AccountType,
			TransactionType: spec.Legs[i].GetTransactionType(),
			AmountFrom:      spec.Legs[i].AmountFrom,
			Amount:          signedAmount.Abs(),
			Delta:           signedAmount,
		})
	}

	if len(plan.Legs) == 0 {
		return TransactionPlan{}, fmt.Errorf("[domain][TxSpec:Assemble][err: no transaction legs generated]")
	}

	return plan, nil
}

// JournalEntryForCreation builds the journal entry skeleton that should be
// persisted for the plan.
func (p TransactionPlan) JournalEntryForCreation() *JournalEntry {
	return &JournalEntry{
		ExternalIdempotencyKey: p.IdempotencyKey,
		BizType:                p.BizType,
		Status:                 JournalStatusCompleted,
		Description:            p.Description,
	}
}

// Transactions converts the plan into a slice of domain transactions ready to
// be persisted. All legs must be bound prior to calling this method.
func (p TransactionPlan) Transactions(journalID ID) ([]Transaction, error) {
	transactions := make([]Transaction, len(p.Legs))
	for i := range p.Legs {
		accountID, ok := p.Legs[i].AccountID()
		if !ok {
			return nil, fmt.Errorf("[domain][TransactionPlan:Transactions][err: leg %d is not bound to an account]", i)
		}

		transactions[i] = Transaction{
			JournalID: journalID,
			AccountID: accountID,
			AssetCode: p.AssetCode,
			Type:      p.Legs[i].TransactionType,
			Amount:    p.Legs[i].Amount,
		}
	}

	return transactions, nil
}

// BalanceDeltas aggregates the per-account balance deltas derived from the
// transaction legs. All legs must be bound.
func (p TransactionPlan) BalanceDeltas() (map[ID]decimal.Decimal, error) {
	deltas := make(map[ID]decimal.Decimal)
	for i := range p.Legs {
		accountID, ok := p.Legs[i].AccountID()
		if !ok {
			return nil, fmt.Errorf("[domain][TransactionPlan:BalanceDeltas][err: leg %d is not bound to an account]", i)
		}

		if current, exists := deltas[accountID]; exists {
			deltas[accountID] = current.Add(p.Legs[i].Delta)
			continue
		}

		deltas[accountID] = p.Legs[i].Delta
	}

	return deltas, nil
}

// CheckResultingBalances ensures that the resulting balance after applying the
// plan would not drive any account negative. The provided map must contain all
// accounts referenced in the plan keyed by their identifier.
func (p TransactionPlan) CheckResultingBalances(accounts map[ID]Account, deltas map[ID]decimal.Decimal) error {
	for accountID, delta := range deltas {
		account, exists := accounts[accountID]
		if !exists {
			return fmt.Errorf("[domain][TransactionPlan:CheckResultingBalances][err: missing account %d]", accountID)
		}

		if account.OwnerID != nil && account.Balance.Add(delta).IsNegative() {
			return fmt.Errorf("[domain][TransactionPlan:CheckResultingBalances][err: account %d would go negative]", accountID)
		}
	}

	return nil
}

type AmountFrom uint8

const (
	AmountFromTotal AmountFrom = iota + 1
	AmountFromNet
	AmountFromFee
)

type TxLegRole uint8

const (
	LegClientSender   TxLegRole = iota + 1 // The client who pays (the sender)
	LegClientReceiver                      // The client who receives (the receiver)
	LegSystem                              // The system account (for internal transfers)
)

// resolveAccountOwner determines the owner of an account based on the leg role and input parameters
func resolveAccountOwner(in MakeTxParams, role TxLegRole) (*ID, error) {
	switch role {
	case LegClientSender:
		id := in.UserID
		return &id, nil
	case LegClientReceiver:
		return in.ToUserID, nil
	case LegSystem:
		return nil, nil
	default:
		return nil, fmt.Errorf("[resolveAccountOwner][err: unsupported leg role %d]", role)
	}
}

var (
	ErrMissingToUserID  = errors.New("missing ToUserID")
	ErrSameUserTransfer = errors.New("cannot transfer to the same user")
)

// validateTransfer performs additional validation specific to transfer transactions
func validateTransfer(in MakeTxParams) error {
	if in.ToUserID == nil {
		return ErrMissingToUserID
	}
	if in.UserID == *in.ToUserID {
		return ErrSameUserTransfer
	}
	return nil
}

var TxRegistry = map[JournalBizType]TxSpec{
	JournalBizTypeDeposit: {
		Type:    JournalBizTypeDeposit,
		UsesFee: true,
		FeeMode: FeeModeSeparate,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Credit},
			{Role: LegClientSender, AmountFrom: AmountFromFee, AccountType: AccountTypeLiability, Sign: Debit},
		},
	},
	JournalBizTypeWithdrawalRequest: {
		Type: JournalBizTypeWithdrawalRequest,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Debit},
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeHold, Sign: Credit},
		},
	},
	JournalBizTypeWithdrawalComplete: {
		Type:    JournalBizTypeWithdrawalComplete,
		UsesFee: true,
		FeeMode: FeeModeDeduct,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromNet, AccountType: AccountTypeHold, Sign: Debit},
			{Role: LegClientSender, AmountFrom: AmountFromFee, AccountType: AccountTypeHold, Sign: Debit},
		},
	},
	JournalBizTypeWithdrawalReject: {
		Type: JournalBizTypeWithdrawalReject,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeHold, Sign: Debit},
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Credit},
		},
	},
	JournalBizTypeTransfer: {
		Type:    JournalBizTypeTransfer,
		UsesFee: true,
		FeeMode: FeeModeAdd,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Debit},
			{Role: LegClientSender, AmountFrom: AmountFromFee, AccountType: AccountTypeLiability, Sign: Debit},
			{Role: LegClientReceiver, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Credit},
		},
		Validate: validateTransfer,
	},
	JournalBizTypeCheckoutPayment: {
		Type:    JournalBizTypeCheckoutPayment,
		UsesFee: true,
		FeeMode: FeeModeDeduct,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Debit},
			{Role: LegClientReceiver, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Credit},
			{Role: LegClientReceiver, AmountFrom: AmountFromFee, AccountType: AccountTypeLiability, Sign: Debit},
		},
		Validate: validateTransfer,
	},
	JournalBizTypeCheckoutPaymentHold: {
		Type: JournalBizTypeCheckoutPaymentHold,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromTotal, AccountType: AccountTypeLiability, Sign: Debit},
			{Role: LegClientReceiver, AmountFrom: AmountFromTotal, AccountType: AccountTypeHold, Sign: Credit},
		},
		Validate: validateTransfer,
	},
	JournalBizTypeCheckoutPaymentHoldRelease: {
		Type:    JournalBizTypeCheckoutPaymentHoldRelease,
		UsesFee: true,
		FeeMode: FeeModeDeduct,
		Legs: []TxLeg{
			{Role: LegClientSender, AmountFrom: AmountFromNet, AccountType: AccountTypeHold, Sign: Debit},
			{Role: LegClientSender, AmountFrom: AmountFromFee, AccountType: AccountTypeHold, Sign: Debit},
			{Role: LegClientSender, AmountFrom: AmountFromNet, AccountType: AccountTypeLiability, Sign: Credit},
		},
	},
}

type TransactionAmounts struct {
	Total decimal.Decimal
	Net   decimal.Decimal
	Fee   decimal.Decimal
}

// AmountFor returns the appropriate amount based on the AmountFrom source
func (a TransactionAmounts) AmountFor(source AmountFrom) decimal.Decimal {
	switch source {
	case AmountFromTotal:
		return a.Total
	case AmountFromNet:
		return a.Net
	case AmountFromFee:
		return a.Fee
	default:
		return decimal.Zero
	}
}

// computeTransactionAmounts calculates the total, net and fee amounts based on the input parameters and transaction specification.
func (spec TxSpec) computeTransactionAmounts(in MakeTxParams) (TransactionAmounts, error) {
	if in.Amount.LessThanOrEqual(decimal.Zero) {
		return TransactionAmounts{}, fmt.Errorf("[computeTransactionAmounts][err: amount must be positive]")
	}

	amounts := TransactionAmounts{
		Total: in.Amount,
		Net:   in.Amount,
		Fee:   decimal.Zero,
	}

	if spec.UsesFee {
		if in.Fee.IsNegative() {
			return TransactionAmounts{}, fmt.Errorf("[computeTransactionAmounts][err: fee cannot be negative]")
		}

		amounts.Fee = in.Fee
		switch spec.FeeMode {
		case FeeModeSeparate, FeeModeDeduct:
			amounts.Net = in.Amount.Sub(in.Fee)
		case FeeModeAdd:
			amounts.Net = in.Amount
		default:
			return TransactionAmounts{}, fmt.Errorf("[computeTransactionAmounts][err: unsupported fee mode %d]", spec.FeeMode)
		}

		if amounts.Net.LessThan(decimal.Zero) {
			return TransactionAmounts{}, fmt.Errorf("[computeTransactionAmounts][err: net amount cannot be negative]")
		}
	} else if !in.Fee.IsZero() {
		return TransactionAmounts{}, fmt.Errorf("[computeTransactionAmounts][err: fee not applicable for this transaction type]")
	}

	return amounts, nil
}

type TransactionType uint8

const (
	TransactionTypeCredit TransactionType = iota + 1
	TransactionTypeDebit
	TransactionTypeFee
)

var _transactionTypeToString = map[TransactionType]string{
	TransactionTypeCredit: "credit",
	TransactionTypeDebit:  "debit",
	TransactionTypeFee:    "fee",
}

var _transactionTypeByString = map[string]TransactionType{
	"credit": TransactionTypeCredit,
	"debit":  TransactionTypeDebit,
	"fee":    TransactionTypeFee,
}

// NewTransactionType converts a stored uint8 to a TransactionType.
func NewTransactionType(t uint8) TransactionType {
	return TransactionType(t)
}

// Uint8 returns the uint8 representation of the TransactionType.
func (t TransactionType) Uint8() uint8 {
	return uint8(t)
}

// String returns the string representation of the TransactionType.
func (t TransactionType) String() string {
	if val, ok := _transactionTypeToString[t]; ok {
		return val
	}
	return "unknown"
}

// NewTransactionTypeFromString converts a transport string to TransactionType.
func NewTransactionTypeFromString(value string) (TransactionType, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	transactionType, ok := _transactionTypeByString[normalized]
	if ok {
		return transactionType, nil
	}

	return 0, fmt.Errorf("[domain][NewTransactionTypeFromString][err: unsupported transaction type %q]", value)
}

// IsSupportedTransactionType reports whether the transaction type is supported.
func IsSupportedTransactionType(transactionType TransactionType) bool {
	_, ok := _transactionTypeToString[transactionType]
	return ok
}

// SupportedTransactionTypeStrings returns transport strings for supported transaction types.
func SupportedTransactionTypeStrings() []string {
	return slices.Sorted(maps.Keys(_transactionTypeByString))
}

// SignedAmount returns the signed amount delta based on the transaction type.
// Credit increases the balance, Debit decreases it. Fees are treated as debits.
func (t TransactionType) SignedAmount(amount decimal.Decimal) decimal.Decimal {
	switch t {
	case TransactionTypeCredit:
		return amount
	case TransactionTypeDebit, TransactionTypeFee:
		return amount.Neg()
	default:
		return decimal.Zero
	}
}

type Transaction struct {
	ID        ID
	JournalID ID
	AccountID ID
	Type      TransactionType
	AssetCode currency.AssetCode
	Amount    decimal.Decimal
	CreatedAt time.Time
}

type TransactionDetails struct {
	ID        ID
	OwnerID   ID
	Type      TransactionType
	AssetCode currency.AssetCode
	Amount    decimal.Decimal
	Status    JournalStatus
	BizType   JournalBizType
	AcctType  AccountType
	CreatedAt time.Time
}

type TransactionListResult struct {
	List       []TransactionDetails
	Pagination pagination.Pagination
}

type TransactionFilter struct {
	Limit       int
	Offset      int
	AssetCode   *string
	Status      *JournalStatus
	Type        *TransactionType
	EntryType   *JournalBizType
	AccountType *AccountType
}

// TransactionController defines operations for managing transactions.
type TransactionController interface {
	MakeTransaction(context.Context, MakeTxParams) (MakeTxResult, error)
	RollbackTransaction(context.Context, ID) error
	FindJournalByIdempotencyKey(context.Context, FindJournalParams) (FindJournalResult, error)
}

// TransactionRepository defines operations for storing transactions.
type TransactionRepository interface {
	CreateBatch(context.Context, []Transaction) error
	TransactionList(context.Context, ID, TransactionFilter) ([]TransactionDetails, int64, error)
	GetByJournalID(context.Context, ID) ([]Transaction, error)

	WithTxFromContext(context.Context) TransactionRepository
}
