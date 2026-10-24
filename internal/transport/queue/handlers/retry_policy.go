package handlers

import (
	stdErrors "errors"

	"account-service/internal/domain"

	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/queue"
)

// discardOnKnownAccountQueueError checks if the error is a known non-retryable error and marks it as such for the queue.
func discardOnKnownAccountQueueError(err error, permanentCodes ...int) error {
	if err == nil {
		return nil
	}

	if er, ok := appErrors.AsErrorResponse(err); ok {
		for _, code := range permanentCodes {
			if er.Code == code {
				return queue.MarkNonRetryable(err)
			}
		}
	}

	if stdErrors.Is(err, domain.ErrRecordNotFound) {
		return queue.MarkNonRetryable(err)
	}

	return err
}
