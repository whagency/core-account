package amlbot

import (
	"context"
	"fmt"

	"account-service/config"
	kytProviders "account-service/config/kyt-providers"
	"account-service/internal/domain"

	commonHTTP "github.com/whagency/core-common/http"
	"github.com/whagency/core-common/tools"
)

type checkAddressRequest struct {
	Hash     string `json:"hash"`
	Asset    string `json:"asset"`
	Token    string `json:"token"`
	AccessId string `json:"accessId"`
	Locale   string `json:"locale"`
	Flow     string `json:"flow"`
}

const (
	_riskScoreLevel = 0.5

	_checkAddressLocale = "en_US"
	_checkAddressFlow   = "fast_limited"
)

type checkAddressResponse struct {
	Result bool `json:"result"`
	Data   struct {
		Status           string  `json:"status"`
		Flow             string  `json:"flow"`
		Asset            string  `json:"asset"`
		Network          string  `json:"network"`
		Address          string  `json:"address"`
		HasBlackListFlag bool    `json:"hasBlackListFlag"`
		PdfReport        string  `json:"pdfReport"`
		Riskscore        float64 `json:"riskscore"`
	} `json:"data"`
}

// decode converts checkAddressResponse to domain.KYTCheckAddressResult.
func (r checkAddressResponse) decode() domain.KYTCheckAddressResult {
	isClean := true
	status := domain.KYTStatusError
	if r.Data.HasBlackListFlag {
		isClean = false
	}
	if r.Data.Riskscore > _riskScoreLevel {
		isClean = false
	}
	if r.Result {
		status = statusToDomain(r.Data.Status)
	}
	return domain.KYTCheckAddressResult{
		Status:  status,
		IsClean: isClean,
	}
}

// CheckAddress retrieves the crypto address verification result from the aml bot service.
func (s Service) CheckAddress(ctx context.Context, address string, network string) (domain.KYTCheckAddressResult, error) {
	var (
		response    checkAddressResponse
		errResponse amlBotErrResponse
	)

	// Bound this call without touching the shared client — see
	// commonHTTP.WithCallTimeout.
	ctx, cancel := commonHTTP.WithCallTimeout(ctx, config.Commons().TimeoutExtCall)
	defer cancel()

	res, err := s.client.
		R().
		SetContext(ctx).
		SetHeaders(map[string]string{
			tools.ContentTypeHeader: tools.FormUrlencodedContentType,
		}).
		SetBody(&checkAddressRequest{
			Hash:     address,
			Asset:    network,
			Token:    fmt.Sprintf("hash:%s:%s", kytProviders.AMLBot().AmlBotAccessKey, kytProviders.AMLBot().AmlBotAccessID),
			AccessId: kytProviders.AMLBot().AmlBotAccessID,
			Locale:   _checkAddressLocale,
			Flow:     _checkAddressFlow,
		}).
		SetSuccessResult(&response).
		SetErrorResult(&errResponse).
		Post(kytProviders.AMLBot().AmlBotURL)
	if err != nil {
		return domain.KYTCheckAddressResult{
			Status: domain.KYTStatusError,
		}, fmt.Errorf("[kyt][amlbot][Service:CheckAddress][err: %w]", err)
	}
	if !res.IsSuccessState() {
		return domain.KYTCheckAddressResult{
			Status: domain.KYTStatusError,
		}, fmt.Errorf("[kyt][amlbot][Service:CheckAddress][httpCode: %d][description: %s]", res.StatusCode, errResponse.Description)
	}

	return response.decode(), nil
}

// statusToDomain maps the verification status to domain.KYTStatus.
func statusToDomain(status string) domain.KYTStatus {
	switch status {
	case "success":
		return domain.KYTStatusSuccess
	case "pending":
		return domain.KYTStatusPending
	case "failed", "error":
		return domain.KYTStatusError
	default:
		return domain.KYTStatusError
	}
}
