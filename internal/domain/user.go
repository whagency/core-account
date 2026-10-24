package domain

import (
	"context"

	"github.com/whagency/core-common/services"
	"github.com/whagency/core-common/services/user"
)

// UserService defines methods for user session validation and management.
type UserService interface {
	ValidateSession(context.Context, string, bool) (user.Session, error)
	BlockUser(context.Context, uint64) error
}

// UserGRPCService defines methods for gRPC-based user operations.
type UserGRPCService interface {
	services.GRPCClientBase

	ValidateSession(context.Context, string, bool) (user.Session, error)
	GetUserByNumber(context.Context, uint64) (user.User, error)
}
