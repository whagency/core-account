package app

import (
	"context"
	"fmt"

	"account-service/config"
	"account-service/internal/domain"
	"account-service/tools"

	"github.com/whagency/core-common/cron"
	outboxcron "github.com/whagency/core-common/outbox/cronjobs"
)

// setupCronManager sets the cron jobs for the application and runs them
func (app *Application) setupCronManager() error {
	app.cronManager = cron.NewManager()

	// Add cron jobs
	err := app.cronManager.AddFunc(config.Cron().AccountPNLUpdateSpec, "account.pnl.update", func(ctx context.Context) error {
		return app.Core().AccountCtrl.UpdatePnlStat(ctx)
	})
	if err != nil {
		return fmt.Errorf("[setupCronManager]%w", err)
	}

	err = app.cronManager.AddFunc(
		config.Cron().WithdrawalReconcileSpec,
		"withdrawal.reconcile",
		func(ctx context.Context) error {
			return app.Core().WithdrawalCtrl.ReconcileStuck(ctx, domain.ReconcileStuckParams{
				PendingThreshold: config.Cron().WithdrawalReconcilePendingThreshold,
				NewThreshold:     config.Cron().WithdrawalReconcileNewThreshold,
				BatchSize:        config.Cron().WithdrawalReconcileBatchSize,
			})
		},
	)
	if err != nil {
		return fmt.Errorf("[setupCronManager]%w", err)
	}

	err = app.cronManager.AddFunc(
		config.Cron().DepositReconcileSpec,
		"deposit.reconcile",
		func(ctx context.Context) error {
			return app.Core().DepositCtrl.ReconcileStuck(ctx, domain.ReconcileStuckDepositParams{
				Threshold: config.Cron().DepositReconcileThreshold,
				BatchSize: config.Cron().DepositReconcileBatchSize,
			})
		},
	)
	if err != nil {
		return fmt.Errorf("[setupCronManager]%w", err)
	}

	// Prune and lag come from common: they are pure infrastructure, identical in
	// every adopting service, and easy to forget.
	if errOutbox := outboxcron.Register(
		app.cronManager,
		app.Core().Outbox,
		config.Cron().Outbox,
		tools.SetOutboxLag,
	); errOutbox != nil {
		return fmt.Errorf("[setupCronManager]%w", errOutbox)
	}
	return nil
}
