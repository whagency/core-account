package domain

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services"
	"github.com/whagency/core-common/services/restriction"
)

var MinDepositAmount = decimal.NewFromInt(100000000) // 1 USDT with 8 decimals

// RestrictionService defines the interface for restriction-related operations.
type RestrictionService interface {
	CheckLimit(context.Context, restriction.CheckLimitParams) (restriction.CheckLimitResult, error)
	CountersUpdate(context.Context, restriction.CounterUpdateParams) error
	SetRequiredVerification(context.Context, uint64, bool) error
}

// RestrictionGRPCService defines the interface for restriction-related operations.
type RestrictionGRPCService interface {
	services.GRPCClientBase
	RestrictionService
}
