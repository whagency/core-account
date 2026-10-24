package app

import (
	"testing"
	"time"

	"account-service/config"

	"github.com/whagency/core-common/queue/rabbitmq"
)

func TestRetryConfigForConsumer(t *testing.T) {
	t.Run("disabled when max retries is zero", func(t *testing.T) {
		policy, retryExchange, dlqExchange := retryConfigForConsumer(retryConfig{
			queueName:  "transaction.rollback.request",
			maxRetries: 0,
			delays:     []time.Duration{time.Second},
		})

		if policy != nil {
			t.Fatalf("expected nil policy, got %#v", policy)
		}
		if retryExchange != "" || dlqExchange != "" {
			t.Fatalf("expected empty exchanges, got retry=%q dlq=%q", retryExchange, dlqExchange)
		}
	})

	t.Run("enabled policy uses max retries and default exchange names", func(t *testing.T) {
		delays := []time.Duration{30 * time.Second, time.Minute}
		policy, retryExchange, dlqExchange := retryConfigForConsumer(retryConfig{
			queueName:  "blockchain.transaction.event",
			maxRetries: 7,
			delays:     delays,
		})

		if policy == nil {
			t.Fatalf("expected policy")
		}
		if policy.MaxRetries != 7 {
			t.Fatalf("MaxRetries = %d; want 7", policy.MaxRetries)
		}
		if len(policy.Delays) != len(delays) {
			t.Fatalf("delays len = %d; want %d", len(policy.Delays), len(delays))
		}
		if retryExchange != rabbitmq.DefaultRetryExchangeName("blockchain.transaction.event") {
			t.Fatalf("retry exchange = %q", retryExchange)
		}
		if dlqExchange != rabbitmq.DefaultDLQExchangeName("blockchain.transaction.event") {
			t.Fatalf("dlq exchange = %q", dlqExchange)
		}
	})
}

func TestRetryTopologiesForQueueSettings(t *testing.T) {
	base := config.QueueSettings{
		UserCreatedLifecycleQueueName:             "account.user.created",
		UserCreatedLifecycleRoutingKey:            "user.created",
		UserCreatedLifecycleRetryDelays:           []time.Duration{time.Second},
		BlockchainTransactionLifecycleQueueName:   "blockchain.transaction.event",
		BlockchainTransactionLifecycleRoutingKey:  "blockchain.transaction.event",
		BlockchainTransactionLifecycleRetryDelays: []time.Duration{time.Second},
		WithdrawalRequestLifecycleQueueName:       "withdrawal.request.event",
		WithdrawalRequestLifecycleRoutingKey:      "withdrawal.request.event",
		WithdrawalRequestLifecycleRetryDelays:     []time.Duration{time.Second},
		TransactionRollbackQueueName:              "transaction.rollback.request",
		TransactionRollbackRoutingKey:             "transaction.rollback.request",
		TransactionRollbackRetryDelays:            []time.Duration{time.Second},
	}

	t.Run("builds all topologies when all retry budgets are enabled", func(t *testing.T) {
		q := base
		q.UserCreatedLifecycleRetryMaxRetries = 5
		q.BlockchainTransactionLifecycleRetryMaxRetries = 7
		q.WithdrawalRequestLifecycleRetryMaxRetries = 5
		q.TransactionRollbackRetryMaxRetries = 7

		topologies := retryTopologiesForQueueSettings(q)
		if len(topologies) != 4 {
			t.Fatalf("topologies len = %d; want 4", len(topologies))
		}
		assertTopology(t, topologies[0], "account.user.created", "user.created")
		assertTopology(t, topologies[1], "blockchain.transaction.event", "blockchain.transaction.event")
		assertTopology(t, topologies[2], "withdrawal.request.event", "withdrawal.request.event")
		assertTopology(t, topologies[3], "transaction.rollback.request", "transaction.rollback.request")
	})

	t.Run("skips disabled queues", func(t *testing.T) {
		q := base
		q.UserCreatedLifecycleRetryMaxRetries = 0
		q.BlockchainTransactionLifecycleRetryMaxRetries = 7
		q.WithdrawalRequestLifecycleRetryMaxRetries = 0
		q.TransactionRollbackRetryMaxRetries = 7

		topologies := retryTopologiesForQueueSettings(q)
		if len(topologies) != 2 {
			t.Fatalf("topologies len = %d; want 2", len(topologies))
		}
		assertTopology(t, topologies[0], "blockchain.transaction.event", "blockchain.transaction.event")
		assertTopology(t, topologies[1], "transaction.rollback.request", "transaction.rollback.request")
	})

	t.Run("returns empty slice when all retry budgets are disabled", func(t *testing.T) {
		topologies := retryTopologiesForQueueSettings(base)
		if len(topologies) != 0 {
			t.Fatalf("topologies len = %d; want 0", len(topologies))
		}
	})
}

func assertTopology(t *testing.T, got rabbitmq.RetryTopology, queueName string, routingKey string) {
	t.Helper()

	if got.QueueName != queueName {
		t.Fatalf("QueueName = %q; want %q", got.QueueName, queueName)
	}
	if got.RoutingKey != routingKey {
		t.Fatalf("RoutingKey = %q; want %q", got.RoutingKey, routingKey)
	}
	if got.RetryExchange != rabbitmq.DefaultRetryExchangeName(queueName) {
		t.Fatalf("RetryExchange = %q", got.RetryExchange)
	}
	if got.DLQExchange != rabbitmq.DefaultDLQExchangeName(queueName) {
		t.Fatalf("DLQExchange = %q", got.DLQExchange)
	}
	if got.DLQQueueName != rabbitmq.DefaultDLQQueueName(queueName) {
		t.Fatalf("DLQQueueName = %q", got.DLQQueueName)
	}
}
