package domain

import "context"

type KYTStatus uint8

const (
	KYTStatusSuccess KYTStatus = iota + 1
	KYTStatusPending
	KYTStatusError
)

// NewKYTStatus creates a new KYTStatus from a uint8 value.
func NewKYTStatus(val uint8) KYTStatus {
	return KYTStatus(val)
}

type KYTCheckAddressResult struct {
	Status  KYTStatus
	IsClean bool
}

// IsSuccess returns whether the address verification was successful.
func (r KYTCheckAddressResult) IsSuccess() bool {
	return r.Status == KYTStatusSuccess
}

// IsPending returns whether the address verification is pending.
func (r KYTCheckAddressResult) IsPending() bool {
	return r.Status == KYTStatusPending
}

// IsError returns whether the address verification finished with errors.
func (r KYTCheckAddressResult) IsError() bool {
	return r.Status == KYTStatusError
}

// KYTVerdict is the decision callers act on after an address check.
//
// The provider reports three outcomes and only one of them is a clean bill of
// health. Collapsing "could not decide" into "clean" is what made the check
// fail open: AMLBot answers HTTP 200 with result:false on its own internal
// errors, and reports a still-running check as pending. Both decode to a
// non-success status, so a caller that only acted on "success and not clean"
// let flagged and undecided addresses through alike.
//
// The zero value is deliberately not a verdict. Every caller must state which
// of these applies, including "no check runs here", so that a field nobody
// filled in fails closed instead of passing for a clean one.
type KYTVerdict uint8

const (
	// KYTVerdictClean means the provider checked the address and cleared it.
	KYTVerdictClean KYTVerdict = iota + 1
	// KYTVerdictFlagged means the provider checked the address and flagged it.
	KYTVerdictFlagged
	// KYTVerdictInconclusive means the provider returned no usable verdict: the
	// check is still running, or the provider failed on its own side.
	KYTVerdictInconclusive
	// KYTVerdictNotChecked means no address check applies to this operation.
	// Outbound transactions are screened once, before the funds are sent, not
	// again while their on-chain status is tracked.
	KYTVerdictNotChecked
)

var _kytVerdictToString = map[KYTVerdict]string{
	KYTVerdictClean:        "clean",
	KYTVerdictFlagged:      "flagged",
	KYTVerdictInconclusive: "inconclusive",
	KYTVerdictNotChecked:   "not_checked",
}

// String returns the string representation of the KYTVerdict.
func (v KYTVerdict) String() string {
	if s, ok := _kytVerdictToString[v]; ok {
		return s
	}
	return "unknown"
}

// PermitsCredit reports whether the verdict accounts for the address well
// enough to let value be credited.
//
// Only two verdicts do: a clean check, and an operation the check does not
// cover. Everything else answers false — an undecided provider reply, and the
// zero value, which means nobody classified the address at all. Phrasing it as
// an allow-list is the point: a caller that forgets to set a verdict holds
// rather than pays out.
func (v KYTVerdict) PermitsCredit() bool {
	return v == KYTVerdictClean || v == KYTVerdictNotChecked
}

// Verdict reduces the provider result to the decision callers act on.
//
// It only interprets a result the provider actually returned. A transport
// failure never reaches here: "we got no answer" and "the answer was
// undecided" are handled differently, the first by the call site's error path.
func (r KYTCheckAddressResult) Verdict() KYTVerdict {
	switch {
	case !r.IsSuccess():
		return KYTVerdictInconclusive
	case !r.IsClean:
		return KYTVerdictFlagged
	default:
		return KYTVerdictClean
	}
}

// KYTService defines the interface for managing KYT service in the data layer.
type KYTService interface {
	CheckAddress(context.Context, string, string) (KYTCheckAddressResult, error)
}
