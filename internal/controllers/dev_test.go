package controllers

import (
	"context"
	"errors"
	"testing"

	"account-service/internal/domain"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/tools"
)

func TestDevCtrlTopUpUserAccountUsesDepositTransaction(t *testing.T) {
	t.Parallel()

	transactionCtrl := &mockDevTransactionController{
		result: domain.MakeTxResult{
			JournalID: domain.NewIDFromUint64(77),
			Status:    domain.JournalStatusCompleted,
		},
	}

	ctrl := NewDevController(transactionCtrl)
	result, err := ctrl.TopUpUserAccount(context.Background(), domain.TopUpUserAccountParams{
		UserID:         domain.NewIDFromUint64(123),
		AssetCode:      currency.DefaultAccountAssetCode,
		Amount:         decimal.RequireFromString("100.25"),
		IdempotencyKey: tools.ToPtr("dev-topup-123"),
		Description:    tools.ToPtr("Manual local funding"),
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if result.JournalID != domain.NewIDFromUint64(77) {
		t.Fatalf("unexpected journal id: got %d", result.JournalID)
	}
	if result.Status != domain.JournalStatusCompleted {
		t.Fatalf("unexpected status: got %s", result.Status.String())
	}

	if transactionCtrl.lastParams.Type != domain.JournalBizTypeDeposit {
		t.Fatalf("expected deposit biz type, got %s", transactionCtrl.lastParams.Type.String())
	}
	if transactionCtrl.lastParams.UserID != domain.NewIDFromUint64(123) {
		t.Fatalf("unexpected user id: got %d", transactionCtrl.lastParams.UserID)
	}
	if transactionCtrl.lastParams.ToUserID != nil {
		t.Fatalf("expected nil ToUserID, got %#v", transactionCtrl.lastParams.ToUserID)
	}
	if !transactionCtrl.lastParams.Amount.Equal(decimal.RequireFromString("10025000000")) {
		t.Fatalf("unexpected quantized amount: got %s", transactionCtrl.lastParams.Amount)
	}
	if !transactionCtrl.lastParams.Fee.Equal(decimal.Zero) {
		t.Fatalf("expected zero fee, got %s", transactionCtrl.lastParams.Fee)
	}
	if transactionCtrl.lastParams.IdempotencyKey == nil || *transactionCtrl.lastParams.IdempotencyKey != "dev-topup-123" {
		t.Fatalf("unexpected idempotency key: %#v", transactionCtrl.lastParams.IdempotencyKey)
	}
	if transactionCtrl.lastParams.Description == nil || *transactionCtrl.lastParams.Description != "Manual local funding" {
		t.Fatalf("unexpected description: %#v", transactionCtrl.lastParams.Description)
	}
}

func TestDevCtrlTopUpUserAccountUsesDefaultDescription(t *testing.T) {
	t.Parallel()

	transactionCtrl := &mockDevTransactionController{
		result: domain.MakeTxResult{
			JournalID: domain.NewIDFromUint64(1),
			Status:    domain.JournalStatusCompleted,
		},
	}

	ctrl := NewDevController(transactionCtrl)
	_, err := ctrl.TopUpUserAccount(context.Background(), domain.TopUpUserAccountParams{
		UserID:    domain.NewIDFromUint64(123),
		AssetCode: currency.DefaultAccountAssetCode,
		Amount:    decimal.RequireFromString("1"),
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if transactionCtrl.lastParams.Description == nil || *transactionCtrl.lastParams.Description != _defaultTopUpUserAccountDescription {
		t.Fatalf("unexpected default description: %#v", transactionCtrl.lastParams.Description)
	}
}

func TestDevCtrlTopUpUserAccountPropagatesTransactionError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	transactionCtrl := &mockDevTransactionController{err: wantErr}

	ctrl := NewDevController(transactionCtrl)
	_, err := ctrl.TopUpUserAccount(context.Background(), domain.TopUpUserAccountParams{
		UserID:    domain.NewIDFromUint64(123),
		AssetCode: currency.DefaultAccountAssetCode,
		Amount:    decimal.RequireFromString("1"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

type mockDevTransactionController struct {
	lastParams domain.MakeTxParams
	result     domain.MakeTxResult
	err        error
}

func (m *mockDevTransactionController) MakeTransaction(_ context.Context, params domain.MakeTxParams) (domain.MakeTxResult, error) {
	m.lastParams = params
	if m.err != nil {
		return domain.MakeTxResult{}, m.err
	}
	return m.result, nil
}

func (m *mockDevTransactionController) RollbackTransaction(context.Context, domain.ID) error {
	return nil
}

func (m *mockDevTransactionController) FindJournalByIdempotencyKey(
	context.Context,
	domain.FindJournalParams,
) (domain.FindJournalResult, error) {
	return domain.FindJournalResult{}, nil
}
