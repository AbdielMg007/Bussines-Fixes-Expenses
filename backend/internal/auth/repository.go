package auth

import (
	"context"
	"time"
)

// Repository is the persistence boundary for the single owner and server-side
// sessions. Atomic owner uniqueness is an adapter responsibility.
type Repository interface {
	CreateOwner(context.Context, Owner) error
	FindOwnerByEmail(context.Context, string) (Owner, error)
	CreateSession(context.Context, Session) error
	FindOwnerBySession(context.Context, SessionDigest, time.Time) (Owner, error)
	DeleteSession(context.Context, SessionDigest) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (bool, error)
}
