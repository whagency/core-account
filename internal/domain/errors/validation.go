package errors

import (
	"fmt"

	appErrors "github.com/whagency/core-common/errorsx"
)

// NewOneOfValidationError builds a validation error for unsupported values from an allowed set.
func NewOneOfValidationError(scope, field, value string, supportedValues []string) *appErrors.ErrorResponse {
	return New(
		appErrors.ValidationError,
		appErrors.WithCause(fmt.Errorf("%s[err: invalid %s: %q]", scope, field, value)),
		appErrors.WithOneOfValidation(field, supportedValues...),
	)
}
