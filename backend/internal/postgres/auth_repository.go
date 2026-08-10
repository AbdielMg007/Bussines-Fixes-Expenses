package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"runway/backend/internal/auth"
)

type AuthRepository struct {
	pool *pgxpool.Pool
}

func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{pool: pool}
}

func (r *AuthRepository) CreateOwner(ctx context.Context, owner auth.Owner) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO owners (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`,
		owner.ID(),
		owner.Email(),
		owner.PasswordHash(),
		owner.CreatedAt(),
		owner.UpdatedAt(),
	)
	if err == nil {
		return nil
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return auth.ErrOwnerExists
	}
	return errors.New("create owner")
}

func (r *AuthRepository) FindOwnerByEmail(ctx context.Context, email string) (auth.Owner, error) {
	return scanOwner(r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, created_at, updated_at
		FROM owners
		WHERE email = $1`, email), auth.ErrOwnerNotFound)
}

func (r *AuthRepository) CreateSession(ctx context.Context, session auth.Session) error {
	digest := session.Digest()
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO auth_sessions (token_digest, owner_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4)`,
		digest[:],
		session.OwnerID(),
		session.CreatedAt(),
		session.ExpiresAt(),
	); err != nil {
		return errors.New("create session")
	}
	return nil
}

func (r *AuthRepository) FindOwnerBySession(
	ctx context.Context,
	digest auth.SessionDigest,
	now time.Time,
) (auth.Owner, error) {
	return scanOwner(r.pool.QueryRow(ctx, `
		SELECT o.id, o.email, o.password_hash, o.created_at, o.updated_at
		FROM auth_sessions s
		JOIN owners o ON o.id = s.owner_id
		WHERE s.token_digest = $1 AND s.expires_at > $2`, digest[:], now), auth.ErrSessionNotFound)
}

func (r *AuthRepository) DeleteSession(ctx context.Context, digest auth.SessionDigest) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM auth_sessions WHERE token_digest = $1", digest[:]); err != nil {
		return errors.New("delete session")
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOwner(row rowScanner, notFound error) (auth.Owner, error) {
	var (
		id           string
		email        string
		passwordHash string
		createdAt    time.Time
		updatedAt    time.Time
	)
	if err := row.Scan(&id, &email, &passwordHash, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Owner{}, notFound
		}
		return auth.Owner{}, errors.New("read owner")
	}
	owner, err := auth.RestoreOwner(id, email, passwordHash, createdAt, updatedAt)
	if err != nil {
		return auth.Owner{}, fmt.Errorf("restore owner: %w", err)
	}
	return owner, nil
}
