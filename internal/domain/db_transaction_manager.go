package domain

import "context"

type TransactionManager interface {
	Begin(context.Context) context.Context
	Commit(context.Context) error
	Rollback(context.Context) error
}
