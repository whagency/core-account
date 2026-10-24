package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/pagination"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/services/user"
)

// TransferParams defines the input parameters required to perform a transfer
// between users.
type TransferParams struct {
	User           user.User
	ToUserNumber   ID
	AssetCode      currency.AssetCode
	Amount         decimal.Decimal
	Description    *string
	IdempotencyKey *string
}

// UserID returns the ID of the user initiating the transfer
func (t TransferParams) UserID() ID {
	return NewIDFromUint64(t.User.ID)
}

// NullableUserID returns the ID of the user initiating the transfer as a pointer
func (t TransferParams) NullableUserID() *ID {
	return NewNullableIDFromUint64(&t.User.ID)
}

// AmountQuantized returns the amount shifted to quantized value.
func (t TransferParams) AmountQuantized() decimal.Decimal {
	return t.Amount.Shift(currency.DefaultAssetScale)
}

// TransferRequest represents a transfer transaction request
type TransferRequest struct {
	ID         ID
	UserFromID ID
	UserToID   ID
	AssetCode  currency.AssetCode
	Amount     decimal.Decimal
	Fee        decimal.Decimal
	CreatedAt  time.Time

	// internal fields
	ToUserNumber ID
}

// GetAmountInBaseUnits converts the amount from quants to base units
func (tr TransferRequest) GetAmountInBaseUnits() decimal.Decimal {
	return tr.Amount.Shift(-currency.DefaultAssetScale)
}

// TransferResult represents the outcome of a transfer operation.
type TransferResult struct {
	JournalID ID
	Status    JournalStatus
}

type TransferFilter struct {
	Limit     int
	Offset    int
	AssetCode *string
}

type TransferRequestListResult struct {
	List       []TransferRequest
	Pagination pagination.Pagination
}

// TransferController defines the business logic for processing transfers.
type TransferController interface {
	Init(context.Context, TransferParams) (TransferResult, error)
}

// TransferRequestRepository defines operations for transfer requests
type TransferRequestRepository interface {
	Create(context.Context, *TransferRequest) error
	TransferList(context.Context, ID, TransferFilter) ([]TransferRequest, int64, error)

	WithTxFromContext(context.Context) TransferRequestRepository
}
