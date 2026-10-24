package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/whagency/core-common/outbox"
	outboxmetrics "github.com/whagency/core-common/outbox/metrics"
	ginprometheus "github.com/willthrom/go-gin-prometheus"
)

const _serviceName = "account_service_"

var defaultBuckets = []float64{0.05, 0.15, 0.3, 0.6, 1, 2, 5}

var (
	dbRequestError = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: _serviceName + "db_error_count",
			Help: "Number of DB Errors",
		},
	)

	dbRequestTime = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:        _serviceName + "db_request_time",
			Help:        "DB requests time in seconds",
			ConstLabels: map[string]string{},
			Buckets:     defaultBuckets,
		},
	)
)

// RegisterMetrics registers all account-service metrics with Prometheus.
func RegisterMetrics() {
	prometheus.MustRegister(
		dbRequestError,
		dbRequestTime,
	)

	var err error
	outboxMetrics, err = outboxmetrics.New(_serviceName, prometheus.DefaultRegisterer)
	if err != nil {
		// Registration only fails on a duplicate or malformed collector, which is
		// a programming error rather than a runtime condition.
		panic(fmt.Sprintf("[tools][RegisterMetrics][err: %s]", err))
	}
}

// outboxMetrics is the shared outbox instrumentation, built by RegisterMetrics.
// Kept in the common package so every service publishes the same metric names —
// dashboards and alert rules are written once against those names.
var outboxMetrics *outboxmetrics.Metrics

// OutboxHooks returns WorkerHooks wired to the outbox Prometheus metrics.
// Call after RegisterMetrics.
func OutboxHooks() outbox.WorkerHooks {
	if outboxMetrics == nil {
		return outbox.WorkerHooks{}
	}
	return outboxMetrics.Hooks()
}

// SetOutboxLag publishes the per-event_type outbox lag sample.
func SetOutboxLag(ages map[string]time.Duration) {
	if outboxMetrics == nil {
		return
	}
	outboxMetrics.SetLag(ages)
}

// LinkToPrometheusToGin Link Prometheus to Gin
func LinkToPrometheusToGin(ge *gin.Engine) {
	p := ginprometheus.NewPrometheusZap(_serviceName+"gin", nil)
	p.ReqCntURLLabelMappingFn = func(c *gin.Context) string {
		url := c.Request.URL.Path
		for _, param := range c.Params {
			url = strings.Replace(url, param.Value, ":"+param.Key, 1)
		}
		return url
	}
	p.Use(ge)
}

// TrackMetricDBErrorAdd Increment DB error counter
func TrackMetricDBErrorAdd() {
	dbRequestError.Inc()
}

// EndDBRequestTimer Record timing for DB request
func EndDBRequestTimer(startTime time.Time) {
	dbRequestTime.Observe(time.Since(startTime).Seconds())
}
