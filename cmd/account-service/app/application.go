package app

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"account-service/config"
	"account-service/internal/domain"
	"account-service/tools"

	"github.com/dgraph-io/ristretto"
	"github.com/hazelcast/hazelcast-go-client"
	"github.com/imroc/req/v3"
	"github.com/wagslane/go-rabbitmq"
	"github.com/whagency/core-common/cache"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	"google.golang.org/grpc"
	"gorm.io/gorm"
)

type Application struct {
	httpServer       *http.Server
	httpClient       *req.Client
	grpcServer       *grpc.Server
	grpcListener     net.Listener
	db               *gorm.DB
	remoteCache      *hazelcast.Client
	globalLocalCache *ristretto.Cache
	queueConnections map[queue.ConnectionType]*rabbitmq.Conn

	remoteCacheManager   cache.RemoteCacheManager
	dbTransactionManager domain.TransactionManager
	queueManager         domain.QueueManager
	cronManager          domain.CronManager

	// Core
	core *Core
}

// NewApplication creates a new instance of Application.
func NewApplication() *Application {
	return &Application{
		queueConnections: make(map[queue.ConnectionType]*rabbitmq.Conn),
	}
}

// SetupDependencies initializes the dependencies for the application.
func (app *Application) SetupDependencies() error {
	if err := app.setupDB(); err != nil {
		return fmt.Errorf("[Application:SetupDependencies]%w", err)
	}

	if err := app.setupDBTransactionManager(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.setupQueueConnections(); err != nil {
		return fmt.Errorf("[Application:SetupDependencies]%w", err)
	}

	if err := app.setupQueueRetryTopologies(); err != nil {
		return fmt.Errorf("[Application:SetupDependencies]%w", err)
	}

	if err := app.setupGlobalLocalCache(); err != nil {
		return fmt.Errorf("[Application:SetupDependencies]%w", err)
	}

	if err := app.setupRemoteCache(); err != nil {
		return fmt.Errorf("[Application:SetupDependencies]%w", err)
	}

	if err := app.setupRemoteCacheManager(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.setupHTTPClient(); err != nil {
		return fmt.Errorf("[Application:SetupDependencies]%w", err)
	}

	return nil
}

// SetupStartupData initializes any necessary startup data for the application.
func (app *Application) SetupStartupData() error {
	tools.RegisterMetrics()

	if err := app.setupQueueManager(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.setupQueuePublishers(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.buildCore(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.createDefaultSystemAccounts(context.Background()); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.createDefaultChainConfigs(context.Background()); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.setupQueueConsumers(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	if err := app.setupHTTPServer(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	// The gRPC server is skipped in RoleWorker: binding a port that is never
	// served would hide a port-conflict misconfiguration until the day the role
	// changes.
	if config.AppRole().ServesRequests() {
		if err := app.setupGRPCServer(); err != nil {
			return fmt.Errorf("[Application:SetupStartupData]%w", err)
		}
	}

	if err := app.setupCronManager(); err != nil {
		return fmt.Errorf("[Application:SetupStartupData]%w", err)
	}

	return nil
}

// createDefaultSystemAccounts creates the default system accounts if they do not exist.
func (app *Application) createDefaultSystemAccounts(ctx context.Context) error {
	if err := app.core.AccountCtrl.CreateDefaultSystemAccounts(ctx); err != nil {
		return fmt.Errorf("[createDefaultSystemAccounts]%w", err)
	}

	return nil
}

// createDefaultChainConfigs creates the default chain configurations if they do not exist.
func (app *Application) createDefaultChainConfigs(ctx context.Context) error {
	if err := app.core.BlockchainCtrl.CreateDefaultChainConfigs(ctx); err != nil {
		return fmt.Errorf("[createDefaultChainConfigs]%w", err)
	}

	if _, err := app.core.cacheRepositories.ChainConfigs.GetWithRefresh(context.Background(), ""); err != nil {
		logger.Log().Fatalf("[Application:createDefaultChainConfigs]%s", err)
	}

	return nil
}

// CronManager returns the cron manager for the application.
func (app *Application) CronManager() domain.CronManager {
	return app.cronManager
}

// RemoteCacheManager returns the remote cache manager for the application.
func (app *Application) RemoteCacheManager() cache.RemoteCacheManager {
	return app.remoteCacheManager
}

// HTTPServer returns the HTTP server for the application.
func (app *Application) HTTPServer() *http.Server {
	return app.httpServer
}

// GRPCServer returns the gRPC server for the application.
func (app *Application) GRPCServer() *grpc.Server {
	return app.grpcServer
}

// GRPCListener returns the gRPC listener for the application.
func (app *Application) GRPCListener() net.Listener {
	return app.grpcListener
}

// DB returns the database connection for the application.
func (app *Application) DB() *gorm.DB {
	return app.db
}

// DBTransactionManager returns the db transaction manager for the application.
func (app *Application) DBTransactionManager() domain.TransactionManager {
	return app.dbTransactionManager
}

// RemoteCache returns the remote cache client for the application.
func (app *Application) RemoteCache() *hazelcast.Client {
	return app.remoteCache
}

// GlobalLocalCache returns the global local cache for the application.
func (app *Application) GlobalLocalCache() *ristretto.Cache {
	return app.globalLocalCache
}

// Core returns the core components of the application.
func (app *Application) Core() *Core {
	return app.core
}

// QueueManager returns the queue manager for the application.
func (app *Application) QueueManager() domain.QueueManager {
	return app.queueManager
}
