package config

import (
	"testing"
)

// setRequiredCommonEnv sets every variable loadCommonConfig marks as required,
// so a test can vary the single one it is about.
func setRequiredCommonEnv(t *testing.T) {
	t.Helper()

	vars := map[string]string{
		"APP_NAME":                     "account-service",
		"APP_ENVIRONMENT":              "local",
		"ACCOUNT_INTEGRITY_SECRET":     "integrity-secret",
		"CURRENCY_SERVICE_URL":         "http://localhost:8082",
		"USER_SERVICE_URL":             "http://localhost:8000",
		"NODE_SERVICE_URL":             "http://localhost:3000",
		"RESTRICTION_SERVICE_URL":      "http://localhost:8083",
		"USER_SERVICE_GRPC_URL":        "localhost:50051",
		"RESTRICTION_SERVICE_GRPC_URL": "localhost:50054",
	}
	for key, value := range vars {
		t.Setenv(key, value)
	}
}

func TestLoadCommonConfig_SecurityToken(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    string
		wantErr bool
	}{
		{name: "token is kept", env: "s3cret", want: "s3cret"},
		{name: "token is trimmed", env: "  s3cret  ", want: "s3cret"},
		// envconfig's required tag only rejects an *unset* variable. SECURITY_TOKEN=
		// counts as set, and an empty expected token used to make the internal-endpoint
		// middleware accept every request that sent no header at all.
		{name: "empty token is rejected", env: "", wantErr: true},
		{name: "whitespace-only token is rejected", env: "   ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredCommonEnv(t)
			t.Setenv("SECURITY_TOKEN", tt.env)
			commonsSettings = CommonsSetting{}

			err := loadCommonConfig()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SECURITY_TOKEN=%q was accepted, want an error", tt.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadCommonConfig: %v", err)
			}
			if got := Commons().SecurityToken; got != tt.want {
				t.Fatalf("SecurityToken = %q, want %q", got, tt.want)
			}
		})
	}
}
