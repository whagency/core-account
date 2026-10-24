package app

import (
	"fmt"
	"net"

	"account-service/config"
	grpcaccount "account-service/internal/transport/grpc/account"
	"account-service/internal/transport/grpc/pb/accountv1"

	grpcMiddleware "github.com/whagency/core-common/grpc/middleware"
	"github.com/whagency/core-common/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// setupGRPCServer initializes and configures the gRPC server.
func (app *Application) setupGRPCServer() error {
	logger.Log().Info("[Application:setupGRPCServer][msg: setting up server]")

	grpcConfig := config.GRPC()
	opts := []grpc.ServerOption{
		grpc.MaxConcurrentStreams(grpcConfig.MaxConcurrentStreams),
		grpc.ConnectionTimeout(grpcConfig.ConnectionTimeout),
		grpc.MaxRecvMsgSize(grpcConfig.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(grpcConfig.MaxSendMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{ // options related to heartbeat
			Timeout:           grpcConfig.KeepaliveTimeout,
			Time:              grpcConfig.KeepaliveTime,
			MaxConnectionIdle: grpcConfig.MaxConnectionIdle,
		}),
		grpc.ChainUnaryInterceptor(
			grpcMiddleware.ObservabilityInterceptor(),
			grpcMiddleware.TimeoutInterceptor(grpcConfig.RequestTimeout),
		),
	}

	server := grpc.NewServer(opts...)

	accountService := grpcaccount.NewService(
		app.core.TransactionCtrl,
		app.core.DepositCtrl,
	)
	accountv1.RegisterAccountServiceServer(server, accountService)

	if !config.Commons().IsProduction() {
		reflection.Register(server)
	}

	app.grpcServer = server

	listener, err := net.Listen("tcp", ":"+config.GRPC().Port)
	if err != nil {
		return fmt.Errorf("[setupGRPCServer][err: %w]", err)
	}

	app.grpcListener = listener

	return nil
}
