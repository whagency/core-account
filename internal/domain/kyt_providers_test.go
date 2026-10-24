package domain

import "testing"

func TestKYTVerdictPermitsCredit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		verdict KYTVerdict
		want    bool
	}{
		{name: "clean check", verdict: KYTVerdictClean, want: true},
		{name: "check does not apply", verdict: KYTVerdictNotChecked, want: true},
		{name: "flagged", verdict: KYTVerdictFlagged, want: false},
		{name: "inconclusive", verdict: KYTVerdictInconclusive, want: false},
		{
			// The reason this is an allow-list rather than a deny-list: a
			// verdict nobody set must not read as a clean check.
			name:    "unset verdict",
			verdict: KYTVerdict(0),
			want:    false,
		},
		{
			name:    "unknown future verdict",
			verdict: KYTVerdict(99),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.verdict.PermitsCredit(); got != tt.want {
				t.Fatalf("%s.PermitsCredit() = %v, want %v", tt.verdict, got, tt.want)
			}
		})
	}
}

func TestKYTCheckAddressResultVerdict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result KYTCheckAddressResult
		want   KYTVerdict
	}{
		{
			name:   "checked and cleared",
			result: KYTCheckAddressResult{Status: KYTStatusSuccess, IsClean: true},
			want:   KYTVerdictClean,
		},
		{
			name:   "checked and flagged",
			result: KYTCheckAddressResult{Status: KYTStatusSuccess, IsClean: false},
			want:   KYTVerdictFlagged,
		},
		{
			// AMLBot answers HTTP 200 with result:false on its own internal
			// errors, which decodes to this status. IsClean is left true to pin
			// the point: without a successful check the flag carries no
			// information, and reading it as "clean" is what failed open.
			name:   "provider error is not a clean bill of health",
			result: KYTCheckAddressResult{Status: KYTStatusError, IsClean: true},
			want:   KYTVerdictInconclusive,
		},
		{
			name:   "check still running is not a clean bill of health",
			result: KYTCheckAddressResult{Status: KYTStatusPending, IsClean: true},
			want:   KYTVerdictInconclusive,
		},
		{
			// The zero value reaches Verdict whenever a caller forgets to fill
			// the result in. It must not read as clean.
			name:   "zero value is inconclusive",
			result: KYTCheckAddressResult{},
			want:   KYTVerdictInconclusive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.result.Verdict(); got != tt.want {
				t.Fatalf("Verdict() = %d, want %d", got, tt.want)
			}
		})
	}
}
