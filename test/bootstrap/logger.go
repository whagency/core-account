package bootstrap

import (
	"testing"

	"github.com/whagency/core-common/testkit/loggertest"
)

// InitLogger initialises the shared logger for account-service tests.
// Safe to call multiple times — loggertest.Init uses sync.Once internally.
func InitLogger(t testing.TB) {
	loggertest.Init(t, "account-service-tests")
}
