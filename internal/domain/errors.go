package domain

import "errors"

var (
	ErrRecordNotFound      = errors.New("record not found")
	ErrRecordAlreadyExists = errors.New("record already exists")
	ErrMessageBadFormat    = errors.New("bad message format")
)
