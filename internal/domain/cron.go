package domain

import "context"

type CronManager interface {
	Start()
	AddFunc(string, string, func(context.Context) error) error
	Stop() context.Context
}
