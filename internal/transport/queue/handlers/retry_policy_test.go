package handlers

import (
	"errors"
	"testing"

	"account-service/internal/domain"

	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/queue"
)

func TestDiscardOnKnownAccountQueueError(t *testing.T) {
	t.Run("configured error code is discarded", func(t *testing.T) {
		err := appErrors.New(appErrors.InvalidRequestError)

		got := discardOnKnownAccountQueueError(err, appErrors.InvalidRequestError)
		if !errors.Is(got, queue.NonRetryableErr) {
			t.Fatalf("expected non-retryable error, got %v", got)
		}
	})

	t.Run("record not found in cause chain is discarded", func(t *testing.T) {
		err := appErrors.New(
			appErrors.DatabaseError,
			appErrors.WithCause(domain.ErrRecordNotFound),
		)

		got := discardOnKnownAccountQueueError(err)
		if !errors.Is(got, queue.NonRetryableErr) {
			t.Fatalf("expected non-retryable error, got %v", got)
		}
	})

	t.Run("other errors stay retryable", func(t *testing.T) {
		err := appErrors.New(appErrors.DatabaseError)

		got := discardOnKnownAccountQueueError(err, appErrors.InvalidRequestError)
		if errors.Is(got, queue.NonRetryableErr) {
			t.Fatalf("expected retryable error, got %v", got)
		}
	})
}
