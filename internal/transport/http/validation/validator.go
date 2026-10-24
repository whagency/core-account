package validation

import (
	"sync"

	"github.com/go-playground/validator/v10"
	commonValidation "github.com/whagency/core-common/http/validation"
)

var (
	once               sync.Once
	transportValidator *validator.Validate
)

// Validator exposes the shared HTTP transport validator instance.
// Safe for concurrent use after first initialization.
func Validator() *validator.Validate {
	once.Do(func() { transportValidator = newValidator() })
	return transportValidator
}

type Option = commonValidation.Option

// Init allows initializing the singleton with extra options (call once at app bootstrap).
// If called multiple times, only the first call has effect.
func Init(opts ...Option) *validator.Validate {
	once.Do(func() {
		transportValidator = newValidator(opts...)
	})
	return transportValidator
}

// ResetForTests resets the singleton for testing purposes.
func ResetForTests() {
	transportValidator = nil
	once = sync.Once{}
}

func newValidator(opts ...Option) *validator.Validate {
	return commonValidation.MustNew(opts...)
}
