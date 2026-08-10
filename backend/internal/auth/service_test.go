package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"
)

const testPassword = "a sufficiently long password"

func TestBootstrapCreatesOnlyOneHashedOwner(t *testing.T) {
	repository := newMemoryRepository()
	service := newTestService(t, repository, fixedClock(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)))

	owner, err := service.Bootstrap(context.Background(), " Owner@Example.COM ", testPassword)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if owner.ID() != "owner-id" || owner.Email() != "owner@example.com" {
		t.Fatalf("owner identity = %q %q", owner.ID(), owner.Email())
	}
	if owner.PasswordHash() == testPassword || owner.PasswordHash() != "hashed:"+testPassword {
		t.Fatal("owner password was not stored as the hasher output")
	}
	if _, err := service.Bootstrap(context.Background(), "second@example.com", testPassword); !errors.Is(err, ErrOwnerExists) {
		t.Fatalf("second Bootstrap() error = %v, want %v", err, ErrOwnerExists)
	}
}

func TestBootstrapRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{name: "invalid email", email: "not-an-email", password: testPassword, wantErr: ErrInvalidEmail},
		{name: "short password", email: "owner@example.com", password: "too short", wantErr: ErrInvalidPassword},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryRepository()
			service := newTestService(t, repository, fixedClock(time.Now()))
			if _, err := service.Bootstrap(context.Background(), test.email, test.password); !errors.Is(err, test.wantErr) {
				t.Fatalf("Bootstrap() error = %v, want %v", err, test.wantErr)
			}
			if repository.owner != nil {
				t.Fatal("invalid bootstrap persisted an owner")
			}
		})
	}
}

func TestLoginSessionAuthenticationExpirationAndLogout(t *testing.T) {
	repository := newMemoryRepository()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	service := newTestService(t, repository, clock)
	owner, err := service.Bootstrap(context.Background(), "owner@example.com", testPassword)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	for _, attempt := range []struct {
		name     string
		email    string
		password string
	}{
		{name: "wrong password", email: owner.Email(), password: "wrong password value"},
		{name: "unknown email", email: "unknown@example.com", password: testPassword},
	} {
		t.Run(attempt.name, func(t *testing.T) {
			if _, err := service.Login(context.Background(), attempt.email, attempt.password); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want %v", err, ErrInvalidCredentials)
			}
		})
	}

	result, err := service.Login(context.Background(), owner.Email(), testPassword)
	if err != nil {
		t.Fatalf("Login(valid) error = %v", err)
	}
	if result.Token == "" || result.ExpiresAt != now.Add(time.Hour) || len(repository.sessions) != 1 {
		t.Fatalf("login result = %+v, sessions = %d", result, len(repository.sessions))
	}
	authenticated, err := service.Authenticate(context.Background(), result.Token)
	if err != nil || authenticated.ID() != owner.ID() {
		t.Fatalf("Authenticate(valid) owner = %+v, error = %v", authenticated, err)
	}
	if _, err := service.Authenticate(context.Background(), "invalid"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate(invalid) error = %v, want %v", err, ErrUnauthenticated)
	}

	now = result.ExpiresAt
	if _, err := service.Authenticate(context.Background(), result.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate(expired) error = %v, want %v", err, ErrUnauthenticated)
	}
	now = result.ExpiresAt.Add(-time.Second)
	if err := service.Logout(context.Background(), result.Token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), result.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate(after logout) error = %v, want %v", err, ErrUnauthenticated)
	}
	if err := service.Logout(context.Background(), "already-invalid"); err != nil {
		t.Fatalf("Logout(invalid) error = %v", err)
	}
}

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) { return "hashed:" + password, nil }
func (fakeHasher) Verify(password, encoded string) (bool, error) {
	return encoded == "hashed:"+password, nil
}

type memoryRepository struct {
	mu       sync.Mutex
	owner    *Owner
	sessions map[SessionDigest]Session
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{sessions: make(map[SessionDigest]Session)}
}

func (r *memoryRepository) CreateOwner(_ context.Context, owner Owner) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner != nil {
		return ErrOwnerExists
	}
	r.owner = &owner
	return nil
}

func (r *memoryRepository) FindOwnerByEmail(_ context.Context, email string) (Owner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner == nil || r.owner.Email() != email {
		return Owner{}, ErrOwnerNotFound
	}
	return *r.owner, nil
}

func (r *memoryRepository) CreateSession(_ context.Context, session Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.Digest()] = session
	return nil
}

func (r *memoryRepository) FindOwnerBySession(_ context.Context, digest SessionDigest, now time.Time) (Owner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[digest]
	if !ok || !session.ExpiresAt().After(now) || r.owner == nil {
		return Owner{}, ErrSessionNotFound
	}
	return *r.owner, nil
}

func (r *memoryRepository) DeleteSession(_ context.Context, digest SessionDigest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, digest)
	return nil
}

func newTestService(t *testing.T, repository Repository, clock func() time.Time) *Service {
	t.Helper()
	token := base64.RawURLEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	credential := SessionCredential{Token: token, Digest: sha256.Sum256([]byte(token))}
	service, err := NewService(repository, fakeHasher{}, ServiceOptions{
		SessionDuration: time.Hour,
		Clock:           clock,
		OwnerID:         func() (string, error) { return "owner-id", nil },
		SessionToken:    func() (SessionCredential, error) { return credential, nil },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func fixedClock(value time.Time) func() time.Time {
	return func() time.Time { return value }
}
