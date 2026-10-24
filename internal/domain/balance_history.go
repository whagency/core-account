package domain

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services/currency"
)

// NewBalanceHistory creates a new BalanceHistory for creating
func NewBalanceHistory(userID ID, assetCode currency.AssetCode, amount decimal.Decimal, rate decimal.Decimal) BalanceHistory {
	now := time.Now().UTC()
	return BalanceHistory{
		UserID:    userID,
		AssetCode: assetCode,
		Amount:    amount,
		Rate:      rate,
		StatDate:  time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
	}
}

// BalanceHistory represents the balance history data for the user.
type BalanceHistory struct {
	UserID    ID
	AssetCode currency.AssetCode
	Amount    decimal.Decimal
	Rate      decimal.Decimal
	StatDate  time.Time
	CreatedAt time.Time
	UpdatedAt *time.Time
}

// BalanceHistoryRepository defines the methods for persisting and retrieving balance history from storage.
type BalanceHistoryRepository interface {
	FindTodayStats(context.Context, *ID) ([]BalanceHistory, error)
	FindYesterdayStats(context.Context, *ID) ([]BalanceHistory, error)
	UpsertTodayStats(context.Context, []BalanceHistory) error

	WithTxFromContext(context.Context) BalanceHistoryRepository
}
