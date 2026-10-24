package domain

import (
	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services/currency"
)

type GetPnlResult struct {
	PnlTotalAmount  decimal.Decimal
	PnlTotalPercent string
	PnlAssets       map[currency.AssetCode]AssetPnlResult
}

type AssetPnlResult struct {
	PnlAmount  decimal.Decimal
	PnlPercent string
}
