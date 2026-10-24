package protected

import (
	"testing"

	"github.com/whagency/core-common/pagination"
)

// TestHistoryFilters_ClampPagination pins the bound on every history list.
//
// These endpoints take page and per-page straight from the query string, and
// the handler is the only place that knows the values are untrusted. Without a
// cap, any authenticated caller turns one cheap request into a full-table scan
// of their own history; without a page bound, (page-1)*limit overflows into a
// negative offset, which GORM drops — silently serving page 1 for a request
// that asked for something far past the end.
//
// All four lists are covered because the clamp is easy to reintroduce one
// handler at a time.
func TestHistoryFilters_ClampPagination(t *testing.T) {
	t.Parallel()

	const absurdPageSize = 100_000_000
	const absurdPage = 1 << 62

	t.Run("transactions", func(t *testing.T) {
		t.Parallel()

		filter, er := transactionListRequestURLParams{Page: 1, PageSize: absurdPageSize}.toTransactionFilter()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, filter.Limit, filter.Offset)

		deep, er := transactionListRequestURLParams{Page: absurdPage, PageSize: 10}.toTransactionFilter()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, deep.Limit, deep.Offset)
	})

	t.Run("deposits", func(t *testing.T) {
		t.Parallel()

		filter, er := depositListRequestQueryParams{Page: 1, PageSize: absurdPageSize}.toDepositFilter()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, filter.Limit, filter.Offset)

		deep, er := depositListRequestQueryParams{Page: absurdPage, PageSize: 10}.toDepositFilter()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, deep.Limit, deep.Offset)
	})

	t.Run("withdrawals", func(t *testing.T) {
		t.Parallel()

		filter, er := withdrawalListRequestQueryParams{Page: 1, PageSize: absurdPageSize}.toWithdrawalFilter()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, filter.Limit, filter.Offset)

		deep, er := withdrawalListRequestQueryParams{Page: absurdPage, PageSize: 10}.toWithdrawalFilter()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, deep.Limit, deep.Offset)
	})

	t.Run("transfers", func(t *testing.T) {
		t.Parallel()

		params := transferListRequestQueryParams{Page: 1, PageSize: absurdPageSize}
		filter, er := params.toDomain()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, filter.Limit, filter.Offset)

		deepParams := transferListRequestQueryParams{Page: absurdPage, PageSize: 10}
		deep, er := deepParams.toDomain()
		if er != nil {
			t.Fatalf("unexpected error: %v", er)
		}
		assertClamped(t, deep.Limit, deep.Offset)
	})
}

func assertClamped(t *testing.T, limit, offset int) {
	t.Helper()

	if limit <= 0 || limit > pagination.MaxPageSize {
		t.Fatalf("limit = %d, want 1..%d — an unbounded per-page reaches the database as LIMIT", limit, pagination.MaxPageSize)
	}
	if offset < 0 {
		t.Fatalf("offset = %d, want >= 0 — GORM drops a negative OFFSET and serves page 1", offset)
	}
}

// TestHistoryFilters_DefaultPagination keeps the clamp from swallowing the
// ordinary case: an unset page and per-page must still mean "first page,
// default size".
func TestHistoryFilters_DefaultPagination(t *testing.T) {
	t.Parallel()

	filter, er := transactionListRequestURLParams{}.toTransactionFilter()
	if er != nil {
		t.Fatalf("unexpected error: %v", er)
	}
	if filter.Limit != pagination.DefaultPageSize {
		t.Fatalf("limit = %d, want the default %d", filter.Limit, pagination.DefaultPageSize)
	}
	if filter.Offset != 0 {
		t.Fatalf("offset = %d, want 0", filter.Offset)
	}
}
