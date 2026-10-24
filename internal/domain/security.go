package domain

import "github.com/shopspring/decimal"

import "errors"

var (
	ErrAccountIntegrityViolation      = errors.New("account integrity violation")
	ErrAccountIntegrityNotInitialized = errors.New("account integrity not initialised")
)

// AccountIntegrityService defines behaviour for computing and verifying
// integrity check values associated with account balances.
type AccountIntegrityService interface {
	// Compute returns the integrity check value for the provided account
	// assuming the supplied balance.
	Compute(Account, decimal.Decimal) (string, error)
	// Verify ensures the integrity check stored in the account matches the
	// expected value derived from its current state.
	Verify(Account) error
}
