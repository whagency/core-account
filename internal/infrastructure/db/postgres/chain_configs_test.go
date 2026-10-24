package postgres

import (
	"testing"

	"account-service/internal/domain"

	"github.com/whagency/core-common/services/currency"
)

func TestEncodeChainConfigForCreatePersistsAddressRegex(t *testing.T) {
	t.Parallel()

	config := &domain.ChainConfig{
		AssetCode:        currency.DefaultAccountAssetCode,
		Network:          domain.ChainNetworkTRX,
		Decimals:         6,
		MinConfirmations: 20,
		ConfirmFinality:  20,
		AssetName:        currency.DefaultAccountAssetCode.String(),
		AddressRegex:     ptr(domain.TRXAddressRegex),
		Status:           domain.ChainConfigStatusAll,
	}

	row := encodeChainConfigForCreate(config)
	if row.AddressRegex == nil {
		t.Fatal("expected address regex to be persisted")
	}
	if *row.AddressRegex != domain.TRXAddressRegex {
		t.Fatalf("expected address regex %q, got %q", domain.TRXAddressRegex, *row.AddressRegex)
	}
}

func ptr[T any](value T) *T {
	return &value
}
