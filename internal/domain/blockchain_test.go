package domain

import (
	"regexp"
	"testing"

	"github.com/whagency/core-common/services/currency"
)

func TestDefaultChainConfigsIncludesTRXAddressRegex(t *testing.T) {
	t.Parallel()

	configs := DefaultChainConfigs()
	if len(configs) == 0 {
		t.Fatal("expected default chain configs")
	}

	var found *ChainConfig
	for i := range configs {
		if configs[i].AssetCode == currency.DefaultAccountAssetCode && configs[i].Network == ChainNetworkTRX {
			found = &configs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected default USDT/trx chain config")
	}
	if found.AddressRegex == nil || *found.AddressRegex == "" {
		t.Fatal("expected default USDT/trx address regex")
	}
	if _, err := regexp.Compile(*found.AddressRegex); err != nil {
		t.Fatalf("expected address regex to compile, got error: %v", err)
	}
}
