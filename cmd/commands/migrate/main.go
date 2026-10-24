package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"account-service/config"

	dbcommon "github.com/whagency/core-common/db"
	"github.com/whagency/core-common/db/migrator"
	outboxmigrations "github.com/whagency/core-common/outbox/postgres/migrations"
)

// _defaultTimeout is the default duration to wait before timing out a migration command.
const _defaultTimeout = 30 * time.Second

// _outboxMigrationsTable is the history table for the outbox migrations that come
// from the shared common module rather than this repository.
//
// It is named for the source rather than for "common" because common owns more
// than one migration source; each gets its own history table so a service can
// adopt one without inheriting the others.
//
// The two sources are versioned independently — account-service allocates its own
// numbers, common allocates from a reserved band — so they cannot share one
// history table without fighting over the version counter.
const _outboxMigrationsTable = "schema_migrations_outbox"

func main() {
	if err := runCommand(); err != nil {
		log.Printf("[commands:migrate]%s", err)
		os.Exit(1)
	}
}

func runCommand() error {
	var (
		command = flag.String("command", migrator.CommandUp.String(), fmt.Sprintf("migration command (%s|%s|%s|%s|%s|%s)", migrator.CommandUp, migrator.CommandDown, migrator.CommandSteps, migrator.CommandVersion, migrator.CommandForce, migrator.CommandDrop))
		steps   = flag.Int("steps", 1, "number of steps to migrate (used with steps command)")
		timeout = flag.Duration("timeout", _defaultTimeout, "command timeout")
		version = flag.Int("version", 0, "set version (used with force command)")
		source  = flag.String("source", "", "migration source: service|common|all (default: all for up/version, service otherwise)")
	)
	flag.Parse()

	if err := config.InitForDBMigration(); err != nil {
		return err
	}

	cfgDB := config.DB()
	commons := config.Commons()

	var cfg migrator.Config
	switch config.DB().DriverName {
	case dbcommon.DriverNamePostgres:
		cfg = migrator.Config{
			Driver: dbcommon.DriverPostgres,
			DSN: fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
				cfgDB.Host,
				cfgDB.Port,
				cfgDB.User,
				cfgDB.Password,
				cfgDB.DBName,
				cfgDB.SSLMode,
				commons.TimeZone,
			),
		}
	default:
		return fmt.Errorf("unsupported DB_DRIVER value: %s", cfgDB.DriverName)
	}

	manager, err := migrator.New(cfg)
	if err != nil {
		return err
	}

	// The outbox_messages DDL is owned by common and applied from its embedded FS
	// rather than copied into data/db/migrations. Copying it would give every
	// adopting service its own drifting fork of a shared table.
	commonCfg := cfg
	commonCfg.MigrationsDir = ""
	commonCfg.MigrationsFS = outboxmigrations.FS
	commonCfg.MigrationsTable = _outboxMigrationsTable

	commonManager, err := migrator.New(commonCfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cmd, err := migrator.ParseCommand(*command)
	if err != nil {
		return err
	}

	execCfg := migrator.ExecuteConfig{Command: cmd, Steps: *steps, Version: *version}

	managers, err := selectManagers(cmd, *source, manager, commonManager)
	if err != nil {
		return err
	}

	for _, m := range managers {
		status, errExecute := m.Execute(ctx, execCfg)
		if errExecute != nil {
			return errExecute
		}
		if cmd == migrator.CommandVersion {
			log.Printf("[commands:migrate][command: %s][version: %d][dirty: %t]", *command, status.Version, status.Dirty)
		}
	}

	if cmd != migrator.CommandVersion {
		fmt.Printf("[commands:migrate][command: %s][msg: completed successfully]", *command)
	}

	return nil
}

// selectManagers picks which migration sources a command runs against.
//
// Only "up" and "version" fan out to both by default. The rest — down, steps,
// force, drop — carry a step count or a version number that is meaningful for
// exactly one history table, so applying them to both sources would silently do
// the wrong thing to one of them. Those default to the service's own schema and
// require an explicit -source=common to touch the shared one.
func selectManagers(
	cmd migrator.Command,
	source string,
	service, common *migrator.Manager,
) ([]*migrator.Manager, error) {
	if source == "" {
		if cmd == migrator.CommandUp || cmd == migrator.CommandVersion {
			source = "all"
		} else {
			source = "service"
		}
	}

	switch source {
	case "service":
		return []*migrator.Manager{service}, nil
	case "common":
		return []*migrator.Manager{common}, nil
	case "all":
		// The shared schema goes up last and comes down first, so a service-owned
		// migration can never reference a shared table that is already gone.
		if cmd == migrator.CommandDown || cmd == migrator.CommandDrop {
			return []*migrator.Manager{common, service}, nil
		}
		return []*migrator.Manager{service, common}, nil
	default:
		return nil, fmt.Errorf("unsupported -source value: %s (expected service|common|all)", source)
	}
}
