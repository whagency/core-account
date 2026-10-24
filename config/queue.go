package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

var queueSettings QueueSettings

type QueueSettings struct {
	Host              string        `envconfig:"QUEUE_HOST" required:"true"`
	Port              string        `envconfig:"QUEUE_PORT" required:"true"`
	User              string        `envconfig:"QUEUE_USER" required:"true"`
	Password          string        `envconfig:"QUEUE_PASSWORD" required:"true"`
	ReconnectInterval time.Duration `envconfig:"QUEUE_RECONNECT_INTERVAL" default:"30s"`

	PushExchangeName       string `envconfig:"QUEUE_PUSH_EXCHANGE_NAME" default:"account.push"`
	PushExchangeType       string `envconfig:"QUEUE_PUSH_EXCHANGE_TYPE" default:"direct"`
	PushExchangePersistent bool   `envconfig:"QUEUE_PUSH_EXCHANGE_PERSISTENT" default:"true"`
	PushExchangeMandatory  bool   `envconfig:"QUEUE_PUSH_EXCHANGE_MANDATORY" default:"true"`
	PushRoutingKey         string `envconfig:"QUEUE_PUSH_ROUTING_KEY" default:"push.dispatch"`

	UserLifecycleExchangeName  string `envconfig:"QUEUE_USER_LIFECYCLE_EXCHANGE_NAME" default:"user.lifecycle"`
	UserLifecycleExchangeType  string `envconfig:"QUEUE_USER_LIFECYCLE_EXCHANGE_TYPE" default:"topic"`
	UserLifecyclePrefetchCount int    `envconfig:"QUEUE_USER_LIFECYCLE_PREFETCH_COUNT" default:"100"`
	UserLifecycleConsumers     int    `envconfig:"QUEUE_USER_LIFECYCLE_CONSUMERS" default:"1"`

	UserCreatedLifecycleConsumerName string `envconfig:"QUEUE_USER_CREATED_LIFECYCLE_CONSUMER_NAME" default:"account.user.created.consumer"`
	UserCreatedLifecycleRoutingKey   string `envconfig:"QUEUE_USER_CREATED_LIFECYCLE_ROUTING_KEY" default:"user.created"`
	UserCreatedLifecycleQueueName    string `envconfig:"QUEUE_USER_CREATED_LIFECYCLE_QUEUE_NAME" default:"account.user.created"`

	UserCreatedLifecycleRetryMaxRetries int             `envconfig:"QUEUE_USER_CREATED_LIFECYCLE_RETRY_MAX_RETRIES" default:"5"`
	UserCreatedLifecycleRetryDelays     []time.Duration `envconfig:"QUEUE_USER_CREATED_LIFECYCLE_RETRY_DELAYS" default:"30s,2m,10m,30m,2h"`

	BlockchainTransactionLifecycleExchangeName       string `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_EXCHANGE_NAME" default:"blockchain.transaction.lifecycle"`
	BlockchainTransactionLifecycleExchangeType       string `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_EXCHANGE_TYPE" default:"topic"`
	BlockchainTransactionLifecycleExchangePersistent bool   `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_EXCHANGE_PERSISTENT" default:"true"`
	BlockchainTransactionLifecycleExchangeMandatory  bool   `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_EXCHANGE_MANDATORY" default:"true"`
	BlockchainTransactionLifecycleRoutingKey         string `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_ROUTING_KEY" default:"blockchain.transaction.event"`
	BlockchainTransactionLifecycleQueueName          string `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_QUEUE_NAME" default:"blockchain.transaction.event"`
	BlockchainTransactionLifecycleConsumerName       string `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_CONSUMER_NAME" default:"blockchain.transaction.lifecycle.consumer"`
	BlockchainTransactionLifecycleConsumers          int    `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_CONSUMERS" default:"1"`
	BlockchainTransactionLifecyclePrefetchCount      int    `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_PREFETCH_COUNT" default:"100"`

	BlockchainTransactionLifecycleRetryMaxRetries int             `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_RETRY_MAX_RETRIES" default:"7"`
	BlockchainTransactionLifecycleRetryDelays     []time.Duration `envconfig:"QUEUE_BLOCKCHAIN_TRANSACTION_LIFECYCLE_RETRY_DELAYS" default:"30s,1m,5m,15m,1h,2h,6h"`

	WithdrawalRequestLifecycleExchangeName       string `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_EXCHANGE_NAME" default:"withdrawal.request.lifecycle"`
	WithdrawalRequestLifecycleExchangeType       string `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_EXCHANGE_TYPE" default:"topic"`
	WithdrawalRequestLifecycleExchangePersistent bool   `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_EXCHANGE_PERSISTENT" default:"true"`
	WithdrawalRequestLifecycleExchangeMandatory  bool   `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_EXCHANGE_MANDATORY" default:"true"`
	WithdrawalRequestLifecycleRoutingKey         string `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_ROUTING_KEY" default:"withdrawal.request.event"`
	WithdrawalRequestLifecycleQueueName          string `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_QUEUE_NAME" default:"withdrawal.request.event"`
	WithdrawalRequestLifecycleConsumerName       string `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_CONSUMER_NAME" default:"withdrawal.request.lifecycle.consumer"`
	WithdrawalRequestLifecycleConsumers          int    `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_CONSUMERS" default:"1"`
	WithdrawalRequestLifecyclePrefetchCount      int    `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_PREFETCH_COUNT" default:"100"`

	WithdrawalRequestLifecycleRetryMaxRetries int             `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_RETRY_MAX_RETRIES" default:"5"`
	WithdrawalRequestLifecycleRetryDelays     []time.Duration `envconfig:"QUEUE_WITHDRAWAL_REQUEST_LIFECYCLE_RETRY_DELAYS" default:"30s,2m,10m,30m,2h"`

	TransactionRollbackExchangeName       string `envconfig:"QUEUE_TRANSACTION_ROLLBACK_EXCHANGE_NAME" default:"transaction.rollback"`
	TransactionRollbackExchangeType       string `envconfig:"QUEUE_TRANSACTION_ROLLBACK_EXCHANGE_TYPE" default:"direct"`
	TransactionRollbackExchangePersistent bool   `envconfig:"QUEUE_TRANSACTION_ROLLBACK_EXCHANGE_PERSISTENT" default:"true"`
	TransactionRollbackExchangeMandatory  bool   `envconfig:"QUEUE_TRANSACTION_ROLLBACK_EXCHANGE_MANDATORY" default:"true"`
	TransactionRollbackRoutingKey         string `envconfig:"QUEUE_TRANSACTION_ROLLBACK_ROUTING_KEY" default:"transaction.rollback.request"`
	TransactionRollbackQueueName          string `envconfig:"QUEUE_TRANSACTION_ROLLBACK_QUEUE_NAME" default:"transaction.rollback.request"`
	TransactionRollbackConsumerName       string `envconfig:"QUEUE_TRANSACTION_ROLLBACK_CONSUMER_NAME" default:"transaction.rollback.consumer"`
	TransactionRollbackConsumers          int    `envconfig:"QUEUE_TRANSACTION_ROLLBACK_CONSUMERS" default:"1"`
	TransactionRollbackPrefetchCount      int    `envconfig:"QUEUE_TRANSACTION_ROLLBACK_PREFETCH_COUNT" default:"100"`

	TransactionRollbackRetryMaxRetries int             `envconfig:"QUEUE_TRANSACTION_ROLLBACK_RETRY_MAX_RETRIES" default:"7"`
	TransactionRollbackRetryDelays     []time.Duration `envconfig:"QUEUE_TRANSACTION_ROLLBACK_RETRY_DELAYS" default:"30s,1m,5m,15m,1h,2h,6h"`
}

// Queue returns the queue settings
func Queue() QueueSettings {
	return queueSettings
}

// loadQueueConfig loads the queue configuration from the environment variables
func loadQueueConfig() error {
	if err := envconfig.Process("", &queueSettings); err != nil {
		return fmt.Errorf("[loadQueueConfig][err: %w]", err)
	}
	return nil
}
