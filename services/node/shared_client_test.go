package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"account-service/internal/domain"
	"account-service/test/bootstrap"

	"github.com/imroc/req/v3"
)

// TestSharedClient_ConcurrentCallsAreRaceFree is the regression for the shared
// req.Client race. Run it with -race.
//
// The composition root hands one client to node, KYT, currency and user alike.
// Every call used to start with client.SetTimeout, which writes
// httpClient.Timeout — a field net/http reads on each in-flight request. Two
// concurrent outbound calls were therefore a write against a read, reported by
// the detector as SetTimeout versus net/http.(*Client).do. Per-call bounds now
// live on the request context, so the client is never mutated after setup.
//
// Without -race this passes either way, which is the point: nothing here fails
// on wall-clock behaviour, only on the detector seeing unsynchronised access.
func TestSharedClient_ConcurrentCallsAreRaceFree(t *testing.T) {
	bootstrap.InitLogger(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	shared := req.C()
	svc := NewNodeService(shared)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Errors are irrelevant: the assertion is the detector, and the call
			// has to reach the transport for net/http to read the client.
			_, _ = svc.GetFees(context.Background(), domain.ChainNetworkTRX)
		}()
	}
	wg.Wait()
}
