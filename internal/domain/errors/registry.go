package errors

import "github.com/whagency/core-common/errorsx"

// Errors is the global error registry for the account domain, initialized with common errors and domain-specific templates.
var registry = func() *errorsx.Registry {
	r := errorsx.NewRegistryWithCommon()
	r.MustRegisterAll(Templates())
	return r
}()

// New creates a new ErrorResponse based on the template for the given code and applies any provided options. It uses the global Errors registry.
func New(code int, opts ...errorsx.Option) *errorsx.ErrorResponse {
	return registry.New(code, opts...)
}
