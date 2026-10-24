package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"account-service/internal/domain"

	"github.com/shopspring/decimal"
)

// HMACAccountIntegrity provides HMAC-SHA256 based account integrity checks.
type HMACAccountIntegrity struct {
	secret []byte
}

// NewHMACAccountIntegrity builds a new account integrity service using the
// supplied secret. The secret must not be empty.
func NewHMACAccountIntegrity(secret string) (*HMACAccountIntegrity, error) {
	if secret == "" {
		return nil, fmt.Errorf("[security][NewHMACAccountIntegrity][err: secret must not be empty]")
	}

	return &HMACAccountIntegrity{secret: []byte(secret)}, nil
}

// Compute returns the integrity check value for the given account state.
func (h *HMACAccountIntegrity) Compute(account domain.Account, balance decimal.Decimal) (string, error) {
	payload := h.payload(account, balance)
	mac := hmac.New(sha256.New, h.secret)
	if _, err := mac.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("[security][HMACAccountIntegrity:Compute][err: %w]", err)
	}

	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Verify ensures the current integrity value matches the computed one.
func (h *HMACAccountIntegrity) Verify(account domain.Account) error {
	expected, err := h.Compute(account, account.Balance)
	if err != nil {
		return err
	}

	if account.IntegrityCheckValue == "" {
		return fmt.Errorf("[security][HMACAccountIntegrity:Verify][err: %w]", domain.ErrAccountIntegrityNotInitialized)
	}

	if !hmac.Equal([]byte(account.IntegrityCheckValue), []byte(expected)) {
		return fmt.Errorf("[security][HMACAccountIntegrity:Verify][err: %w]", domain.ErrAccountIntegrityViolation)
	}

	return nil
}

// payload constructs the HMAC payload string from the account and balance.
func (h *HMACAccountIntegrity) payload(account domain.Account, balance decimal.Decimal) string {
	owner := "system"
	if account.OwnerID != nil {
		owner = account.OwnerID.String()
	}

	return fmt.Sprintf("%s|%s|%d|%s", owner, account.AssetCode.String(), account.Type.Uint8(), balance.String())
}
