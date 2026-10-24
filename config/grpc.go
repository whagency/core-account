package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

var grpcSettings GRPCSettings

type GRPCSettings struct {
	Port                 string        `envconfig:"GRPC_PORT" default:"50051"`
	ConnectionTimeout    time.Duration `envconfig:"GRPC_CONNECTION_TIMEOUT" default:"10s"`
	RequestTimeout       time.Duration `envconfig:"GRPC_REQUEST_TIMEOUT" default:"5s"`         // Timeout per RPC call
	MaxConcurrentStreams uint32        `envconfig:"GRPC_MAX_CONCURRENT_STREAMS" default:"200"` // limit concurrent requests (2x DB pool)
	MaxRecvMsgSize       int           `envconfig:"GRPC_MAX_RECV_MSG_SIZE" default:"4194304" ` // 4MB (4 * 1024 * 1024)
	MaxSendMsgSize       int           `envconfig:"GRPC_MAX_SEND_MSG_SIZE" default:"4194304"`  // 4MB (4 * 1024 * 1024)
	KeepaliveTime        time.Duration `envconfig:"GRPC_KEEP_ALIVE_TIME" default:"2h"`
	KeepaliveTimeout     time.Duration `envconfig:"GRPC_KEEP_ALIVE_TIMEOUT" default:"20s"`
	MaxConnectionIdle    time.Duration `envconfig:"GRPC_MAX_CONNECTION_IDLE" default:"0"` // disabled: keep reusable client transports open
}

// GRPC returns the gRPC settings
func GRPC() GRPCSettings {
	return grpcSettings
}

// loadGRPCConfig loads the gRPC configuration from the environment variables
func loadGRPCConfig() error {
	if err := envconfig.Process("", &grpcSettings); err != nil {
		return fmt.Errorf("[loadGRPCConfig][err: %w]", err)
	}

	return nil
}
