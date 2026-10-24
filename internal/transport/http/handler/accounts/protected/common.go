package protected

import (
	"slices"
	"strings"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/services/currency"
	commonTools "github.com/whagency/core-common/tools"
)

// _accountsHTTPAllowedAssetCodes defines the allowed asset codes for accounts-related HTTP requests.
var _accountsHTTPAllowedAssetCodes = []currency.AssetCode{
	currency.DefaultAccountAssetCode,
}

// _accountsHTTPAllowedNetworks defines the allowed blockchain networks for accounts-related HTTP requests.
var _accountsHTTPAllowedNetworks = []domain.ChainNetwork{
	domain.ChainNetworkTRX,
}

// _accountsHTTPAllowedFeeTiers defines the allowed fee tiers for accounts-related HTTP requests.
var _accountsHTTPAllowedFeeTiers = []domain.FeeTier{
	domain.FeeTierSlow,
	domain.FeeTierStandard,
	domain.FeeTierFast,
}

// assetCodeToDomain converts the asset code from the request to the domain type and validates it against allowed values.
func assetCodeToDomain(scope, field, value string) (currency.AssetCode, *appErrors.ErrorResponse) {
	assetCode := currency.NewAssetCode(strings.ToUpper(strings.TrimSpace(value)))
	if slices.Contains(_accountsHTTPAllowedAssetCodes, assetCode) {
		return assetCode, nil
	}

	return "", svcErrors.NewOneOfValidationError(scope, field, value, commonTools.StringValues(_accountsHTTPAllowedAssetCodes))
}

// networkToDomain converts the network from the request to the domain type and validates it against allowed values.
func networkToDomain(scope, field, value string) (domain.ChainNetwork, *appErrors.ErrorResponse) {
	network, err := domain.NewChainNetworkFromString(value)
	if err == nil && slices.Contains(_accountsHTTPAllowedNetworks, network) {
		return network, nil
	}

	return 0, svcErrors.NewOneOfValidationError(scope, field, value, commonTools.StringValues(_accountsHTTPAllowedNetworks))
}

// feeTierToDomain converts the fee tier from the request to the domain type and validates it against allowed values.
func feeTierToDomain(scope, field, value string) (domain.FeeTier, *appErrors.ErrorResponse) {
	feeTier, err := domain.NewFeeTierFromString(value)
	if err == nil && slices.Contains(_accountsHTTPAllowedFeeTiers, feeTier) {
		return feeTier, nil
	}

	return 0, svcErrors.NewOneOfValidationError(scope, field, value, commonTools.StringValues(_accountsHTTPAllowedFeeTiers))
}
