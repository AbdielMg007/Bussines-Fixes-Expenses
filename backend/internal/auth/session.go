package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"time"
)

const sessionTokenBytes = 32

type SessionDigest [sha256.Size]byte

type Session struct {
	digest    SessionDigest
	ownerID   string
	createdAt time.Time
	expiresAt time.Time
}

func NewSession(digest SessionDigest, ownerID string, createdAt, expiresAt time.Time) (Session, error) {
	if digest == (SessionDigest{}) || ownerID == "" || createdAt.IsZero() || !expiresAt.After(createdAt) {
		return Session{}, ErrUnauthenticated
	}
	return Session{
		digest:    digest,
		ownerID:   ownerID,
		createdAt: createdAt.UTC(),
		expiresAt: expiresAt.UTC(),
	}, nil
}

func (s Session) Digest() SessionDigest { return s.digest }
func (s Session) OwnerID() string       { return s.ownerID }
func (s Session) CreatedAt() time.Time  { return s.createdAt }
func (s Session) ExpiresAt() time.Time  { return s.expiresAt }

type SessionCredential struct {
	Token  string
	Digest SessionDigest
}

func GenerateSessionCredential() (SessionCredential, error) {
	buffer := make([]byte, sessionTokenBytes)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return SessionCredential{}, errors.New("generate session credential")
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	return SessionCredential{Token: token, Digest: sha256.Sum256([]byte(token))}, nil
}

func DigestSessionToken(token string) (SessionDigest, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != sessionTokenBytes {
		return SessionDigest{}, ErrUnauthenticated
	}
	return sha256.Sum256([]byte(token)), nil
}
