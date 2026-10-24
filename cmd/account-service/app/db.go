package app

import (
	"database/sql"
	"fmt"
	"time"

	"account-service/config"

	_ "github.com/jackc/pgx/v5/stdlib"
	dbcommon "github.com/whagency/core-common/db"
	"github.com/whagency/core-common/db/gormlog"
	"github.com/whagency/core-common/logger"
	postgresGorm "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// setupDB initializes the database connection based on the configured driver.
func (app *Application) setupDB() error {
	switch config.DB().DriverName {
	case dbcommon.DriverNamePostgres:
		if err := app.setupPostgresDB(); err != nil {
			return fmt.Errorf("[Application:setupDB]%w", err)
		}
	default:
		return fmt.Errorf("[Application:setupDB][err: unsupported database driver %s]", config.DB().DriverName)
	}

	return nil
}

// setupPostgresDB sets up the PostgreSQL database connection.
func (app *Application) setupPostgresDB() error {
	logger.Log().Infof("[Application:setupPostgresDB][host:%s][port:%s][user:%s][dbname:%s]",
		config.DB().Host,
		config.DB().Port,
		config.DB().User,
		config.DB().DBName)

	loc, err := time.LoadLocation(config.Commons().TimeZone)
	if err != nil {
		logger.Log().Warnf("[Application:setupPostgresDB][loadLocation][err: error loading this timezone %s, application will set UTC timezone by default]", config.Commons().TimeZone)

		loc, err = time.LoadLocation("UTC")
		if err != nil {
			return fmt.Errorf("[setupPostgresDB][loadLocation][err: %w]", err)
		}
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=%s application_name=%s",
		config.DB().Host,
		config.DB().Port,
		config.DB().User,
		config.DB().Password,
		config.DB().DBName,
		config.DB().SSLMode,
		loc.String(),
		config.Commons().AppName,
	)

	sqlDB, err := sql.Open(config.DB().DriverName, dsn)
	if err != nil {
		return fmt.Errorf("[setupPostgresDB][sql.Open][err: %w]", err)
	}

	sqlDB.SetMaxIdleConns(config.DB().MaxIdleConns)
	sqlDB.SetMaxOpenConns(config.DB().MaxOpenConns)
	sqlDB.SetConnMaxIdleTime(config.DB().ConnMaxIdleTime)
	sqlDB.SetConnMaxLifetime(config.DB().ConnMaxLifetime)

	gormDB, err := gorm.Open(postgresGorm.New(postgresGorm.Config{
		Conn: sqlDB,
	}), &gorm.Config{
		TranslateError: true,
		Logger:         gormlog.NewDBLogger(config.DBLoggerConfig()),
	})
	if err != nil {
		return fmt.Errorf("[setupPostgresDB][gorm.Open][err: %w]", err)
	}

	app.db = gormDB

	logger.Log().Info("[Application:setupPostgresDB][msg: completed]")

	return nil
}

// setupDBTransactionManager initializes the db transaction manager for the application.
func (app *Application) setupDBTransactionManager() error {
	if app.db == nil {
		return fmt.Errorf("[setupDBTransactionManager][err: db is not initialized]")
	}

	app.dbTransactionManager = dbcommon.NewTransactionManager(app.db)

	logger.Log().Info("[Application:setupDBTransactionManager][msg: completed]")

	return nil
}
