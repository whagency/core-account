package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/whagency/core-common/outbox"
)

var cronSettings CronSettings

type CronSettings struct {
	AccountPNLUpdateSpec string `envconfig:"CRON_ACCOUNT_PNL_UPDATE_SPEC" default:"@daily"` // @daily // @every 5s

	// Withdrawal reconciliation cron — recovers withdrawals stuck because
	// their queue message died. See WithdrawalCtrl.ReconcileStuck.
	WithdrawalReconcileSpec             string        `envconfig:"CRON_WITHDRAWAL_RECONCILE_SPEC" default:"@every 5m"`
	WithdrawalReconcilePendingThreshold time.Duration `envconfig:"CRON_WITHDRAWAL_RECONCILE_PENDING_THRESHOLD" default:"30m"`
	WithdrawalReconcileNewThreshold     time.Duration `envconfig:"CRON_WITHDRAWAL_RECONCILE_NEW_THRESHOLD" default:"3h"`
	WithdrawalReconcileBatchSize        int           `envconfig:"CRON_WITHDRAWAL_RECONCILE_BATCH_SIZE" default:"50"`

	// Deposit reconciliation cron — recovers inbound BlockchainTx rows whose
	// blockchain.transaction.event message died between the callback webhook
	// and the consumer. See DepositCtrl.ReconcileStuck.
	DepositReconcileSpec      string        `envconfig:"CRON_DEPOSIT_RECONCILE_SPEC" default:"@every 5m"`
	DepositReconcileThreshold time.Duration `envconfig:"CRON_DEPOSIT_RECONCILE_THRESHOLD" default:"15m"`
	DepositReconcileBatchSize int           `envconfig:"CRON_DEPOSIT_RECONCILE_BATCH_SIZE" default:"50"`

	// Outbox runtime and retention. The values themselves live in common because
	// every adopting service shares one dashboard and one alert set — see
	// outbox.Settings. Override through the environment when this service really
	// needs a different number, and say why.
	Outbox outbox.Settings
}

// Cron returns the cron settings
func Cron() CronSettings {
	return cronSettings
}

// loadCronConfig loads the cron configuration from the environment variables
func loadCronConfig() error {
	if err := envconfig.Process("", &cronSettings); err != nil {
		return fmt.Errorf("[loadCronConfig][err: %w]", err)
	}
	return nil
}
