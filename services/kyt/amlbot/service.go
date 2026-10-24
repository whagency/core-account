package amlbot

import "github.com/imroc/req/v3"

type Service struct {
	client *req.Client
}

// NewAmlBotService creates a new instance of Service.
func NewAmlBotService(client *req.Client) *Service {
	return &Service{client: client}
}

type amlBotErrResponse struct {
	Result      bool   `json:"result"`
	Description string `json:"description"`
}
