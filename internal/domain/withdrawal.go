package domain

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/pagination"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/services/user"
)

// WithdrawalStatus represents the status of a withdrawal request
type WithdrawalStatus uint8

const (
	WithdrawalStatusNew WithdrawalStatus = iota + 1
	WithdrawalStatusPending
	WithdrawalStatusSuccess
	WithdrawalStatusFailed
	// WithdrawalStatusCancelled is never assigned: nothing in this service
	// cancels a withdrawal, and performWithdrawalTransaction has no ledger
	// treatment for it. Kept for the same reason as
	// JournalBizTypeWithdrawalCancel — the value is part of the persisted status
	// space and the history filter accepts the name.
	WithdrawalStatusCancelled
	WithdrawalStatusBlocked
	WithdrawalStatusRejected
	WithdrawalStatusManualReview
)

var withdrawalStatusToString = map[WithdrawalStatus]string{
	WithdrawalStatusNew:          "new",
	WithdrawalStatusPending:      "pending",
	WithdrawalStatusSuccess:      "success",
	WithdrawalStatusFailed:       "failed",
	WithdrawalStatusCancelled:    "cancelled",
	WithdrawalStatusBlocked:      "blocked",
	WithdrawalStatusRejected:     "rejected",
	WithdrawalStatusManualReview: "manual_review",
}

var _withdrawalStatusByString = map[string]WithdrawalStatus{
	"new":           WithdrawalStatusNew,
	"pending":       WithdrawalStatusPending,
	"success":       WithdrawalStatusSuccess,
	"failed":        WithdrawalStatusFailed,
	"cancelled":     WithdrawalStatusCancelled,
	"blocked":       WithdrawalStatusBlocked,
	"rejected":      WithdrawalStatusRejected,
	"manual_review": WithdrawalStatusManualReview,
}

// String returns the string representation of the WithdrawalStatus
func (ws WithdrawalStatus) String() string {
	if str, ok := withdrawalStatusToString[ws]; ok {
		return str
	}
	return "unknown"
}

// NewWithdrawalStatusFromString converts a transport string to WithdrawalStatus.
func NewWithdrawalStatusFromString(value string) (WithdrawalStatus, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	status, ok := _withdrawalStatusByString[normalized]
	if ok {
		return status, nil
	}

	return 0, fmt.Errorf("[domain][NewWithdrawalStatusFromString][err: unsupported withdrawal status %q]", value)
}

// IsSupportedWithdrawalStatus reports whether the withdrawal status is supported.
func IsSupportedWithdrawalStatus(status WithdrawalStatus) bool {
	_, ok := withdrawalStatusToString[status]
	return ok
}

// SupportedWithdrawalStatusStrings returns transport strings for supported withdrawal statuses.
func SupportedWithdrawalStatusStrings() []string {
	return slices.Sorted(maps.Keys(_withdrawalStatusByString))
}

// NewWithdrawalStatus creates a WithdrawalStatus from a WithdrawalStatus value
func NewWithdrawalStatus(value uint8) WithdrawalStatus {
	return WithdrawalStatus(value)
}

// Uint8 converts WithdrawalStatus to uint8
func (ws WithdrawalStatus) Uint8() uint8 {
	return uint8(ws)
}

// WithdrawalRequest represents a withdrawal transaction request
type WithdrawalRequest struct {
	ID             ID
	UserID         ID
	AssetCode      currency.AssetCode
	Network        ChainNetwork
	ToAddress      string
	Amount         decimal.Decimal
	Fee            decimal.Decimal
	FeeTier        FeeTier
	Description    *string
	Status         WithdrawalStatus
	BlockchainTxID *ID
	CreatedAt      time.Time
	ConfirmedAt    *time.Time
	UpdatedAt      *time.Time
}

// GetAmountInBaseUnits returns the amount in base units according to the asset's scale
func (wr *WithdrawalRequest) GetAmountInBaseUnits() decimal.Decimal {
	return wr.Amount.Shift(-currency.DefaultAssetScale)
}

// GetFeeInBaseUnits returns the fee in base units according to the asset's scale'
func (wr *WithdrawalRequest) GetFeeInBaseUnits() decimal.Decimal {
	return wr.Fee.Shift(-currency.DefaultAssetScale)
}

// CanBeRejected returns true if the withdrawal request can be rejected
func (wr *WithdrawalRequest) CanBeRejected() bool {
	return wr.Status == WithdrawalStatusFailed || wr.Status == WithdrawalStatusManualReview
}

// GetIdempotencyKeyForBlockchain generates an idempotency key for the withdrawal request
func (wr *WithdrawalRequest) GetIdempotencyKeyForBlockchain() string {
	// withdrawal_request_id_network_to_address
	return fmt.Sprintf("withdrawal_request_%d_%s_%s", wr.ID, wr.Network, wr.ToAddress)
}

// WithdrawalRequestWithTransactionDetails represents a withdrawal transaction request with blockchain extra data
type WithdrawalRequestWithTransactionDetails struct {
	Withdrawal  WithdrawalRequest
	Transaction BlockchainTx
}

type InitWithdrawalParams struct {
	User      user.User
	AssetCode currency.AssetCode
	Network   ChainNetwork
	ToAddress string
	Amount    decimal.Decimal
	FeeTier   FeeTier
}

// UserID returns the ID of the user initiating the withdrawal
func (i InitWithdrawalParams) UserID() ID {
	return NewIDFromUint64(i.User.ID)
}

// NullableUserID returns the ID of the user initiating the withdrawal as a pointer
func (i InitWithdrawalParams) NullableUserID() *ID {
	return NewNullableIDFromUint64(&i.User.ID)
}

// AmountQuantized returns the amount shifted to quantized value.
func (i InitWithdrawalParams) AmountQuantized() decimal.Decimal {
	return i.Amount.Shift(currency.DefaultAssetScale)
}

type ProcessWithdrawalParams struct {
	BlockchainTxID ID
}

type SendWithdrawalToBlockchain struct {
	WithdrawalRequestID ID
}

type GetWithdrawalRequestParams struct {
	UserID         *ID
	ToAddress      *string
	AssetCode      *currency.AssetCode
	Network        *ChainNetwork
	BlockchainTxID *ID
	Statuses       []WithdrawalStatus
}

type WithdrawalRequestListResult struct {
	List       []WithdrawalRequest
	Pagination pagination.Pagination
}

type WithdrawalFilter struct {
	Limit     int
	Offset    int
	AssetCode *string
	Status    *WithdrawalStatus
}

type UpdateWithdrawalRequestParams struct {
	Status         WithdrawalStatus
	Description    *string
	BlockchainTxID *ID
	ConfirmedAt    *time.Time
}

// WithdrawalRequestRepository defines operations for withdrawal requests
type WithdrawalRequestRepository interface {
	Create(context.Context, *WithdrawalRequest) error
	Update(context.Context, ID, UpdateWithdrawalRequestParams) error
	GetByID(context.Context, ID) (WithdrawalRequest, error)
	GetByIDForUpdate(context.Context, ID) (WithdrawalRequest, error)
	GetByBlockchainIDForUpdate(context.Context, ID) (WithdrawalRequest, error)
	WithdrawalList(context.Context, ID, WithdrawalFilter) ([]WithdrawalRequest, int64, error)
	// FindStuckByStatus returns up to limit withdrawal requests with the given
	// status whose age exceeds olderThan. Used by the reconciliation cron to
	// detect withdrawals stuck because their queue message died.
	//
	// Age is measured from created_at for WithdrawalStatusNew (no update has
	// happened yet) and from updated_at for WithdrawalStatusPending.
	// Ordering is ascending by id so the oldest stuck records are processed
	// first within each cron tick's batch.
	FindStuckByStatus(context.Context, WithdrawalStatus, time.Duration, int) ([]WithdrawalRequest, error)

	WithTxFromContext(context.Context) WithdrawalRequestRepository
}

type WithdrawalController interface {
	Init(context.Context, InitWithdrawalParams) error
	Process(context.Context, ProcessWithdrawalParams) error
	SendToBlockchain(context.Context, SendWithdrawalToBlockchain) error
	Reject(context.Context, ID) error
	// ReconcileStuck runs one reconciliation tick: poll node for stuck
	// Pending withdrawals (Pass 1) and resend stuck New withdrawals (Pass 2).
	// Idempotent and safe to call concurrently with the queue pipeline; the
	// existing processingLocker plus status guards serialize work per
	// withdrawal ID.
	ReconcileStuck(context.Context, ReconcileStuckParams) error
}

// ReconcileStuckParams configures one reconciliation tick.
type ReconcileStuckParams struct {
	// PendingThreshold defines how old a Pending withdrawal must be before
	// the cron polls node-service for its on-chain status.
	PendingThreshold time.Duration
	// NewThreshold defines how old a New withdrawal must be before the cron
	// re-invokes SendToBlockchain. Should be greater than the queue retry
	// budget so the cron does not race the consumer.
	NewThreshold time.Duration
	// BatchSize caps records per pass per tick. Each pass processes at most
	// BatchSize records; the next tick picks up where this one left off.
	BatchSize int
}
