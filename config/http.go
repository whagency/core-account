package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

const (
	_maxConnsPerHost     = 100
	_maxIdleConns        = 100
	_maxIdleConnsPerHost = 100
	_maxRetries          = 2
)

var httpSettings HTTPSettings

type HTTPSettings struct {
	Port                 string          `envconfig:"PORT" default:"8000"`
	ServerTimeout        time.Duration   `envconfig:"SERVER_TIMEOUT" default:"5s"`
	ReadTimeout          time.Duration   `envconfig:"SERVER_READ_TIMEOUT" default:"5s"`
	ReadHeaderTimeout    time.Duration   `envconfig:"SERVER_READ_HEADER_TIMEOUT" default:"5s"`
	WriteTimeout         time.Duration   `envconfig:"SERVER_WRITE_TIMEOUT" default:"5s"`
	RetryBackoffInterval []time.Duration `default:"1s,5s"`

	MaxConnsPerHost     int
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxRetries          int
}

// HTTP returns the HTTP settings
func HTTP() HTTPSettings {
	return httpSettings
}

// loadHTTPConfig loads the HTTP settings from the environment variables
func loadHTTPConfig() error {
	if err := envconfig.Process("", &httpSettings); err != nil {
		return fmt.Errorf("[loadHTTPConfig][err: %w]", err)
	}

	httpSettings.MaxConnsPerHost = _maxConnsPerHost
	httpSettings.MaxIdleConns = _maxIdleConns
	httpSettings.MaxIdleConnsPerHost = _maxIdleConnsPerHost
	httpSettings.MaxRetries = _maxRetries

	return nil
}
