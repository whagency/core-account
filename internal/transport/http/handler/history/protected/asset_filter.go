package protected

import (
	"slices"
	"strings"

	svcErrors "account-service/internal/domain/errors"

	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/services/currency"
	commonTools "github.com/whagency/core-common/tools"
)

// _historyAllowedAssetCodes defines the supported asset filters for account history endpoints.
var _historyAllowedAssetCodes = []currency.AssetCode{
	currency.DefaultAccountAssetCode,
}

// decodeOptionalHistoryAssetCode validates and normalizes an optional history asset filter.
func decodeOptionalHistoryAssetCode(scope, field string, raw *string) (*string, *appErrors.ErrorResponse) {
	if raw == nil {
		return nil, nil
	}

	assetCode := currency.NewAssetCode(strings.ToUpper(strings.TrimSpace(*raw)))
	if slices.Contains(_historyAllowedAssetCodes, assetCode) {
		return commonTools.ToPtr(assetCode.String()), nil
	}

	return nil, svcErrors.NewOneOfValidationError(
		scope,
		field,
		*raw,
		commonTools.StringValues(_historyAllowedAssetCodes),
	)
}
