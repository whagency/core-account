package domain

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services/currency"
)

// TopUpUserAccountParams defines the input for local/dev-only balance top-up requests.
type TopUpUserAccountParams struct {
	UserID         ID
	AssetCode      currency.AssetCode
	Amount         decimal.Decimal
	IdempotencyKey *string
	Description    *string
}

// AmountQuantized converts a display-unit amount into the internal quantized representation.
func (p TopUpUserAccountParams) AmountQuantized() decimal.Decimal {
	return p.Amount.Shift(currency.DefaultAssetScale)
}

// TopUpUserAccountResult describes the ledger result of a local/dev top-up.
type TopUpUserAccountResult struct {
	JournalID ID
	Status    JournalStatus
}

// DevController defines local/dev-only helper use cases.
type DevController interface {
	TopUpUserAccount(context.Context, TopUpUserAccountParams) (TopUpUserAccountResult, error)
}
