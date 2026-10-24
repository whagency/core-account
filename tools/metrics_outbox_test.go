package tools

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	outboxmetrics "github.com/whagency/core-common/outbox/metrics"
)

// Moving to the shared metrics package must not rename anything: these names are
// already scraped, and a rename breaks the history behind every panel and alert.
func TestOutboxMetricNamesUnchanged(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := outboxmetrics.New(_serviceName, reg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	// A *Vec with no series does not appear in Gather, so record one of each.
	h := m.Hooks()
	h.OnDelivered("probe")
	h.OnFailed("probe")
	h.OnDead("probe")
	h.OnLeaseLost("probe")
	h.OnClaimError(nil)
	h.OnHandlerDuration("probe", time.Millisecond)
	m.SetLag(map[string]time.Duration{"probe": time.Second})

	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range families {
		got[f.GetName()] = true
	}

	for _, want := range []string{
		"account_service_outbox_delivered_total",
		"account_service_outbox_failed_total",
		"account_service_outbox_dead_total",
		"account_service_outbox_lease_lost_total",
		"account_service_outbox_claim_errors_total",
		"account_service_outbox_handler_duration_seconds",
		"account_service_outbox_oldest_pending_age_seconds",
	} {
		if !got[want] {
			t.Errorf("metric %q is missing — the shared package renamed it", want)
		}
	}
}
