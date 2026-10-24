package app

import (
	"net/http"

	"account-service/config"
	accountHandlers "account-service/internal/transport/http/handler/accounts/protected"
	blockchainHandlers "account-service/internal/transport/http/handler/blockchain"
	blockchainWebhooks "account-service/internal/transport/http/handler/blockchain/webhooks"
	"account-service/internal/transport/http/handler/history/protected"
	"account-service/tools"

	"github.com/gin-gonic/gin"
	httpCommonHandlers "github.com/whagency/core-common/http/handler"
	httpMiddleware "github.com/whagency/core-common/http/middleware"
	"github.com/whagency/core-common/logger"
	userMiddlewares "github.com/whagency/core-common/services/user/http/middleware"
)

// setupHTTPServer sets up the server
//
//	@title Account Service
//	@version 1.0.0
//	@description ### 📘 Account Service API
//	@description Provides wallet and ledger operations. It owns user and system accounts, balance mutations, transaction journaling, deposit and withdrawal request flows, transfer flows, blockchain transaction tracking, balance history snapshots, and related event publishing.
//	@description ### 🔑 Authentication
//	@description Some endpoints require an **Authorization** header:
//	@description
//	@description ```http
//	@description Authorization: Bearer <token>
//	@description ```
//	@host https://api.app-stage.pro
//	@BasePath /account/
//
//	@securityDefinitions.apikey Authorization
//	@in header
//	@name Authorization
//	@description Provide your auth token
func (app *Application) setupHTTPServer() error {
	logger.Log().Info("[Application:setupHTTPServer][msg: setting up server]")

	if config.Commons().IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// Handlers
	getAccountBalancesHandler := accountHandlers.NewGetBalancesHandler(app.Core().AccountCtrl)
	getAccountPnlHandler := accountHandlers.NewGetPnlHandler(app.Core().AccountCtrl)
	initDepositHandler := accountHandlers.NewInitDepositHandler(app.Core().DepositCtrl)
	initWithdrawalHandler := accountHandlers.NewInitWithdrawalHandler(app.Core().WithdrawalCtrl)
	withdrawalOperationsHandler := accountHandlers.NewWithdrawalOperationsHandlerHandler(app.Core().WithdrawalCtrl)
	depositOperationsHandler := accountHandlers.NewDepositOperationsHandlerHandler(app.Core().DepositCtrl)
	topUpUserAccountHandler := accountHandlers.NewTopUpUserAccountHandler(app.Core().DevCtrl)
	transactionListHandler := protected.NewTransactionListHandler(app.Core().HistoryCtrl)
	depositListHandler := protected.NewDepositListHandler(app.Core().HistoryCtrl)
	withdrawalListHandler := protected.NewWithdrawalListHandler(app.Core().HistoryCtrl)
	transferListHandler := protected.NewTransferListHandler(app.Core().HistoryCtrl)
	depositDetailsHandler := protected.NewDepositDetailsHandler(app.Core().HistoryCtrl)
	withdrawalDetailsHandler := protected.NewWithdrawalDetailsHandler(app.Core().HistoryCtrl)
	transferHandler := accountHandlers.NewTransferHandler(app.Core().TransferCtrl)

	// Blockchain handlers
	processBlockchainTx := blockchainWebhooks.NewProcessTxHandler(app.Core().BlockchainCtrl)
	getFeeHandler := blockchainHandlers.NewGetFeeHandler(app.Core().BlockchainCtrl)
	chainConfigListHandler := blockchainHandlers.NewChainConfigListHandler(app.Core().BlockchainCtrl)

	router := gin.New()
	router.Use(
		gin.Recovery(),
		httpMiddleware.GinObservabilityMiddleware(),
		httpMiddleware.CORS(),
	)
	router.NoRoute(httpCommonHandlers.RouteNotFound)

	// Adding Metrics
	tools.LinkToPrometheusToGin(router)

	// API v1
	v1 := router.Group("api/v1")

	// Account endpoints
	accountProtected := v1.Group("/accounts")
	accountProtected.Use(userMiddlewares.ValidateUserSession(app.Core().servicesGRPC.User))
	accountProtected.GET("/get-balances", getAccountBalancesHandler.Handle)
	accountProtected.GET("/get-pnl", getAccountPnlHandler.Handle)
	accountProtected.POST("/deposits/init", initDepositHandler.Handle)
	accountProtected.POST("/withdrawals/init", initWithdrawalHandler.Handle)
	accountProtected.POST("/transfers/init", transferHandler.Handle)

	// History endpoints
	historyProtected := v1.Group("/history")
	historyProtected.Use(userMiddlewares.ValidateUserSession(app.Core().servicesGRPC.User))
	historyProtected.GET("/transactions", transactionListHandler.Handle)
	historyProtected.GET("/deposits", depositListHandler.Handle)
	historyProtected.GET("/withdrawals", withdrawalListHandler.Handle)
	historyProtected.GET("/transfers", transferListHandler.Handle)
	historyProtected.GET("/deposits/:id", depositDetailsHandler.Handle)
	historyProtected.GET("/withdrawals/:id", withdrawalDetailsHandler.Handle)

	// Blockchain endpoints
	blockchainProtected := v1.Group("/blockchain")
	blockchainProtected.Use(userMiddlewares.ValidateUserSession(app.Core().servicesGRPC.User))
	blockchainProtected.GET("/fees/:network/:asset", getFeeHandler.Handle)
	blockchainProtected.GET("/configs", chainConfigListHandler.Handle)

	// Internal endpoints
	internal := v1.Group("/internal")
	internal.Use(httpMiddleware.ValidateSecurityToken(
		config.Commons().SecurityToken,
		config.Commons().SecurityTokenHeader,
	))
	internal.POST("/blockchain/webhooks/transaction/process", processBlockchainTx.Handle)
	internal.POST("/accounts/withdrawals/operations", withdrawalOperationsHandler.Handle)
	internal.POST("/accounts/deposits/operations", depositOperationsHandler.Handle)
	if config.Commons().AllowsDevInternalEndpoints() {
		internal.POST("/dev/accounts/topup", topUpUserAccountHandler.Handle)
	}

	v1.GET("health", httpCommonHandlers.HealthCheck)

	app.httpServer = &http.Server{
		Addr:              ":" + config.HTTP().Port,
		Handler:           router,
		ReadTimeout:       config.HTTP().ReadTimeout,
		ReadHeaderTimeout: config.HTTP().ReadHeaderTimeout,
		WriteTimeout:      config.HTTP().WriteTimeout,
		MaxHeaderBytes:    http.DefaultMaxHeaderBytes,
	}

	return nil
}
