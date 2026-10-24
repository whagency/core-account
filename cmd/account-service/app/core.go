package app

import (
	"context"
	"fmt"
	"io"

	"account-service/config"
	"account-service/internal/controllers"
	"account-service/internal/domain"
	"account-service/internal/infrastructure/db/postgres"
	"account-service/internal/infrastructure/security"
	outboxtransport "account-service/internal/transport/outbox"
	amlBotService "account-service/services/kyt/amlbot"
	nodeService "account-service/services/node"
	"account-service/tools"

	"github.com/dgraph-io/ristretto"
	"github.com/imroc/req/v3"
	"github.com/whagency/core-common/cache"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/outbox"
	"github.com/whagency/core-common/services/currency"
	currencyService "github.com/whagency/core-common/services/currency/http"
	restrictionGrpc "github.com/whagency/core-common/services/restriction/grpc"
	userGrpc "github.com/whagency/core-common/services/user/grpc"
	userService "github.com/whagency/core-common/services/user/http"
	"gorm.io/gorm"
)

type Core struct {
	CacheCtrl domain.CacheController

	EventCtrl domain.EventController

	AccountCtrl     domain.AccountController
	HistoryCtrl     domain.HistoryController
	DepositCtrl     domain.DepositController
	WithdrawalCtrl  domain.WithdrawalController
	TransferCtrl    domain.TransferController
	BlockchainCtrl  domain.BlockchainController
	TransactionCtrl domain.TransactionController
	DevCtrl         domain.DevController

	// OutboxWorker claims outbox messages and dispatches them to their handlers.
	// Driven by OutboxPool; not used directly.
	OutboxWorker *outbox.Worker

	// OutboxPool owns the outbox runtime: one relay goroutine claiming due
	// messages plus Workers goroutines delivering them. Its Run method is
	// registered in the errgroup in main.go so shutdown is graceful.
	OutboxPool *outbox.Pool

	dbRepositories
	cacheRepositories
	securityComponents

	servicesHTTP
	servicesGRPC
}

type dbRepositories struct {
	Accounts           domain.AccountRepository
	Journals           domain.JournalEntryRepository
	Transactions       domain.TransactionRepository
	BalanceHistories   domain.BalanceHistoryRepository
	ChainConfigs       domain.ChainConfigRepository
	AddressAssignments domain.AddressAssignmentRepository
	BlockchainTxs      domain.BlockchainTxRepository
	WithdrawalRequests domain.WithdrawalRequestRepository
	DepositRequests    domain.DepositRequestRepository
	TransferRequests   domain.TransferRequestRepository
	Outbox             domain.OutboxRepository
}

type cacheRepositories struct {
	Assets                     *cache.Repository[currency.AssetMap]
	ChainConfigs               *cache.Repository[domain.ChainConfigs]
	WithdrawalLocker           domain.Locker
	WithdrawalProcessingLocker domain.Locker
	DepositLocker              domain.Locker
	DepositProcessingLocker    domain.Locker
	TransferLocker             domain.Locker
	BlockchainLocker           domain.Locker
}

type securityComponents struct {
	accountIntegrity domain.AccountIntegrityService
}

type servicesHTTP struct {
	Currency domain.CurrencyService
	User     domain.UserService
	Node     domain.NodeService
	KYT      domain.KYTService
}

type servicesGRPC struct {
	Restriction domain.RestrictionGRPCService
	User        domain.UserGRPCService
	closers     []io.Closer
}

// registerCloser registers an io.Closer to be closed when Core is closed.
func (s *servicesGRPC) registerCloser(c io.Closer) {
	s.closers = append(s.closers, c)
}

// Close closes all registered io.Closers in reverse order.
func (s *servicesGRPC) Close() error {
	var firstErr error
	for i := len(s.closers) - 1; i >= 0; i-- {
		if err := s.closers[i].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// CloseGRPC closes outgoing gRPC client connections owned by Core.
func (c *Core) CloseGRPC() error {
	return c.servicesGRPC.Close()
}

// buildCore initializes the core components of the application.
func (app *Application) buildCore() error {
	core := &Core{}

	// Initialize DB Repositories
	core.initDBRepositories(app.db)

	// Initialize Services
	core.initServices(app.httpClient)

	// Initialize Cache Repositories
	core.initCacheRepositories(app.globalLocalCache, app.remoteCacheManager)

	// Init Security Components
	if err := core.initSecurity(); err != nil {
		return fmt.Errorf("[buildCore]%w", err)
	}

	// Initialize Controllers
	if err := core.initControllers(app.DBTransactionManager(), app.QueueManager()); err != nil {
		return fmt.Errorf("[buildCore]%w", err)
	}

	app.core = core

	return nil
}

// initDBRepositories initializes the database repositories for the core components.
func (c *Core) initDBRepositories(db *gorm.DB) {
	c.dbRepositories.Accounts = postgres.NewAccountRepository(db)
	c.dbRepositories.Journals = postgres.NewJournalEntryRepository(db)
	c.dbRepositories.Transactions = postgres.NewTransactionRepository(db)
	c.dbRepositories.BalanceHistories = postgres.NewBalanceHistoryRepository(db)
	c.dbRepositories.ChainConfigs = postgres.NewAChainConfigRepository(db)
	c.dbRepositories.AddressAssignments = postgres.NewAddressAssignmentRepository(db)
	c.dbRepositories.BlockchainTxs = postgres.NewBlockchainTxRepository(db)
	c.dbRepositories.WithdrawalRequests = postgres.NewWithdrawalRequestRepository(db)
	c.dbRepositories.DepositRequests = postgres.NewDepositRequestRepository(db)
	c.dbRepositories.TransferRequests = postgres.NewTransferRequestsRepository(db)
	c.dbRepositories.Outbox = postgres.NewOutboxRepository(db)
}

// initCacheRepositories initializes the cache repositories for the core components.
func (c *Core) initCacheRepositories(globalLocalCache *ristretto.Cache, remoteCacheManager cache.RemoteCacheManager) {
	var err error

	c.cacheRepositories.ChainConfigs, err = cache.New[domain.ChainConfigs](
		cache.RepoOptions{
			Options:          domain.CacheKeys[domain.ChainConfigsCacheKeyID].Options,
			Name:             domain.CacheKeys[domain.ChainConfigsCacheKeyID].Name,
			CacheLocalGlobal: globalLocalCache,
			RemoteCache:      remoteCacheManager,
		},
	)
	if err != nil {
		logger.Log().Fatalf("[Application][Core:initCacheRepositories]%s", err)
	}

	c.cacheRepositories.ChainConfigs.SetRefreshFn(func(ctx context.Context, _ string) (domain.ChainConfigs, error) {
		return c.dbRepositories.ChainConfigs.GetAll(ctx)
	})

	if _, errRefresh := c.cacheRepositories.ChainConfigs.GetWithRefresh(context.Background(), ""); errRefresh != nil {
		logger.Log().Fatalf("[Application][Core:initCacheRepositories]%s", errRefresh)
	}

	c.cacheRepositories.Assets, err = cache.New[currency.AssetMap](
		cache.RepoOptions{
			Options:          domain.CacheKeys[domain.AssetsCacheKeyID].Options,
			Name:             domain.CacheKeys[domain.AssetsCacheKeyID].Name,
			CacheLocalGlobal: globalLocalCache,
			RemoteCache:      remoteCacheManager,
		},
	)
	if err != nil {
		logger.Log().Fatalf("[Application][Core:initCacheRepositories]%s", err)
	}

	c.cacheRepositories.Assets.SetRefreshFn(func(ctx context.Context, _ string) (currency.AssetMap, error) {
		return c.servicesHTTP.Currency.GetAssetMap(ctx)
	})

	if _, errRefresh := c.cacheRepositories.Assets.GetWithRefresh(context.Background(), ""); errRefresh != nil {
		logger.Log().Fatalf("[Application][Core:initCacheRepositories]%s", errRefresh)
	}

	c.cacheRepositories.WithdrawalLocker = cache.NewLocker(
		cache.LockerOptions{
			Name:        domain.WithdrawalLockerConfigKey,
			RemoteCache: remoteCacheManager,
		},
	)

	c.cacheRepositories.WithdrawalProcessingLocker = cache.NewLocker(
		cache.LockerOptions{
			Name:        domain.WithdrawalProcessingLockerConfigKey,
			RemoteCache: remoteCacheManager,
		},
	)

	c.cacheRepositories.DepositLocker = cache.NewLocker(
		cache.LockerOptions{
			Name:        domain.DepositLockerConfigKey,
			RemoteCache: remoteCacheManager,
		},
	)

	c.cacheRepositories.DepositProcessingLocker = cache.NewLocker(
		cache.LockerOptions{
			Name:        domain.DepositProcessingLockerConfigKey,
			RemoteCache: remoteCacheManager,
		},
	)

	c.cacheRepositories.TransferLocker = cache.NewLocker(
		cache.LockerOptions{
			Name:        domain.TransferLockerConfigKey,
			RemoteCache: remoteCacheManager,
		},
	)

	c.cacheRepositories.BlockchainLocker = cache.NewLocker(
		cache.LockerOptions{
			Name:        domain.BlockchainLockerConfigKey,
			RemoteCache: remoteCacheManager,
		},
	)
}

// initControllers initializes the controllers for the core components.x
func (c *Core) initControllers(dbTransactionManager domain.TransactionManager, queueManager domain.QueueManager) error {
	// Initialize Cache Controller
	c.CacheCtrl = controllers.NewCacheController(
		c.cacheRepositories.Assets,
		c.cacheRepositories.ChainConfigs,
	)

	// Initialize Event Controller
	pushPublisher, err := queueManager.GetPublisher(domain.PushQueuePublisherType)
	if err != nil {
		return fmt.Errorf("[initControllers]%w", err)
	}

	transactionRollbackPublisher, err := queueManager.GetPublisher(domain.TransactionRollbackQueuePublisherType)
	if err != nil {
		return fmt.Errorf("[initControllers]%w", err)
	}

	blockchainTransactionPublisher, err := queueManager.GetPublisher(domain.BlockchainTransactionLifecycleQueuePublisherType)
	if err != nil {
		return fmt.Errorf("[initControllers]%w", err)
	}

	withdrawalRequestPublisher, err := queueManager.GetPublisher(domain.WithdrawalRequestLifecycleQueuePublisherType)
	if err != nil {
		return fmt.Errorf("[initControllers]%w", err)
	}

	c.EventCtrl = controllers.NewEventController(
		blockchainTransactionPublisher,
		withdrawalRequestPublisher,
		pushPublisher,
		transactionRollbackPublisher,
	)

	// Initialize Main Controllers

	c.AccountCtrl = controllers.NewAccountController(
		c.CacheCtrl,

		c.dbRepositories.Accounts,
		c.dbRepositories.BalanceHistories,

		c.securityComponents.accountIntegrity,
		dbTransactionManager,
	)

	c.TransactionCtrl = controllers.NewTransactionController(
		c.AccountCtrl,
		c.CacheCtrl,

		c.dbRepositories.Accounts,
		c.dbRepositories.Journals,
		c.dbRepositories.Transactions,

		c.securityComponents.accountIntegrity,
		dbTransactionManager,
	)

	c.DevCtrl = controllers.NewDevController(
		c.TransactionCtrl,
	)

	c.HistoryCtrl = controllers.NewHistoryController(
		c.dbRepositories.Transactions,
		c.dbRepositories.DepositRequests,
		c.dbRepositories.WithdrawalRequests,
		c.dbRepositories.TransferRequests,
		c.dbRepositories.BlockchainTxs,
	)

	c.BlockchainCtrl = controllers.NewBlockchainController(
		c.EventCtrl,
		c.CacheCtrl,

		c.dbRepositories.AddressAssignments,
		c.dbRepositories.BlockchainTxs,
		c.dbRepositories.ChainConfigs,

		c.servicesHTTP.Node,
		c.servicesHTTP.KYT,
		c.servicesHTTP.Currency,

		c.cacheRepositories.BlockchainLocker,
		dbTransactionManager,
	)

	// Build the outbox handler registry and worker.
	outboxRegistry := outbox.NewRegistry()
	outboxRegistry.Register(outboxtransport.NewCounterUpdateHandler(c.servicesGRPC.Restriction))
	outboxRegistry.Register(outboxtransport.NewNotificationPushHandler(c.EventCtrl))

	outboxSettings := config.Cron().Outbox
	c.OutboxWorker = outbox.NewWorker(
		c.dbRepositories.Outbox,
		outboxRegistry,
		outboxSettings.WorkerConfig(tools.OutboxHooks()),
	)

	// Pool of N worker goroutines fed by one relay goroutine. Its Run loop is
	// registered in the errgroup inside main.go so its lifecycle is bound to
	// graceful shutdown.
	c.OutboxPool = outbox.NewPool(c.OutboxWorker, outboxSettings.PoolConfig())

	c.DepositCtrl = controllers.NewDepositController(
		c.BlockchainCtrl,
		c.EventCtrl,
		c.CacheCtrl,
		c.TransactionCtrl,

		c.dbRepositories.AddressAssignments,
		c.dbRepositories.DepositRequests,
		c.dbRepositories.Outbox,

		c.servicesGRPC.Restriction,
		c.servicesHTTP.User,

		c.cacheRepositories.DepositLocker,
		c.cacheRepositories.DepositProcessingLocker,

		dbTransactionManager,
	)

	c.WithdrawalCtrl = controllers.NewWithdrawalController(
		c.AccountCtrl,
		c.BlockchainCtrl,
		c.EventCtrl,
		c.CacheCtrl,
		c.TransactionCtrl,

		c.dbRepositories.WithdrawalRequests,
		c.dbRepositories.Outbox,

		c.servicesGRPC.Restriction,
		c.servicesHTTP.User,
		c.servicesHTTP.KYT,
		c.servicesHTTP.Currency,

		c.cacheRepositories.WithdrawalLocker,
		c.cacheRepositories.WithdrawalProcessingLocker,
		dbTransactionManager,
	)

	c.TransferCtrl = controllers.NewTransferController(
		c.AccountCtrl,
		c.TransactionCtrl,

		c.dbRepositories.TransferRequests,
		c.dbRepositories.Outbox,

		c.servicesGRPC.Restriction,
		c.servicesGRPC.User,

		c.cacheRepositories.TransferLocker,
		dbTransactionManager,
	)

	return nil
}

// initSecurity initializes the security components for the core components.
func (c *Core) initSecurity() error {
	guard, err := security.NewHMACAccountIntegrity(config.Commons().AccountIntegritySecret)
	if err != nil {
		return fmt.Errorf("[initSecurity]%w", err)
	}

	c.securityComponents.accountIntegrity = guard

	return nil
}

// initServices initializes the services for the core components.
func (c *Core) initServices(httpClient *req.Client) {
	// HTTP
	c.servicesHTTP.Currency = currencyService.NewCurrencyService(httpClient, config.Commons().CurrencyServiceURL)
	c.servicesHTTP.User = userService.NewUserService(httpClient, config.Commons().UserServiceURL, config.Commons().SecurityToken)
	c.servicesHTTP.Node = nodeService.NewNodeService(httpClient)
	c.servicesHTTP.KYT = amlBotService.NewAmlBotService(httpClient)

	// GRPC
	c.servicesGRPC.User = userGrpc.NewUserService(
		config.Commons().UserServiceGrpcURL,
		userGrpc.WithInsecureTransport(),
		userGrpc.WithTimeoutExtCall(config.Commons().GrpcTimeout),
	)
	c.servicesGRPC.registerCloser(c.servicesGRPC.User)
	c.servicesGRPC.Restriction = restrictionGrpc.NewRestrictionService(
		config.Commons().RestrictionServiceGrpcURL,
		restrictionGrpc.WithInsecureTransport(),
		restrictionGrpc.WithTimeoutExtCall(config.Commons().GrpcTimeout),
	)
	c.servicesGRPC.registerCloser(c.servicesGRPC.Restriction)
}
