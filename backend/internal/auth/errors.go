package auth

import "errors"

var (
	ErrInvalidOwner       = errors.New("invalid owner")
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrOwnerExists        = errors.New("owner already exists")
	ErrOwnerNotFound      = errors.New("owner not found")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUnauthenticated    = errors.New("authentication required")
	ErrSessionNotFound    = errors.New("session not found")
)
