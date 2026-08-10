package auth

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

const (
	minimumPasswordBytes = 12
	maximumPasswordBytes = 1024
	maximumEmailBytes    = 254
)

// Owner is the single authentication identity. It is deliberately separate
// from financial accounts and other financial-domain concepts.
type Owner struct {
	id           string
	email        string
	passwordHash string
	createdAt    time.Time
	updatedAt    time.Time
}

func NewOwner(id, email, passwordHash string, now time.Time) (Owner, error) {
	return RestoreOwner(id, email, passwordHash, now, now)
}

// RestoreOwner reconstructs a validated owner from persistence.
func RestoreOwner(id, email, passwordHash string, createdAt, updatedAt time.Time) (Owner, error) {
	normalizedEmail, err := NormalizeEmail(email)
	if err != nil {
		return Owner{}, err
	}
	if strings.TrimSpace(id) == "" || passwordHash == "" {
		return Owner{}, ErrInvalidOwner
	}
	if createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return Owner{}, ErrInvalidOwner
	}
	return Owner{
		id:           id,
		email:        normalizedEmail,
		passwordHash: passwordHash,
		createdAt:    createdAt.UTC(),
		updatedAt:    updatedAt.UTC(),
	}, nil
}

func NormalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || len(normalized) > maximumEmailBytes {
		return "", ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Name != "" || parsed.Address != normalized {
		return "", fmt.Errorf("%w", ErrInvalidEmail)
	}
	return normalized, nil
}

func ValidatePassword(value string) error {
	length := len([]byte(value))
	if length < minimumPasswordBytes || length > maximumPasswordBytes {
		return ErrInvalidPassword
	}
	return nil
}

func (o Owner) ID() string           { return o.id }
func (o Owner) Email() string        { return o.email }
func (o Owner) PasswordHash() string { return o.passwordHash }
func (o Owner) CreatedAt() time.Time { return o.createdAt }
func (o Owner) UpdatedAt() time.Time { return o.updatedAt }
