package domain

import (
	"github.com/whagency/core-common/queue"
	commonAccountQueue "github.com/whagency/core-common/services/account/queue"
)

const (
	PushQueuePublisherType queue.PublisherType = iota + 1
	BlockchainTransactionLifecycleQueuePublisherType
	WithdrawalRequestLifecycleQueuePublisherType
	TransactionRollbackQueuePublisherType
)

const DefaultQueueConnectionType queue.ConnectionType = iota + 1

const (
	MessageTypeBlockchainTransactionUpdated queue.MessageType = "blockchain.transaction.updated"
	MessageTypeWithdrawalRequestCreated     queue.MessageType = "withdrawal.request.created"
	MessageTypeTransactionRollbackRequested                   = commonAccountQueue.MessageTypeTransactionRollbackRequested
)

// QueueManager defines the interface for managing queue consumers and publishers
type QueueManager interface {
	AddConsumer(queue.Consumer) error
	AddPublisher(queue.Publisher) error
	GetPublisher(queue.PublisherType) (queue.Publisher, error)
	RunConsumers() error
	CloseConsumers() error
	ClosePublishers() error
}
