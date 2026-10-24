package domain

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services/currency"
)

// AccountType represents the accounting classification of an account.
type AccountType uint8

const (
	AccountTypeLiability AccountType = iota + 1
	AccountTypeHold
	AccountTypeRevenue
	AccountTypeExpense
)

var _accountTypeToString = map[AccountType]string{
	AccountTypeLiability: "liability",
	AccountTypeHold:      "hold",
	AccountTypeRevenue:   "revenue",
	AccountTypeExpense:   "expense",
}

var _accountTypeByString = map[string]AccountType{
	"liability": AccountTypeLiability,
	"hold":      AccountTypeHold,
	"revenue":   AccountTypeRevenue,
	"expense":   AccountTypeExpense,
}

// NewAccountType creates a new AccountType from a uint8 value.
func NewAccountType(t uint8) AccountType {
	return AccountType(t)
}

// String returns the string representation of the AccountType.
func (a AccountType) String() string {
	if s, ok := _accountTypeToString[a]; ok {
		return s
	}
	return "unknown"
}

// Uint8 returns the uint8 representation of the AccountType.
func (a AccountType) Uint8() uint8 {
	return uint8(a)
}

// NewAccountTypeFromString converts a transport string to AccountType.
func NewAccountTypeFromString(value string) (AccountType, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	accountType, ok := _accountTypeByString[normalized]
	if ok {
		return accountType, nil
	}

	return 0, fmt.Errorf("[domain][NewAccountTypeFromString][err: unsupported account type %q]", value)
}

// IsSupportedAccountType reports whether the account type is supported.
func IsSupportedAccountType(accountType AccountType) bool {
	_, ok := _accountTypeToString[accountType]
	return ok
}

// SupportedAccountTypeStrings returns transport strings for supported account types.
func SupportedAccountTypeStrings() []string {
	return slices.Sorted(maps.Keys(_accountTypeByString))
}

// Account represents a ledger account that belongs to a client or the system.
type Account struct {
	ID                  ID
	OwnerID             *ID // nil if system account
	AssetCode           currency.AssetCode
	Type                AccountType
	Balance             decimal.Decimal
	IntegrityCheckValue string
	CreatedAt           time.Time
	UpdatedAt           *time.Time
}

// GetBalanceInBaseUnits returns the balance in base units according to the asset's scale
func (a Account) GetBalanceInBaseUnits() decimal.Decimal {
	return a.Balance.Shift(-currency.DefaultAssetScale)
}

var DefaultAccountTypesForUser = []AccountType{
	AccountTypeLiability,
	AccountTypeHold,
}

var DefaultAccountsForUser = map[currency.AssetCode][]AccountType{
	currency.DefaultAccountAssetCode: DefaultAccountTypesForUser,
}

var DefaultAccountTypesForSystem = []AccountType{
	AccountTypeLiability,
	AccountTypeHold,
	AccountTypeRevenue,
	AccountTypeExpense,
}

var DefaultAccountsForSystem = map[currency.AssetCode][]AccountType{
	currency.DefaultAccountAssetCode: DefaultAccountTypesForSystem,
}

// NewDefaultAccountsForUser creates a list of default accounts for a new user.
func NewDefaultAccountsForUser(userID ID) []Account {
	accounts := make([]Account, 0, len(DefaultAccountTypesForUser))
	for assetCode := range DefaultAccountsForUser {
		for i := range DefaultAccountsForUser[assetCode] {
			accounts = append(accounts, NewAccount(
				&userID,
				assetCode,
				DefaultAccountsForUser[assetCode][i],
			))
		}
	}
	return accounts
}

// NewDefaultSystemAccounts creates a list of default accounts for a system.
func NewDefaultSystemAccounts() []Account {
	accounts := make([]Account, 0, len(DefaultAccountTypesForSystem))
	for assetCode := range DefaultAccountsForSystem {
		for i := range DefaultAccountsForSystem[assetCode] {
			accounts = append(accounts, NewAccount(
				nil,
				assetCode,
				DefaultAccountsForSystem[assetCode][i],
			))
		}
	}
	return accounts
}

// NewAccount creates a new Account instance.
func NewAccount(ownerID *ID, assetCode currency.AssetCode, accType AccountType) Account {
	var id *ID
	if ownerID != nil {
		id = ownerID
	}
	return Account{
		OwnerID:   id,
		AssetCode: assetCode,
		Type:      accType,
		Balance:   decimal.Zero,
	}
}

type GetForUpdateParams struct {
	OwnerID ID
	Asset   currency.AssetCode
	Type    AccountType
}

// NewAccountParamsForUpdate creates parameters for fetching an account with a lock for update
func NewAccountParamsForUpdate(ownerID *ID, assetCode currency.AssetCode, accType AccountType) GetForUpdateParams {
	var id ID
	if ownerID != nil {
		id = *ownerID
	}
	return GetForUpdateParams{
		OwnerID: id,
		Asset:   assetCode,
		Type:    accType,
	}
}

type UpdateBalanceParams struct {
	AccountID    ID
	NewBalance   decimal.Decimal
	ExpectedHash string
	NewHash      string
}

type GetBalancesResult map[currency.AssetCode]map[AccountType]decimal.Decimal

type GetBalanceParams struct {
	OwnerID     *ID
	AssetCode   currency.AssetCode
	AccountType AccountType
}

type PositiveBalanceSnapshot struct {
	OwnerID     ID
	AssetCode   currency.AssetCode
	AccountType AccountType
	Balance     decimal.Decimal
}

// GetBalanceInBaseUnits returns the balance in base units according to the asset's scale
func (p PositiveBalanceSnapshot) GetBalanceInBaseUnits() decimal.Decimal {
	return p.Balance.Shift(-currency.DefaultAssetScale)
}

// AccountController defines the business logic for managing accounts and transactions.
type AccountController interface {
	CreateDefaultAccountsForUser(context.Context, ID) error
	CreateDefaultSystemAccounts(context.Context) error
	CreateAccount(context.Context, *ID, currency.AssetCode, AccountType) error
	GetBalancesByOwnerID(context.Context, *ID) (GetBalancesResult, error)
	GetPnlByOwnerID(context.Context, *ID) (GetPnlResult, error)
	UpdatePnlStat(context.Context) error
	GetBalance(context.Context, GetBalanceParams) (decimal.Decimal, error)
}

// AccountRepository defines the methods for persisting and retrieving accounts from storage.
type AccountRepository interface {
	CreateBatch(context.Context, []Account) error
	GetForUpdate(context.Context, GetForUpdateParams) (Account, error)
	UpdateBalance(context.Context, UpdateBalanceParams) error
	GetBalancesByOwnerID(context.Context, *ID) (GetBalancesResult, error)
	GetAllPositiveUserBalanceSnapshots(context.Context, []AccountType) ([]PositiveBalanceSnapshot, error)
	GetBalance(context.Context, GetBalanceParams) (decimal.Decimal, error)
	GetByIDsForUpdate(context.Context, []ID) (map[ID]Account, error)
	GetByIDs(context.Context, []ID) (map[ID]Account, error)

	WithTxFromContext(context.Context) AccountRepository
}
