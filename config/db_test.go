package config

import (
	"testing"
)

// setRequiredDBEnv sets every variable loadDBConfig marks as required, so a
// test can vary the single one it is about.
func setRequiredDBEnv(t *testing.T) {
	t.Helper()

	vars := map[string]string{
		"DB_HOST":     "localhost",
		"DB_PORT":     "5432",
		"DB_USER":     "appuser",
		"DB_PASSWORD": "apppassword",
		"DB_NAME":     "account",
	}
	for key, value := range vars {
		t.Setenv(key, value)
	}
}

func TestLoadDBConfig_SSLMode(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    string
		wantErr bool
	}{
		{name: "unset defaults to disable", env: "", want: "disable"},
		{name: "require", env: "require", want: "require"},
		{name: "verify-full", env: "verify-full", want: "verify-full"},
		{name: "case and space tolerant", env: "  Require ", want: "require"},
		// A typo must stop the process. libpq silently treats an unknown sslmode
		// as an error at connect time, and "requre" quietly meaning "no TLS"
		// is exactly the outcome this setting exists to prevent.
		{name: "typo is rejected", env: "requre", wantErr: true},
		{name: "enabled is rejected", env: "enabled", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredDBEnv(t)
			if tt.env != "" {
				t.Setenv("DB_SSL_MODE", tt.env)
			}
			dbSettings = DBSettings{}

			err := loadDBConfig()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DB_SSL_MODE=%q was accepted, want an error", tt.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadDBConfig: %v", err)
			}
			if got := DB().SSLMode; got != tt.want {
				t.Fatalf("SSLMode = %q, want %q", got, tt.want)
			}
		})
	}
}
