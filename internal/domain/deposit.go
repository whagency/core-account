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

type InitDepositParams struct {
	User      user.User
	AssetCode currency.AssetCode
	Network   ChainNetwork
}

// UserID returns the ID of the user initiating the deposit
func (p InitDepositParams) UserID() ID {
	return NewIDFromUint64(p.User.ID)
}

// NullableUserID returns the ID of the user initiating the deposit as a pointer
func (p InitDepositParams) NullableUserID() *ID {
	return NewNullableIDFromUint64(&p.User.ID)
}

type InitDepositResult struct {
	AddressAssignment AddressAssignment
	ChainConfig       ChainConfig
}

type ProcessDepositParams struct {
	BlockchainTxID ID
}

type DepositStatus uint8

const (
	DepositStatusNew DepositStatus = iota + 1
	DepositStatusPending
	DepositStatusSuccess
	DepositStatusFailed
	DepositStatusBlocked
	DepositStatusManualReview
)

// NewDepositStatus creates a DepositStatus from a uint8 value
func NewDepositStatus(value uint8) DepositStatus {
	return DepositStatus(value)
}

// Uint8 converts DepositStatus to uint8
func (ds DepositStatus) Uint8() uint8 {
	return uint8(ds)
}

var depositStatusToString = map[DepositStatus]string{
	DepositStatusNew:          "new",
	DepositStatusPending:      "pending",
	DepositStatusSuccess:      "success",
	DepositStatusFailed:       "failed",
	DepositStatusBlocked:      "blocked",
	DepositStatusManualReview: "manual_review",
}

var _depositStatusByString = map[string]DepositStatus{
	"new":           DepositStatusNew,
	"pending":       DepositStatusPending,
	"success":       DepositStatusSuccess,
	"failed":        DepositStatusFailed,
	"blocked":       DepositStatusBlocked,
	"manual_review": DepositStatusManualReview,
}

// String returns the string representation of the DepositStatus
func (ds DepositStatus) String() string {
	if str, ok := depositStatusToString[ds]; ok {
		return str
	}
	return "unknown"
}

// NewDepositStatusFromString converts a transport string to DepositStatus.
func NewDepositStatusFromString(value string) (DepositStatus, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	status, ok := _depositStatusByString[normalized]
	if ok {
		return status, nil
	}

	return 0, fmt.Errorf("[domain][NewDepositStatusFromString][err: unsupported deposit status %q]", value)
}

// IsSupportedDepositStatus reports whether the deposit status is supported.
func IsSupportedDepositStatus(status DepositStatus) bool {
	_, ok := depositStatusToString[status]
	return ok
}

// SupportedDepositStatusStrings returns transport strings for supported deposit statuses.
func SupportedDepositStatusStrings() []string {
	return slices.Sorted(maps.Keys(_depositStatusByString))
}

// DepositRequest represents a deposit transaction request
type DepositRequest struct {
	ID                 ID
	UserID             ID
	AssetCode          currency.AssetCode
	Network            ChainNetwork
	AddressAssigmentID *ID
	BlockchainTxID     *ID
	Amount             decimal.Decimal
	Fee                decimal.Decimal
	Status             DepositStatus
	Description        *string
	CreatedAt          time.Time
	UpdatedAt          *time.Time
	ConfirmedAt        *time.Time
}

// GetAmountInBaseUnits converts the amount from quants to base units
func (dr DepositRequest) GetAmountInBaseUnits() decimal.Decimal {
	return dr.Amount.Shift(-currency.DefaultAssetScale)
}

type DepositRequestDetails struct {
	ID          ID
	UserID      ID
	AssetCode   currency.AssetCode
	Network     ChainNetwork
	Address     string
	Amount      decimal.Decimal
	Fee         decimal.Decimal
	Status      DepositStatus
	Description *string
	CreatedAt   time.Time
}

// DepositRequestWithTransactionDetails represents a deposit transaction request with blockchain extra data
type DepositRequestWithTransactionDetails struct {
	Deposit     DepositRequest
	Transaction BlockchainTx
}

type GetDepositRequestParams struct {
	UserID              ID
	AddressAssignmentID *ID
	AssetCode           currency.AssetCode
	Network             ChainNetwork
	BlockchainTxID      *ID
	Statuses            []DepositStatus
}

type DepositRequestListResult struct {
	List       []DepositRequestDetails
	Pagination pagination.Pagination
}

type DepositFilter struct {
	Limit     int
	Offset    int
	AssetCode *string
	Status    *DepositStatus
}

type UpdateDepositRequestStatusParams struct {
	Status      DepositStatus
	Description *string
	ConfirmedAt *time.Time
}

// DepositRequestRepository defines operations for deposit requests
type DepositRequestRepository interface {
	Create(context.Context, *DepositRequest) error
	GetByParams(context.Context, GetDepositRequestParams) (DepositRequest, error)
	GetByBlockchainID(context.Context, ID) (DepositRequest, error)
	UpdateStatus(context.Context, ID, UpdateDepositRequestStatusParams) error
	GetByID(context.Context, ID) (DepositRequest, error)
	GetByIDForUpdate(context.Context, ID) (DepositRequest, error)
	GetManualApprove(context.Context, ID) ([]DepositRequest, error)
	DepositList(context.Context, ID, DepositFilter) ([]DepositRequestDetails, int64, error)

	WithTxFromContext(context.Context) DepositRequestRepository
}

// DepositController defines the interface for deposit operations
type DepositController interface {
	Init(context.Context, InitDepositParams) (InitDepositResult, error)
	Process(context.Context, ProcessDepositParams) error
	Approve(context.Context, ID) error
	AutomaticManualApprove(context.Context, ID) error
	// ReconcileStuck runs one reconciliation tick: for each inbound
	// BlockchainTx that has been around longer than Threshold and has no
	// terminal Success deposit_request yet, drives the standard Process()
	// path. Recovers deposits whose blockchain.transaction.event was lost
	// between the callback webhook and the consumer.
	ReconcileStuck(context.Context, ReconcileStuckDepositParams) error
}

// ReconcileStuckDepositParams configures one deposit reconciliation tick.
type ReconcileStuckDepositParams struct {
	// Threshold defines how old a candidate inbound BlockchainTx must be
	// before the cron picks it up. Should be larger than the queue retry
	// budget so the cron does not race the consumer.
	Threshold time.Duration
	// BatchSize caps records per tick. Records are scanned ASC by id so
	// the oldest stuck rows are processed first.
	BatchSize int
}
