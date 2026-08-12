package card

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	domain "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	ledger "runway/backend/internal/ledger"
	"time"
)

type StatementInput struct {
	AccountID              string
	Start, End, Due        financialdate.Date
	Authority              domain.Authority
	Balance                money.Money
	Minimum, AvoidInterest *money.Money
	Mutation               ledger.MutationIdentity
}
type Repository interface {
	RegisterStatement(context.Context, string, StatementInput, string, time.Time) (domain.Statement, error)
	ListStatements(context.Context, string, string) ([]domain.Statement, error)
	GetStatement(context.Context, string, string) (domain.Statement, error)
	GetIntent(context.Context, string, string) (domain.PaymentIntent, error)
	ReplaceIntent(context.Context, string, string, money.Money, financialdate.Date, string, time.Time) (domain.PaymentIntent, error)
	CancelIntent(context.Context, string, string, time.Time) (domain.PaymentIntent, error)
}
type Service struct {
	repo Repository
	now  func() time.Time
	id   func() (string, error)
}

func NewService(repo Repository) (*Service, error) {
	if repo == nil {
		return nil, errors.New("invalid card service")
	}
	return &Service{repo, time.Now, newID}, nil
}
func (s *Service) RegisterStatement(ctx context.Context, owner string, in StatementInput) (domain.Statement, error) {
	if owner == "" {
		return domain.Statement{}, ErrNotFound
	}
	if in.Authority != domain.Estimated && in.Authority != domain.Issued {
		return domain.Statement{}, domain.ErrInvalidStatement
	}
	id, err := s.id()
	if err != nil {
		return domain.Statement{}, err
	}
	return s.repo.RegisterStatement(ctx, owner, in, id, s.now().UTC())
}
func (s *Service) ListStatements(ctx context.Context, owner, account string) ([]domain.Statement, error) {
	return s.repo.ListStatements(ctx, owner, account)
}
func (s *Service) GetStatement(ctx context.Context, owner, id string) (domain.Statement, error) {
	return s.repo.GetStatement(ctx, owner, id)
}
func (s *Service) GetIntent(ctx context.Context, owner, cycle string) (domain.PaymentIntent, error) {
	return s.repo.GetIntent(ctx, owner, cycle)
}
func (s *Service) ReplaceIntent(ctx context.Context, owner, cycle string, amount money.Money, date financialdate.Date) (domain.PaymentIntent, error) {
	id, err := s.id()
	if err != nil {
		return domain.PaymentIntent{}, err
	}
	return s.repo.ReplaceIntent(ctx, owner, cycle, amount, date, id, s.now().UTC())
}
func (s *Service) CancelIntent(ctx context.Context, owner, cycle string) (domain.PaymentIntent, error) {
	return s.repo.CancelIntent(ctx, owner, cycle, s.now().UTC())
}
func newID() (string, error) {
	b := make([]byte, 18)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
