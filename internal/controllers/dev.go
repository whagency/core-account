package controllers

import (
	"context"
	"fmt"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	commonTools "github.com/whagency/core-common/tools"
)

const _defaultTopUpUserAccountDescription = "Local dev top-up"

// DevCtrl performs local/dev-only helper use cases via regular business flows.
type DevCtrl struct {
	transactionCtrl domain.TransactionController
}

// NewDevController creates a new DevCtrl instance.
func NewDevController(transactionCtrl domain.TransactionController) *DevCtrl {
	return &DevCtrl{transactionCtrl: transactionCtrl}
}

// TopUpUserAccount credits a user's balance through the regular deposit journal type.
func (c DevCtrl) TopUpUserAccount(ctx context.Context, params domain.TopUpUserAccountParams) (domain.TopUpUserAccountResult, error) {
	logger.LogCtx(ctx).Debugf("[DevCtrl:TopUpUserAccount][params:%+v]", params)

	if params.UserID == 0 {
		return domain.TopUpUserAccountResult{}, svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[DevCtrl:TopUpUserAccount][err: user id is required]")),
			appErrors.WithValidationEntry("userId", appErrors.ValidationTagRequired, ""),
		)
	}

	if params.Amount.LessThanOrEqual(decimal.Zero) {
		return domain.TopUpUserAccountResult{}, svcErrors.New(
			appErrors.ValidationError,
			appErrors.WithCause(fmt.Errorf("[DevCtrl:TopUpUserAccount][err: amount must be greater than zero]")),
			appErrors.WithValidationEntry("amount", appErrors.ValidationTagGT, "0"),
		)
	}

	description := params.Description
	if description == nil {
		description = commonTools.TrimmedStringOrNil(_defaultTopUpUserAccountDescription)
	}

	result, err := c.transactionCtrl.MakeTransaction(ctx, domain.MakeTxParams{
		Type:           domain.JournalBizTypeDeposit,
		AssetCode:      params.AssetCode,
		UserID:         params.UserID,
		Amount:         params.AmountQuantized(),
		Fee:            decimal.Zero,
		IdempotencyKey: params.IdempotencyKey,
		Description:    description,
	})
	if err != nil {
		if er, ok := appErrors.AsErrorResponse(err); ok {
			er.SetCause(fmt.Errorf("[DevCtrl:TopUpUserAccount]%w", er.Cause))
			return domain.TopUpUserAccountResult{}, er
		}

		return domain.TopUpUserAccountResult{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[DevCtrl:TopUpUserAccount]%w", err)),
		)
	}

	return domain.TopUpUserAccountResult{
		JournalID: result.JournalID,
		Status:    result.Status,
	}, nil
}
