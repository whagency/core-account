package account

import (
	"strings"
	"testing"

	"account-service/internal/domain"
	"account-service/internal/transport/grpc/pb/accountv1"
)

func TestDecodeMakeTransactionRequest_TypeMapping(t *testing.T) {
	tests := []struct {
		name     string
		input    accountv1.JournalBizType
		expected domain.JournalBizType
	}{
		{
			name:     "checkout payment maps to domain checkout payment",
			input:    accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT,
			expected: domain.JournalBizTypeCheckoutPayment,
		},
		{
			name:     "checkout payment hold maps to domain checkout payment hold",
			input:    accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT_HOLD,
			expected: domain.JournalBizTypeCheckoutPaymentHold,
		},
		{
			name:     "checkout payment release maps to domain checkout payment release",
			input:    accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT_HOLD_RELEASE,
			expected: domain.JournalBizTypeCheckoutPaymentHoldRelease,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := decodeMakeTransactionRequest(&accountv1.MakeTransactionRequest{
				Type:      tt.input,
				UserId:    1,
				AssetCode: "USDT",
				Amount:    "1",
			})
			if err != nil {
				t.Fatalf("unexpected decode error: %v", err)
			}
			if params.Type != tt.expected {
				t.Fatalf("unexpected mapped type: got %d, want %d", params.Type, tt.expected)
			}
		})
	}
}

func TestDecodeMakeTransactionRequest_UnsupportedType(t *testing.T) {
	_, err := decodeMakeTransactionRequest(&accountv1.MakeTransactionRequest{
		Type:      accountv1.JournalBizType(99),
		UserId:    1,
		AssetCode: "USDT",
		Amount:    "1",
	})
	if err == nil {
		t.Fatal("expected decode error for unsupported type")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
