package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

type ServiceOptions struct {
	SessionDuration time.Duration
	Clock           func() time.Time
	OwnerID         func() (string, error)
	SessionToken    func() (SessionCredential, error)
}

type Service struct {
	repository      Repository
	passwords       PasswordHasher
	sessionDuration time.Duration
	clock           func() time.Time
	ownerID         func() (string, error)
	sessionToken    func() (SessionCredential, error)
	dummyHash       string
}

type LoginResult struct {
	Owner     Owner
	Token     string
	ExpiresAt time.Time
}

func NewService(repository Repository, passwords PasswordHasher, options ServiceOptions) (*Service, error) {
	if repository == nil || passwords == nil || options.SessionDuration <= 0 {
		return nil, errors.New("invalid authentication service configuration")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.OwnerID == nil {
		options.OwnerID = generateOwnerID
	}
	if options.SessionToken == nil {
		options.SessionToken = GenerateSessionCredential
	}
	dummyHash, err := passwords.Hash("runway-invalid-credentials-dummy-password")
	if err != nil {
		return nil, errors.New("initialize credential verification")
	}
	return &Service{
		repository:      repository,
		passwords:       passwords,
		sessionDuration: options.SessionDuration,
		clock:           options.Clock,
		ownerID:         options.OwnerID,
		sessionToken:    options.SessionToken,
		dummyHash:       dummyHash,
	}, nil
}

func (s *Service) Bootstrap(ctx context.Context, email, password string) (Owner, error) {
	normalizedEmail, err := NormalizeEmail(email)
	if err != nil {
		return Owner{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return Owner{}, err
	}
	passwordHash, err := s.passwords.Hash(password)
	if err != nil {
		return Owner{}, errors.New("hash owner password")
	}
	id, err := s.ownerID()
	if err != nil {
		return Owner{}, errors.New("generate owner identifier")
	}
	now := s.clock().UTC()
	owner, err := NewOwner(id, normalizedEmail, passwordHash, now)
	if err != nil {
		return Owner{}, err
	}
	if err := s.repository.CreateOwner(ctx, owner); err != nil {
		return Owner{}, err
	}
	return owner, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	normalizedEmail, err := NormalizeEmail(email)
	if err != nil {
		s.consumeDummyVerification(password)
		return LoginResult{}, ErrInvalidCredentials
	}
	owner, err := s.repository.FindOwnerByEmail(ctx, normalizedEmail)
	if errors.Is(err, ErrOwnerNotFound) {
		s.consumeDummyVerification(password)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("find owner: %w", err)
	}
	valid, err := s.passwords.Verify(password, owner.PasswordHash())
	if err != nil {
		return LoginResult{}, errors.New("verify owner credentials")
	}
	if !valid {
		return LoginResult{}, ErrInvalidCredentials
	}

	credential, err := s.sessionToken()
	if err != nil || credential.Token == "" || credential.Digest == (SessionDigest{}) {
		return LoginResult{}, errors.New("generate session")
	}
	now := s.clock().UTC()
	expiresAt := now.Add(s.sessionDuration)
	session, err := NewSession(credential.Digest, owner.ID(), now, expiresAt)
	if err != nil {
		return LoginResult{}, errors.New("create session")
	}
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return LoginResult{}, fmt.Errorf("persist session: %w", err)
	}
	return LoginResult{Owner: owner, Token: credential.Token, ExpiresAt: expiresAt}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Owner, error) {
	digest, err := DigestSessionToken(token)
	if err != nil {
		return Owner{}, ErrUnauthenticated
	}
	owner, err := s.repository.FindOwnerBySession(ctx, digest, s.clock().UTC())
	if errors.Is(err, ErrSessionNotFound) {
		return Owner{}, ErrUnauthenticated
	}
	if err != nil {
		return Owner{}, fmt.Errorf("find session: %w", err)
	}
	return owner, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	digest, err := DigestSessionToken(token)
	if err != nil {
		return nil
	}
	if err := s.repository.DeleteSession(ctx, digest); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Service) consumeDummyVerification(password string) {
	_, _ = s.passwords.Verify(password, s.dummyHash)
}

func generateOwnerID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
