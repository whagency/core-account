package kytproviders

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

var amlBotSettings AmlBotSettings

type AmlBotSettings struct {
	AmlBotURL       string `envconfig:"AML_BOT_KYT_URL" required:"true"`
	AmlBotAccessID  string `envconfig:"AML_BOT_KYT_ACCESS_ID" required:"true"`
	AmlBotAccessKey string `envconfig:"AML_BOT_KYT_ACCESS_KEY" required:"true"`
}

// AMLBot returns the AML Bot settings
func AMLBot() AmlBotSettings {
	return amlBotSettings
}

// LoadAMLBotConfig loads the AML Bot configuration
func LoadAMLBotConfig() error {
	if err := envconfig.Process("", &amlBotSettings); err != nil {
		return fmt.Errorf("[kyt-providers][LoadAMLBotConfig][err: %w]", err)
	}
	return nil
}
