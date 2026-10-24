package config

import (
	"fmt"
	"time"

	kytProviders "account-service/config/kyt-providers"

	"github.com/kelseyhightower/envconfig"
	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/appenv"
	"github.com/whagency/core-common/cache"
)

var commonsSettings CommonsSetting

type CommonsSetting struct {
	AppName                    string `envconfig:"APP_NAME" required:"true"`
	Environment                string `envconfig:"APP_ENVIRONMENT" required:"true"`
	Version                    string `envconfig:"APP_VERSION" default:"1.0.0"`
	TimeZone                   string `envconfig:"TIME_ZONE" default:"UTC"`
	SecurityToken              string `envconfig:"SECURITY_TOKEN" required:"true"`
	SecurityTokenHeader        string `default:"X-API-KEY"`
	EnableDevInternalEndpoints bool   `envconfig:"ENABLE_DEV_INTERNAL_ENDPOINTS" default:"false"`
	AccountIntegritySecret     string `envconfig:"ACCOUNT_INTEGRITY_SECRET" required:"true"`

	TimeoutExtCall        time.Duration `envconfig:"TIMEOUT_EXTERNAL_URL" default:"5s"`
	CurrencyServiceURL    string        `envconfig:"CURRENCY_SERVICE_URL" required:"true"`
	UserServiceURL        string        `envconfig:"USER_SERVICE_URL" required:"true"`
	NodeServiceURL        string        `envconfig:"NODE_SERVICE_URL" required:"true"`
	RestrictionServiceURL string        `envconfig:"RESTRICTION_SERVICE_URL" required:"true"`

	UserServiceGrpcURL        string        `envconfig:"USER_SERVICE_GRPC_URL" required:"true"`
	RestrictionServiceGrpcURL string        `envconfig:"RESTRICTION_SERVICE_GRPC_URL" required:"true"`
	GrpcTimeout               time.Duration `envconfig:"GRPC_TIMEOUT" default:"15s"`

	DateFormat string
}

// IsProduction checks if the environment is production
func (CommonsSetting) IsProduction() bool {
	return appenv.IsProduction(commonsSettings.Environment)
}

// AllowsDevInternalEndpoints reports whether dev-like HTTP helpers may be
// exposed by this service runtime.
func (CommonsSetting) AllowsDevInternalEndpoints() bool {
	if !commonsSettings.EnableDevInternalEndpoints {
		return false
	}

	return appenv.IsDevLike(commonsSettings.Environment)
}

func Commons() CommonsSetting {
	return commonsSettings
}

// Init loads the configurations
func Init() error {
	if err := loadCommonConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadLoggerConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadTracerConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadRoleConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadHTTPConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadGRPCConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadQueueConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadDBConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadDBLoggerConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := cache.LoadCacheConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadCronConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadKYTProvidersConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	// decimal library configuration for parsing the float values
	decimal.MarshalJSONWithoutQuotes = true

	return nil
}

// InitForDBMigration initializes only the DB configurations for migration purpose
func InitForDBMigration() error {
	if err := loadCommonConfig(); err != nil {
		return fmt.Errorf("[config:Init]%w", err)
	}

	if err := loadDBConfig(); err != nil {
		return fmt.Errorf("[config:InitForDBMigration]%w", err)
	}

	return nil
}

// loadCommonConfig loads the common configurations
func loadCommonConfig() error {
	if err := envconfig.Process("", &commonsSettings); err != nil {
		return fmt.Errorf("[loadCommonConfig][err: %w]", err)
	}

	if err := appenv.ValidateAppName(commonsSettings.AppName); err != nil {
		return fmt.Errorf("[loadCommonConfig][err: %w]", err)
	}

	normalizedEnvironment, err := appenv.Normalize(commonsSettings.Environment)
	if err != nil {
		return fmt.Errorf("[loadCommonConfig][err: %w]", err)
	}
	commonsSettings.Environment = normalizedEnvironment

	securityToken, err := appenv.NormalizeSecurityToken(commonsSettings.SecurityToken)
	if err != nil {
		return fmt.Errorf("[loadCommonConfig][err: %w]", err)
	}
	commonsSettings.SecurityToken = securityToken

	commonsSettings.DateFormat = time.RFC3339

	return nil
}

// loadKYTProvidersConfig loads the KYT providers configuration from the environment variables
func loadKYTProvidersConfig() error {
	if err := kytProviders.LoadAMLBotConfig(); err != nil {
		return fmt.Errorf("[loadKYTProvidersConfig]%w", err)
	}

	return nil
}
