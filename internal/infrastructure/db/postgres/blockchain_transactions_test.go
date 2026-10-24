package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"account-service/internal/domain"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBlockchainTxRepoStuckInboundForReconcileQueryFiltersOnlyActionableDeposits(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(
		postgres.Open("host=localhost user=test password=test dbname=test sslmode=disable"),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}

	repo := NewBlockchainTxRepository(db)
	query := repo.stuckInboundForReconcileQuery(context.Background(), time.Now().UTC(), 50)
	stmt := query.Find(&[]blockchainTxSQL{}).Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		"NOT EXISTS",
		"EXISTS",
		"dr.blockchain_tx_id = blockchain_txs.id",
		"dr.status IN",
		"ORDER BY id ASC",
		"LIMIT",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected SQL to contain %q, got:\n%s", want, sql)
		}
	}

	if strings.Contains(sql, "dr.status =") {
		t.Fatalf("query must not filter only one status, got:\n%s", sql)
	}

	if strings.Contains(sql, "dr.status IN $") {
		t.Fatalf("postgres must not render an unparenthesized IN placeholder, got:\n%s", sql)
	}

	foundNewStatus := false
	foundPendingStatus := false
	for _, v := range stmt.Vars {
		if v == domain.DepositStatusNew.Uint8() {
			foundNewStatus = true
		}
		if v == domain.DepositStatusPending.Uint8() {
			foundPendingStatus = true
		}
	}
	if !foundNewStatus || !foundPendingStatus {
		t.Fatalf("expected actionable statuses new and pending in vars, got %#v", stmt.Vars)
	}
}
