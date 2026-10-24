package app

import (
	"fmt"
	"time"

	"account-service/config"
	"account-service/internal/domain"
	queueHandlers "account-service/internal/transport/queue/handlers"

	wagsrmq "github.com/wagslane/go-rabbitmq"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/queue"
	"github.com/whagency/core-common/queue/rabbitmq"
)

// retryConfig groups the retry-policy parameters needed to build a
// rabbitmq.ConsumerConfig with the platform retry/DLQ pipeline enabled.
type retryConfig struct {
	queueName  string
	maxRetries int
	delays     []time.Duration
}

// retryConfigForConsumer builds the ConsumerConfig fields that enable the
// retry/DLQ pipeline. The retry/DLQ exchange names follow the platform
// default convention ("<queue>.retry" / "<queue>.dlq").
//
// When maxRetries <= 0 the retry pipeline is disabled (legacy NackRequeue
// behaviour) and the consumer config receives no retry/DLQ exchanges.
func retryConfigForConsumer(rc retryConfig) (*rabbitmq.RetryPolicy, string, string) {
	if rc.maxRetries <= 0 {
		return nil, "", ""
	}
	policy := &rabbitmq.RetryPolicy{
		MaxRetries: rc.maxRetries,
		Delays:     rc.delays,
	}
	return policy,
		rabbitmq.DefaultRetryExchangeName(rc.queueName),
		rabbitmq.DefaultDLQExchangeName(rc.queueName)
}

func retryTopologiesForQueueSettings(q config.QueueSettings) []rabbitmq.RetryTopology {
	topologies := make([]rabbitmq.RetryTopology, 0, 4)
	if q.UserCreatedLifecycleRetryMaxRetries > 0 {
		topologies = append(topologies, rabbitmq.RetryTopology{
			QueueName:     q.UserCreatedLifecycleQueueName,
			RoutingKey:    q.UserCreatedLifecycleRoutingKey,
			RetryExchange: rabbitmq.DefaultRetryExchangeName(q.UserCreatedLifecycleQueueName),
			DLQExchange:   rabbitmq.DefaultDLQExchangeName(q.UserCreatedLifecycleQueueName),
			DLQQueueName:  rabbitmq.DefaultDLQQueueName(q.UserCreatedLifecycleQueueName),
		})
	}
	if q.BlockchainTransactionLifecycleRetryMaxRetries > 0 {
		topologies = append(topologies, rabbitmq.RetryTopology{
			QueueName:     q.BlockchainTransactionLifecycleQueueName,
			RoutingKey:    q.BlockchainTransactionLifecycleRoutingKey,
			RetryExchange: rabbitmq.DefaultRetryExchangeName(q.BlockchainTransactionLifecycleQueueName),
			DLQExchange:   rabbitmq.DefaultDLQExchangeName(q.BlockchainTransactionLifecycleQueueName),
			DLQQueueName:  rabbitmq.DefaultDLQQueueName(q.BlockchainTransactionLifecycleQueueName),
		})
	}
	if q.WithdrawalRequestLifecycleRetryMaxRetries > 0 {
		topologies = append(topologies, rabbitmq.RetryTopology{
			QueueName:     q.WithdrawalRequestLifecycleQueueName,
			RoutingKey:    q.WithdrawalRequestLifecycleRoutingKey,
			RetryExchange: rabbitmq.DefaultRetryExchangeName(q.WithdrawalRequestLifecycleQueueName),
			DLQExchange:   rabbitmq.DefaultDLQExchangeName(q.WithdrawalRequestLifecycleQueueName),
			DLQQueueName:  rabbitmq.DefaultDLQQueueName(q.WithdrawalRequestLifecycleQueueName),
		})
	}
	if q.TransactionRollbackRetryMaxRetries > 0 {
		topologies = append(topologies, rabbitmq.RetryTopology{
			QueueName:     q.TransactionRollbackQueueName,
			RoutingKey:    q.TransactionRollbackRoutingKey,
			RetryExchange: rabbitmq.DefaultRetryExchangeName(q.TransactionRollbackQueueName),
			DLQExchange:   rabbitmq.DefaultDLQExchangeName(q.TransactionRollbackQueueName),
			DLQQueueName:  rabbitmq.DefaultDLQQueueName(q.TransactionRollbackQueueName),
		})
	}
	return topologies
}

// setupQueueRetryTopologies declares the retry+DLQ topology for every consumer
// queue that opts into the retry pipeline. It uses a short-lived dedicated
// AMQP connection per queue and is idempotent, so it is safe to run on every
// service start.
func (app *Application) setupQueueRetryTopologies() error {
	amqpURL := rabbitmq.BuildAMQPURL(queue.ConnectionConfig{
		Host:     config.Queue().Host,
		Port:     config.Queue().Port,
		User:     config.Queue().User,
		Password: config.Queue().Password,
	})

	topologies := retryTopologiesForQueueSettings(config.Queue())
	if len(topologies) == 0 {
		logger.Log().Info("[Application:setupQueueRetryTopologies][msg: retry topology disabled]")
		return nil
	}

	for _, t := range topologies {
		if err := rabbitmq.DeclareRetryTopology(amqpURL, t); err != nil {
			return fmt.Errorf("[setupQueueRetryTopologies][queue:%s][err:%w]", t.QueueName, err)
		}
		logger.Log().Infof("[Application:setupQueueRetryTopologies][queue:%s][retry:%s][dlq:%s]",
			t.QueueName, t.RetryExchange, t.DLQExchange)
	}

	return nil
}

// CloseQueueConnections closes the queue connections.
func (app *Application) CloseQueueConnections() {
	logger.Log().Info("[Application:CloseQueueConnections][msg: closing queue connections]")

	for _, conn := range app.queueConnections {
		if err := conn.Close(); err != nil {
			logger.Log().Errorf("[Application:CloseQueueConnections]%s", err)
		}
	}
}

// setupQueueConnections sets up the queue connections.
func (app *Application) setupQueueConnections() error {
	// Add the default queue connection
	if err := app.addNewQueueConnection(domain.DefaultQueueConnectionType, queue.ConnectionConfig{
		Host:     config.Queue().Host,
		Port:     config.Queue().Port,
		User:     config.Queue().User,
		Password: config.Queue().Password,
	}); err != nil {
		return fmt.Errorf("[app:setupQueue]%w", err)
	}

	return nil
}

// addNewQueueConnection adds a new queue connection to the application.
func (app *Application) addNewQueueConnection(queueType queue.ConnectionType, queueConnectionConfig queue.ConnectionConfig) error {
	logger.Log().Infof("[Application:addNewQueueConnection][queueType: %d][amqp://%s:hidden@%s:%s/%s]", queueType, queueConnectionConfig.User, queueConnectionConfig.Host, queueConnectionConfig.Port, queueConnectionConfig.VHost)

	conn, err := wagsrmq.NewConn(
		fmt.Sprintf("amqp://%s:%s@%s:%s/%s", queueConnectionConfig.User, queueConnectionConfig.Password, queueConnectionConfig.Host, queueConnectionConfig.Port, queueConnectionConfig.VHost),
		wagsrmq.WithConnectionOptionsLogging,
		wagsrmq.WithConnectionOptionsLogger(rabbitmq.QueueLogger{}),
		wagsrmq.WithConnectionOptionsReconnectInterval(config.Queue().ReconnectInterval),
	)
	if err != nil {
		return fmt.Errorf("[addNewQueueConnection][err: %w]", err)
	}

	app.queueConnections[queueType] = conn

	return nil
}

// setupQueueManager sets up the queue manager for the application.
func (app *Application) setupQueueManager() error {
	// Define queue manager
	app.queueManager = queue.NewManager()
	return nil
}

// setupQueuePublishers sets up the queue publishers for the application.
func (app *Application) setupQueuePublishers() error {
	// Define publishers
	pushPublisher, err := rabbitmq.NewPublisher(
		app.queueConnections[domain.DefaultQueueConnectionType],
		rabbitmq.PublisherConfig{
			PublisherType:      domain.PushQueuePublisherType,
			ExchangeName:       config.Queue().PushExchangeName,
			ExchangeType:       config.Queue().PushExchangeType,
			Mandatory:          config.Queue().PushExchangeMandatory,
			Persistent:         config.Queue().PushExchangePersistent,
			DefaultRoutingKeys: []string{config.Queue().PushRoutingKey},
		})
	if err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	// Add publishers to the manager
	if err = app.queueManager.AddPublisher(pushPublisher); err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	blockchainTransactionLifecyclePublisher, err := rabbitmq.NewPublisher(
		app.queueConnections[domain.DefaultQueueConnectionType],
		rabbitmq.PublisherConfig{
			PublisherType:      domain.BlockchainTransactionLifecycleQueuePublisherType,
			ExchangeName:       config.Queue().BlockchainTransactionLifecycleExchangeName,
			ExchangeType:       config.Queue().BlockchainTransactionLifecycleExchangeType,
			Mandatory:          config.Queue().BlockchainTransactionLifecycleExchangeMandatory,
			Persistent:         config.Queue().BlockchainTransactionLifecycleExchangePersistent,
			DefaultRoutingKeys: []string{config.Queue().BlockchainTransactionLifecycleRoutingKey},
		})
	if err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	// Add publishers to the manager
	if err = app.queueManager.AddPublisher(blockchainTransactionLifecyclePublisher); err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	withdrawalRequestLifecyclePublisher, err := rabbitmq.NewPublisher(
		app.queueConnections[domain.DefaultQueueConnectionType],
		rabbitmq.PublisherConfig{
			PublisherType:      domain.WithdrawalRequestLifecycleQueuePublisherType,
			ExchangeName:       config.Queue().WithdrawalRequestLifecycleExchangeName,
			ExchangeType:       config.Queue().WithdrawalRequestLifecycleExchangeType,
			Mandatory:          config.Queue().WithdrawalRequestLifecycleExchangeMandatory,
			Persistent:         config.Queue().WithdrawalRequestLifecycleExchangePersistent,
			DefaultRoutingKeys: []string{config.Queue().WithdrawalRequestLifecycleRoutingKey},
		})
	if err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	// Add publishers to the manager
	if err = app.queueManager.AddPublisher(withdrawalRequestLifecyclePublisher); err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	transactionRollbackPublisher, err := rabbitmq.NewPublisher(
		app.queueConnections[domain.DefaultQueueConnectionType],
		rabbitmq.PublisherConfig{
			PublisherType:      domain.TransactionRollbackQueuePublisherType,
			ExchangeName:       config.Queue().TransactionRollbackExchangeName,
			ExchangeType:       config.Queue().TransactionRollbackExchangeType,
			Mandatory:          config.Queue().TransactionRollbackExchangeMandatory,
			Persistent:         config.Queue().TransactionRollbackExchangePersistent,
			DefaultRoutingKeys: []string{config.Queue().TransactionRollbackRoutingKey},
		})
	if err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	// Add publishers to the manager
	if err = app.queueManager.AddPublisher(transactionRollbackPublisher); err != nil {
		return fmt.Errorf("[setupQueuePublishers]%w", err)
	}

	return nil
}

// setupQueueConsumers sets up the queue consumers for the application.
func (app *Application) setupQueueConsumers() error {
	// Build retry policies for each consumer.
	userCreatedRetry, userCreatedRetryEx, userCreatedDLQEx := retryConfigForConsumer(retryConfig{
		queueName:  config.Queue().UserCreatedLifecycleQueueName,
		maxRetries: config.Queue().UserCreatedLifecycleRetryMaxRetries,
		delays:     config.Queue().UserCreatedLifecycleRetryDelays,
	})
	blockchainTxRetry, blockchainTxRetryEx, blockchainTxDLQEx := retryConfigForConsumer(retryConfig{
		queueName:  config.Queue().BlockchainTransactionLifecycleQueueName,
		maxRetries: config.Queue().BlockchainTransactionLifecycleRetryMaxRetries,
		delays:     config.Queue().BlockchainTransactionLifecycleRetryDelays,
	})
	withdrawalRetry, withdrawalRetryEx, withdrawalDLQEx := retryConfigForConsumer(retryConfig{
		queueName:  config.Queue().WithdrawalRequestLifecycleQueueName,
		maxRetries: config.Queue().WithdrawalRequestLifecycleRetryMaxRetries,
		delays:     config.Queue().WithdrawalRequestLifecycleRetryDelays,
	})
	rollbackRetry, rollbackRetryEx, rollbackDLQEx := retryConfigForConsumer(retryConfig{
		queueName:  config.Queue().TransactionRollbackQueueName,
		maxRetries: config.Queue().TransactionRollbackRetryMaxRetries,
		delays:     config.Queue().TransactionRollbackRetryDelays,
	})

	// Define consumers
	userCreatedLifeCycleConsumer, err := rabbitmq.NewConsumer(
		app.queueConnections[domain.DefaultQueueConnectionType],
		queueHandlers.NewUserCreatedLifeCycleHandler(app.core.AccountCtrl),
		rabbitmq.ConsumerConfig{
			ConsumerName:   config.Queue().UserCreatedLifecycleConsumerName,
			RoutingKey:     config.Queue().UserCreatedLifecycleRoutingKey,
			QueueName:      config.Queue().UserCreatedLifecycleQueueName,
			ExchangeName:   config.Queue().UserLifecycleExchangeName,
			ExchangeType:   config.Queue().UserLifecycleExchangeType,
			ConsumerNumber: config.Queue().UserLifecycleConsumers,
			PrefetchCount:  config.Queue().UserLifecyclePrefetchCount,
			Retry:          userCreatedRetry,
			RetryExchange:  userCreatedRetryEx,
			DLQExchange:    userCreatedDLQEx,
		})
	if err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	// Add consumers to the manager
	if err = app.queueManager.AddConsumer(userCreatedLifeCycleConsumer); err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	blockchainTransactionLifecycleConsumer, err := rabbitmq.NewConsumer(
		app.queueConnections[domain.DefaultQueueConnectionType],
		queueHandlers.NewBlockchainTransactionLifecycleHandler(app.core.DepositCtrl, app.core.WithdrawalCtrl),
		rabbitmq.ConsumerConfig{
			QueueName:      config.Queue().BlockchainTransactionLifecycleQueueName,
			ExchangeName:   config.Queue().BlockchainTransactionLifecycleExchangeName,
			ExchangeType:   config.Queue().BlockchainTransactionLifecycleExchangeType,
			RoutingKey:     config.Queue().BlockchainTransactionLifecycleRoutingKey,
			ConsumerName:   config.Queue().BlockchainTransactionLifecycleConsumerName,
			ConsumerNumber: config.Queue().BlockchainTransactionLifecycleConsumers,
			PrefetchCount:  config.Queue().BlockchainTransactionLifecyclePrefetchCount,
			Retry:          blockchainTxRetry,
			RetryExchange:  blockchainTxRetryEx,
			DLQExchange:    blockchainTxDLQEx,
		})
	if err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	// Add consumers to the manager
	if err = app.queueManager.AddConsumer(blockchainTransactionLifecycleConsumer); err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	withdrawalRequestLifecycleConsumer, err := rabbitmq.NewConsumer(
		app.queueConnections[domain.DefaultQueueConnectionType],
		queueHandlers.NewWithdrawalRequestLifecycleHandler(app.core.WithdrawalCtrl),
		rabbitmq.ConsumerConfig{
			QueueName:      config.Queue().WithdrawalRequestLifecycleQueueName,
			ExchangeName:   config.Queue().WithdrawalRequestLifecycleExchangeName,
			ExchangeType:   config.Queue().WithdrawalRequestLifecycleExchangeType,
			RoutingKey:     config.Queue().WithdrawalRequestLifecycleRoutingKey,
			ConsumerName:   config.Queue().WithdrawalRequestLifecycleConsumerName,
			ConsumerNumber: config.Queue().WithdrawalRequestLifecycleConsumers,
			PrefetchCount:  config.Queue().WithdrawalRequestLifecyclePrefetchCount,
			Retry:          withdrawalRetry,
			RetryExchange:  withdrawalRetryEx,
			DLQExchange:    withdrawalDLQEx,
		})
	if err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	// Add consumers to the manager
	if err = app.queueManager.AddConsumer(withdrawalRequestLifecycleConsumer); err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	transactionRollbackConsumer, err := rabbitmq.NewConsumer(
		app.queueConnections[domain.DefaultQueueConnectionType],
		queueHandlers.NewTransactionRollbackHandler(app.core.TransactionCtrl),
		rabbitmq.ConsumerConfig{
			QueueName:      config.Queue().TransactionRollbackQueueName,
			ExchangeName:   config.Queue().TransactionRollbackExchangeName,
			ExchangeType:   config.Queue().TransactionRollbackExchangeType,
			RoutingKey:     config.Queue().TransactionRollbackRoutingKey,
			ConsumerName:   config.Queue().TransactionRollbackConsumerName,
			ConsumerNumber: config.Queue().TransactionRollbackConsumers,
			PrefetchCount:  config.Queue().TransactionRollbackPrefetchCount,
			Retry:          rollbackRetry,
			RetryExchange:  rollbackRetryEx,
			DLQExchange:    rollbackDLQEx,
		})
	if err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	// Add consumers to the manager
	if err = app.queueManager.AddConsumer(transactionRollbackConsumer); err != nil {
		return fmt.Errorf("[setupQueueConsumers]%w", err)
	}

	return nil
}
