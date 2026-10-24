package app

import (
	"net/http"

	"account-service/config"

	"github.com/imroc/req/v3"
	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/logger"
)

// setupHTTPClient sets up the http client.
func (app *Application) setupHTTPClient() error {
	client := req.C()
	if config.IsDebugLogLevel() {
		client = req.C().EnableDumpAll()
	}

	client.SetLogger(logger.Log())
	client.SetTimeout(config.HTTP().ReadTimeout)
	client.MaxIdleConnsPerHost = config.HTTP().MaxIdleConnsPerHost
	client.MaxConnsPerHost = config.HTTP().MaxConnsPerHost
	client.MaxIdleConns = config.HTTP().MaxIdleConns

	// Enable retry and set the maximum retry count.
	client.SetCommonRetryCount(config.HTTP().MaxRetries)

	// Set the retry sleep interval with a commonly used algorithm
	client.SetCommonRetryBackoffInterval(config.HTTP().RetryBackoffInterval[0], config.HTTP().RetryBackoffInterval[1])

	// Add a retry condition which determines whether the request should retry.
	client.AddCommonRetryCondition(func(resp *req.Response, err error) bool {
		if err != nil {
			return true
		}
		if resp == nil {
			return false
		}

		switch resp.GetStatusCode() {
		case http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
			http.StatusRequestTimeout,
			http.StatusTooManyRequests:
			return true
		default:
			return false
		}
	})

	commonHTTP.WrapReqClient(client) // Wrap the req client with OpenTelemetry instrumentation.
	app.httpClient = client

	logger.Log().Infof("[Application:setupHTTPClient][msg: HTTP client setup complete][maxIdleConns: %d][maxConnsPerHost: %d][maxIdleConnsPerHost: %d]",
		config.HTTP().MaxIdleConns,
		config.HTTP().MaxConnsPerHost,
		config.HTTP().MaxIdleConnsPerHost,
	)

	return nil
}
