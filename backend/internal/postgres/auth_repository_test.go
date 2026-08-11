package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"runway/backend/internal/auth"
	"runway/backend/migrations"
)

func TestMigrationsApplyCleanlyAndIdempotently(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	ctx := context.Background()
	if err := ApplyMigrations(ctx, pool, migrations.Files); err != nil {
		t.Fatalf("second ApplyMigrations() error = %v", err)
	}

	for _, table := range []string{
		"schema_migrations", "owners", "auth_sessions", "accounts", "linked_transfers",
		"financial_transactions", "balance_snapshots", "financial_mutations", "obligations",
		"scheduled_cash_flows", "receivables", "receivable_collections", "projection_policies",
		"projection_policy_accounts",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}
	var migrationCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 7 {
		t.Fatalf("migration count = %d, want 7", migrationCount)
	}
}

func TestAuthRepositoryOwnerUniquenessAndSessionPersistence(t *testing.T) {
	pool := newIsolatedTestDatabase(t)
	repository := NewAuthRepository(pool)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	owner, err := auth.NewOwner("owner-one", "owner@example.com", "encoded-password-hash", now)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	if err := repository.CreateOwner(ctx, owner); err != nil {
		t.Fatalf("CreateOwner() error = %v", err)
	}
	second, err := auth.NewOwner("owner-two", "second@example.com", "different-encoded-hash", now)
	if err != nil {
		t.Fatalf("NewOwner(second) error = %v", err)
	}
	if err := repository.CreateOwner(ctx, second); !errors.Is(err, auth.ErrOwnerExists) {
		t.Fatalf("second CreateOwner() error = %v, want %v", err, auth.ErrOwnerExists)
	}
	loaded, err := repository.FindOwnerByEmail(ctx, owner.Email())
	if err != nil || loaded.ID() != owner.ID() || loaded.PasswordHash() != owner.PasswordHash() {
		t.Fatalf("FindOwnerByEmail() owner = %+v, error = %v", loaded, err)
	}

	credential, err := auth.GenerateSessionCredential()
	if err != nil {
		t.Fatalf("GenerateSessionCredential() error = %v", err)
	}
	session, err := auth.NewSession(credential.Digest, owner.ID(), now, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := repository.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	authenticated, err := repository.FindOwnerBySession(ctx, credential.Digest, now)
	if err != nil || authenticated.ID() != owner.ID() {
		t.Fatalf("FindOwnerBySession() owner = %+v, error = %v", authenticated, err)
	}
	var rawTokenRows int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM auth_sessions WHERE token_digest = $1",
		[]byte(credential.Token),
	).Scan(&rawTokenRows); err != nil {
		t.Fatalf("check raw token persistence: %v", err)
	}
	if rawTokenRows != 0 {
		t.Fatal("raw bearer token was persisted")
	}
	if _, err := repository.FindOwnerBySession(ctx, credential.Digest, session.ExpiresAt()); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("expired FindOwnerBySession() error = %v, want %v", err, auth.ErrSessionNotFound)
	}
	if err := repository.DeleteSession(ctx, credential.Digest); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := repository.FindOwnerBySession(ctx, credential.Digest, now); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("deleted FindOwnerBySession() error = %v, want %v", err, auth.ErrSessionNotFound)
	}
}

func newIsolatedTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	baseConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL is invalid")
	}
	basePool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal("open test database")
	}
	if err := basePool.Ping(ctx); err != nil {
		basePool.Close()
		t.Fatal("connect to test database")
	}

	randomName := make([]byte, 8)
	if _, err := rand.Read(randomName); err != nil {
		basePool.Close()
		t.Fatal("generate test schema name")
	}
	schema := "runway_auth_test_" + hex.EncodeToString(randomName)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := basePool.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		basePool.Close()
		t.Fatalf("create test schema: %v", err)
	}

	testConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		basePool.Close()
		t.Fatal("TEST_DATABASE_URL is invalid")
	}
	testConfig.ConnConfig.RuntimeParams["search_path"] = schema
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		basePool.Close()
		t.Fatal("open isolated test database")
	}
	t.Cleanup(func() {
		testPool.Close()
		if _, err := basePool.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		basePool.Close()
	})
	if err := ApplyMigrations(ctx, testPool, migrations.Files); err != nil {
		t.Fatalf("ApplyMigrations() in empty schema error = %v", err)
	}
	return testPool
}
