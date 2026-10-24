package config

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/whagency/core-common/tracer"
)

var tracerConfig tracer.Config

// loadTracerConfig loads the tracer configuration from the environment variables
func loadTracerConfig() error {
	// Set the environment for logger configuration
	tracerConfig.AppName = Commons().AppName
	tracerConfig.Environment = Commons().Environment
	tracerConfig.Version = Commons().Version
	if err := envconfig.Process("", &tracerConfig); err != nil {
		return fmt.Errorf("[loadTracerConfig][err: %w]", err)
	}

	return nil
}

// TracerConfig returns the tracer configuration
func TracerConfig() tracer.Config {
	return tracerConfig
}
