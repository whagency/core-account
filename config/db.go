package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
	dbcommon "github.com/whagency/core-common/db"
)

var dbSettings DBSettings

type DBSettings struct {
	Host            string        `envconfig:"DB_HOST" required:"true"`
	Port            string        `envconfig:"DB_PORT" required:"true"`
	User            string        `envconfig:"DB_USER" required:"true"`
	Password        string        `envconfig:"DB_PASSWORD" required:"true"`
	DBName          string        `envconfig:"DB_NAME" required:"true"`
	MaxIdleConns    int           `envconfig:"DB_MAX_IDLE_CONN" default:"100"`
	MaxOpenConns    int           `envconfig:"DB_MAX_OPEN_CONN" default:"100"`
	ConnMaxLifetime time.Duration `envconfig:"DB_MAX_LIFETIME_CONN" default:"5m"`
	ConnMaxIdleTime time.Duration `envconfig:"DB_MAX_IDLE_TIME_CONN" default:"45s"`
	Driver          string        `envconfig:"DB_DRIVER" default:"postgres"`  // postgres for PostgreSQL, mysql for MySQL
	SSLMode         string        `envconfig:"DB_SSL_MODE" default:"disable"` // libpq sslmode: disable|allow|prefer|require|verify-ca|verify-full

	DriverName string
}

func DB() DBSettings {
	return dbSettings
}

// loadDBConfig loads the DB settings from the environment variables
func loadDBConfig() error {
	if err := envconfig.Process("", &dbSettings); err != nil {
		return fmt.Errorf("[loadDBConfig][err: %w]", err)
	}

	// Validate DriverName
	driverName, err := dbcommon.SqlDriverName(dbSettings.Driver)
	if err != nil {
		return fmt.Errorf("[loadDBConfig][err: %w]", err)
	}
	dbSettings.DriverName = driverName

	// Validate SSLMode
	sslMode, err := dbcommon.NormalizePostgresSSLMode(dbSettings.SSLMode)
	if err != nil {
		return fmt.Errorf("[loadDBConfig][err: %w]", err)
	}
	dbSettings.SSLMode = sslMode

	return nil
}
