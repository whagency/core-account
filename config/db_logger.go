package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/whagency/core-common/db/gormlog"
)

var dbLoggerConfig gormlog.Config

// loadDBLoggerConfig loads the DB logger configuration from environment variables.
func loadDBLoggerConfig() error {
	if err := envconfig.Process("", &dbLoggerConfig); err != nil {
		return fmt.Errorf("[loadDBLoggerConfig][err: %w]", err)
	}

	if dbLoggerConfig.DBSystem == "" {
		dbLoggerConfig.DBSystem = DB().Driver
	}
	if dbLoggerConfig.DBName == "" {
		dbLoggerConfig.DBName = DB().DBName
	}

	return nil
}

// DBLoggerConfig returns the loaded DB logger configuration.
func DBLoggerConfig() gormlog.Config {
	return dbLoggerConfig
}
