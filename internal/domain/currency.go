package domain

import (
	"context"

	"github.com/whagency/core-common/services/currency"
)

// CurrencyService defines the interface for interacting with the currency service.
type CurrencyService interface {
	GetAssetMap(context.Context) (currency.AssetMap, error)
	GetRateMap(context.Context) (currency.RateMap, error)
}
