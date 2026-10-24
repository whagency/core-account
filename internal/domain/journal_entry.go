package domain

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// JournalStatus describes the lifecycle of a journal entry.
type JournalStatus uint8

// NewJournalStatus creates a new JournalStatus from a uint8 value.
func NewJournalStatus(status uint8) JournalStatus {
	return JournalStatus(status)
}

const (
	JournalStatusPending JournalStatus = iota + 1
	JournalStatusCompleted
	JournalStatusFailed
	JournalStatusCancelled
	JournalStatusReversed
)

var _journalStatusToString = map[JournalStatus]string{
	JournalStatusPending:   "pending",
	JournalStatusCompleted: "completed",
	JournalStatusFailed:    "failed",
	JournalStatusCancelled: "cancelled",
	JournalStatusReversed:  "reversed",
}

var _journalStatusByString = map[string]JournalStatus{
	"pending":   JournalStatusPending,
	"completed": JournalStatusCompleted,
	"failed":    JournalStatusFailed,
	"cancelled": JournalStatusCancelled,
	"reversed":  JournalStatusReversed,
}

// String returns the string representation of the JournalStatus.
func (j JournalStatus) String() string {
	if s, ok := _journalStatusToString[j]; ok {
		return s
	}
	return "unknown"
}

// Uint8 returns the uint8 representation of the JournalStatus.
func (j JournalStatus) Uint8() uint8 {
	return uint8(j)
}

// NewJournalStatusFromString converts a transport string to JournalStatus.
func NewJournalStatusFromString(value string) (JournalStatus, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	status, ok := _journalStatusByString[normalized]
	if ok {
		return status, nil
	}

	return 0, fmt.Errorf("[domain][NewJournalStatusFromString][err: unsupported journal status %q]", value)
}

// IsSupportedJournalStatus reports whether the journal status is supported.
func IsSupportedJournalStatus(status JournalStatus) bool {
	_, ok := _journalStatusToString[status]
	return ok
}

// SupportedJournalStatusStrings returns transport strings for supported journal statuses.
func SupportedJournalStatusStrings() []string {
	return slices.Sorted(maps.Keys(_journalStatusByString))
}

type JournalBizType uint8

// NewJournalBizType creates a new JournalBizType from a uint8 value.
func NewJournalBizType(bizType uint8) JournalBizType {
	return JournalBizType(bizType)
}

// Uint8 returns the uint8 representation of the JournalBizType.
func (j JournalBizType) Uint8() uint8 {
	return uint8(j)
}

const (
	JournalBizTypeDeposit JournalBizType = iota + 1
	JournalBizTypeWithdrawalRequest
	JournalBizTypeWithdrawalComplete
	JournalBizTypeWithdrawalReject
	// JournalBizTypeWithdrawalCancel has no TxRegistry spec, so no journal entry
	// ever carries it and MakeTransaction rejects it as an unsupported type. The
	// constant stays because the value is part of the persisted biz_type space
	// and the history filter still accepts the name — that filter simply matches
	// nothing. Implementing a cancel flow means adding the spec first.
	JournalBizTypeWithdrawalCancel
	JournalBizTypeTransfer
	JournalBizTypeCheckoutPayment
	JournalBizTypeCheckoutPaymentHold
	JournalBizTypeCheckoutPaymentHoldRelease
)

var _journalBizTypeToString = map[JournalBizType]string{
	JournalBizTypeDeposit:                    "deposit",
	JournalBizTypeWithdrawalRequest:          "withdrawal_request",
	JournalBizTypeWithdrawalComplete:         "withdrawal_complete",
	JournalBizTypeWithdrawalReject:           "withdrawal_reject",
	JournalBizTypeWithdrawalCancel:           "withdrawal_cancel",
	JournalBizTypeTransfer:                   "transfer",
	JournalBizTypeCheckoutPayment:            "checkout_payment",
	JournalBizTypeCheckoutPaymentHold:        "checkout_payment_hold",
	JournalBizTypeCheckoutPaymentHoldRelease: "checkout_payment_hold_release",
}

var _journalBizTypeByString = map[string]JournalBizType{
	"deposit":                       JournalBizTypeDeposit,
	"withdrawal_request":            JournalBizTypeWithdrawalRequest,
	"withdrawal_complete":           JournalBizTypeWithdrawalComplete,
	"withdrawal_reject":             JournalBizTypeWithdrawalReject,
	"withdrawal_cancel":             JournalBizTypeWithdrawalCancel,
	"transfer":                      JournalBizTypeTransfer,
	"checkout_payment":              JournalBizTypeCheckoutPayment,
	"checkout_payment_hold":         JournalBizTypeCheckoutPaymentHold,
	"checkout_payment_hold_release": JournalBizTypeCheckoutPaymentHoldRelease,
}

// String returns the string representation of the JournalBizType.
func (j JournalBizType) String() string {
	if s, ok := _journalBizTypeToString[j]; ok {
		return s
	}
	return "unknown"
}

// NewJournalBizTypeFromString converts a transport string to JournalBizType.
func NewJournalBizTypeFromString(value string) (JournalBizType, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	bizType, ok := _journalBizTypeByString[normalized]
	if ok {
		return bizType, nil
	}

	return 0, fmt.Errorf("[domain][NewJournalBizTypeFromString][err: unsupported journal biz type %q]", value)
}

// IsSupportedJournalBizType reports whether the journal business type is supported.
func IsSupportedJournalBizType(bizType JournalBizType) bool {
	_, ok := _journalBizTypeToString[bizType]
	return ok
}

// SupportedJournalBizTypeStrings returns transport strings for supported journal business types.
func SupportedJournalBizTypeStrings() []string {
	return slices.Sorted(maps.Keys(_journalBizTypeByString))
}

// JournalEntry represents a business operation that groups postings.
type JournalEntry struct {
	ID                     ID
	ExternalIdempotencyKey *string
	BizType                JournalBizType
	Status                 JournalStatus
	RelatedEntryID         *ID
	ReversalOfID           *ID
	Description            *string
	CreatedAt              time.Time
	CompletedAt            *time.Time
	UpdatedAt              *time.Time
}

// JournalEntryRepository defines operations for persisting journal entries.
type JournalEntryRepository interface {
	Create(context.Context, *JournalEntry) error
	GetByIDForUpdate(context.Context, ID) (JournalEntry, error)
	GetByIdempotencyKey(context.Context, string) (JournalEntry, error)
	UpdateIdempotencyKey(context.Context, ID, string) error
	MarkReversed(context.Context, ID, ID) error

	WithTxFromContext(context.Context) JournalEntryRepository
}
