package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	application "account-service/cmd/account-service/app"
	"account-service/config"

	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/tracer"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

const _shutdownTimeout = 10 * time.Second

func main() {
	if err := runMain(); err != nil {
		log.Printf("[main]%s", err)
		os.Exit(1)
	}
}

func runMain() error {
	app := application.NewApplication()

	if err := config.Init(); err != nil {
		return err
	}

	if err := logger.Init(config.LoggerConfig()); err != nil {
		return err
	}
	defer logger.Sync()

	if err := tracer.Init(config.TracerConfig()); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, app); err != nil {
		return err
	}

	return nil
}

// run starts the application components and handles graceful shutdown.
// logRole states plainly what this process will and will not do.
//
// A split deployment has to run both roles: APP_ROLE=api alone stops blockchain
// deposits being consumed and stops reconciliation cron, and APP_ROLE=worker
// alone answers no business traffic. Neither failure announces itself in any
// other way, so the banner names the gap rather than only the role.
func logRole(role config.Role) {
	switch role {
	case config.RoleAPI:
		logger.Log().Warnf("[main][msg: APP_ROLE=%s — serving HTTP and gRPC only]"+
			"[not running: queue consumers, cron jobs, outbox pool]"+
			"[a %s deployment must run alongside this one]", role, config.RoleWorker)
	case config.RoleWorker:
		logger.Log().Warnf("[main][msg: APP_ROLE=%s — running queue consumers, cron jobs and outbox pool]"+
			"[not serving: gRPC; HTTP is up for probes and metrics only, do not route business traffic here]"+
			"[an %s deployment must run alongside this one]", role, config.RoleAPI)
	default:
		logger.Log().Infof("[main][msg: APP_ROLE=%s — running everything in one process]", role)
	}
}

func run(ctx context.Context, app *application.Application) error {
	logger.Log().Info("[main][msg: setting up dependencies]")
	if err := app.SetupDependencies(); err != nil {
		return err
	}

	logger.Log().Info("[main][msg: setting up startup data]")
	if err := app.SetupStartupData(); err != nil {
		return err
	}

	role := config.AppRole()
	logRole(role)

	g, gctx := errgroup.WithContext(ctx)

	if role.RunsBackground() {
		// Consumers
		g.Go(func() error {
			logger.Log().Info("[main][msg: starting consumers]")
			return app.QueueManager().RunConsumers()
		})

		// Cron
		g.Go(func() error {
			logger.Log().Info("[main][msg: starting cron jobs]")
			app.CronManager().Start()

			<-gctx.Done()

			logger.Log().Info("[main][msg: stopping cron jobs]")
			cronCtx := app.CronManager().Stop()
			<-cronCtx.Done()
			return nil
		})

		// Outbox pool — one relay goroutine claiming due messages plus Workers
		// goroutines delivering them. Lifecycle is bound to gctx so everything
		// exits cleanly on SIGTERM before the shutdown timeout.
		g.Go(func() error {
			logger.Log().Info("[main][msg: starting outbox pool]")
			return app.Core().OutboxPool.Run(gctx)
		})
	}

	// HTTP server runs in every role. In RoleWorker it exists only so liveness
	// probes and the metrics endpoint are reachable — the outbox metrics are
	// published by exactly the pods that do the delivering, so a worker without
	// a metrics endpoint would be invisible.
	g.Go(func() error {
		logger.Log().Infof("[main][msg: http server starting on port %s]", config.HTTP().Port)

		err := app.HTTPServer().ListenAndServe()
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})

	if role.ServesRequests() {
		// gRPC server
		g.Go(func() error {
			logger.Log().Infof("[main][msg: grpc server starting on port %s]", config.GRPC().Port)

			err := app.GRPCServer().Serve(app.GRPCListener())
			if err == nil || errors.Is(err, grpc.ErrServerStopped) {
				return nil
			}
			return err
		})
	}

	select {
	case <-ctx.Done():
		logger.Log().Info("[main][msg: shutdown requested]")
	case <-gctx.Done():
		logger.Log().Info("[main][msg: background task stopped]")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), _shutdownTimeout)
	defer cancel()
	shutdown(app, shutdownCtx)

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// shutdown performs a graceful shutdown of the application components.
func shutdown(app *application.Application, ctx context.Context) {
	// HTTP
	if err := app.HTTPServer().Shutdown(ctx); err != nil {
		logger.Log().Errorf("[main][err: http shutdown failed: %s]", err)
	} else {
		logger.Log().Info("[main][msg: http server shutdown gracefully]")
	}

	// RoleWorker never builds a gRPC server, so there is nothing to stop.
	if app.GRPCServer() != nil {
		done := make(chan struct{})
		go func() {
			app.GRPCServer().GracefulStop()
			close(done)
		}()

		select {
		case <-done:
			logger.Log().Info("[main][msg: grpc server shutdown gracefully]")
		case <-ctx.Done():
			logger.Log().Error("[main][msg: grpc graceful stop timeout, forcing stop]")
			app.GRPCServer().Stop()
		}
	}

	if listener := app.GRPCListener(); listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Log().Errorf("[main][err: grpc listener close failed: %s]", err)
		}
	}

	if err := tracer.Shutdown(ctx); err != nil {
		logger.Log().Errorf("[main][err: tracer shutdown failed: %s]", err)
	}

	if err := app.QueueManager().ClosePublishers(); err != nil {
		logger.Log().Errorf("[main][err: close publishers: %s]", err)
	}
	if err := app.QueueManager().CloseConsumers(); err != nil {
		logger.Log().Errorf("[main][err: close consumers: %s]", err)
	}
	app.CloseQueueConnections()

	// Close outgoing gRPC connections (clients)
	if core := app.Core(); core != nil {
		if err := core.CloseGRPC(); err != nil {
			logger.Log().Errorf("[main][err: core grpc close failed: %s]", err)
		}
	}
}
