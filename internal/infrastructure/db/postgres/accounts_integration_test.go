package postgres

import (
	"context"
	"os"
	"testing"

	"account-service/internal/domain"
	"account-service/test/bootstrap"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/testkit/postgrestest"
	"gorm.io/gorm"
)

// These tests need Docker: the behaviour under test is a driver semantic, and a
// fake repository can only restate whatever it was told to return.

var suite *postgrestest.Suite

func TestMain(m *testing.M) {
	var err error
	suite, err = postgrestest.Start(context.Background(), postgrestest.ContainerConfig{})
	if err != nil {
		panic("postgrestest: start: " + err.Error())
	}

	code := m.Run()
	_ = suite.Terminate(context.Background())
	os.Exit(code)
}

func openAccountRepo(t *testing.T) (*AccountRepo, *gorm.DB) {
	t.Helper()
	bootstrap.InitLogger(t)

	db := suite.OpenGormDB(t, postgrestest.OpenConfig{
		MigrationsDir: "../../../../data/db/migrations/postgres",
		// The first migration creates pg_stat_statements, which is database-level:
		// per-test schemas applying it in parallel race on pg_extension.
		SkipMigrationLine: postgrestest.SkipDatabaseLevelMigrationLine,
	})
	return NewAccountRepository(db), db
}

// TestAccountRepoGetBalance_MissingAccountReadsAsZero pins a contract that is
// easy to "clean up" into a production incident.
//
// Accounts are created on demand, so a user with no row yet is a legitimate
// state meaning "no funds", and both callers of this method — the withdrawal
// and transfer balance checks — map any error to DatabaseError. Reporting
// not-found here would therefore turn that state into a 500 on two money paths
// rather than a clean insufficient-funds answer.
//
// The zero comes from Scan, which unlike First reports no error for an empty
// result. Switching to First, or restoring the not-found branch that used to
// sit here unreachable, breaks this without any test but this one noticing.
func TestAccountRepoGetBalance_MissingAccountReadsAsZero(t *testing.T) {
	t.Parallel()

	repo, _ := openAccountRepo(t)
	missing := domain.NewIDFromUint64(424242)

	balance, err := repo.GetBalance(context.Background(), domain.GetBalanceParams{
		OwnerID:     &missing,
		AssetCode:   currency.DefaultAccountAssetCode,
		AccountType: domain.AccountTypeLiability,
	})
	if err != nil {
		t.Fatalf("missing account reported an error: %v — the callers turn that into a 500", err)
	}
	if !balance.IsZero() {
		t.Fatalf("missing account balance = %s, want 0", balance)
	}
}

// TestAccountRepoGetBalance_ReturnsStoredValue keeps the zero above from being
// vacuous: a row that exists must return its own balance, quantized as stored.
func TestAccountRepoGetBalance_ReturnsStoredValue(t *testing.T) {
	t.Parallel()

	repo, db := openAccountRepo(t)
	owner := domain.NewIDFromUint64(7)
	stored := decimal.NewFromInt(100).Shift(currency.DefaultAssetScale)

	if err := db.Exec(
		`INSERT INTO accounts (owner_id, asset_code, acct_type, balance, security_hash)
		 VALUES (?, ?, ?, ?, 'test-hash')`,
		owner.Uint64(), currency.DefaultAccountAssetCode.String(),
		domain.AccountTypeLiability.Uint8(), stored,
	).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	balance, err := repo.GetBalance(context.Background(), domain.GetBalanceParams{
		OwnerID:     &owner,
		AssetCode:   currency.DefaultAccountAssetCode,
		AccountType: domain.AccountTypeLiability,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !balance.Equal(stored) {
		t.Fatalf("balance = %s, want the stored %s", balance, stored)
	}
}

// TestAccountRepoGetBalance_SystemAccountMatchesOnNullOwner covers the other
// branch of the owner predicate: a nil OwnerID must match owner_id IS NULL and
// not collide with a user account for the same asset and type.
func TestAccountRepoGetBalance_SystemAccountMatchesOnNullOwner(t *testing.T) {
	t.Parallel()

	repo, db := openAccountRepo(t)
	systemBalance := decimal.NewFromInt(500).Shift(currency.DefaultAssetScale)

	if err := db.Exec(
		`INSERT INTO accounts (owner_id, asset_code, acct_type, balance, security_hash)
		 VALUES (NULL, ?, ?, ?, 'test-hash')`,
		currency.DefaultAccountAssetCode.String(), domain.AccountTypeLiability.Uint8(), systemBalance,
	).Error; err != nil {
		t.Fatalf("seed system account: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO accounts (owner_id, asset_code, acct_type, balance, security_hash)
		 VALUES (9, ?, ?, 1, 'test-hash')`,
		currency.DefaultAccountAssetCode.String(), domain.AccountTypeLiability.Uint8(),
	).Error; err != nil {
		t.Fatalf("seed user account: %v", err)
	}

	balance, err := repo.GetBalance(context.Background(), domain.GetBalanceParams{
		OwnerID:     nil,
		AssetCode:   currency.DefaultAccountAssetCode,
		AccountType: domain.AccountTypeLiability,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !balance.Equal(systemBalance) {
		t.Fatalf("system balance = %s, want %s", balance, systemBalance)
	}
}
