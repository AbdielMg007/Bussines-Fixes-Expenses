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
type InstallmentPlanInput struct {
	AccountID             string
	Description           string
	PurchaseTransactionID string
	Principal             money.Money
	InstallmentCount      int
	FirstCycleStart       financialdate.Date
	FirstCycleEnd         financialdate.Date
	Mutation              ledger.MutationIdentity
}
type InstallmentPrincipalPaymentResult struct {
	Payment domain.InstallmentPrincipalPayment
	Summary InstallmentPlanSummary
}
type PaymentIntentSettlement struct {
	ID, OwnerID, AccountID, CycleID, IntentID, TransferID string
	Amount                                                money.Money
	CreatedAt                                             time.Time
}
type PaymentIntentSettlementInput struct {
	IntentID, TransferID string
	Mutation             ledger.MutationIdentity
}
type PaymentIntentSettlementResult struct {
	Settlement PaymentIntentSettlement
	Intent     domain.PaymentIntent
}

// PaymentIntentSummary is a read-only view derived exclusively from explicit
// settlement lineage. It is deliberately not stored as a second source of truth.
type PaymentIntentSummary struct {
	Intent          domain.PaymentIntent
	SettledAmount   money.Money
	RemainingAmount money.Money
}
type InstallmentPlanSummary struct {
	Plan                 domain.InstallmentPlan
	PaidPrincipal        money.Money
	OutstandingPrincipal money.Money
}
type Repository interface {
	RegisterStatement(context.Context, string, StatementInput, string, time.Time) (domain.Statement, error)
	ListStatements(context.Context, string, string) ([]domain.Statement, error)
	GetStatement(context.Context, string, string) (domain.Statement, error)
	GetIntent(context.Context, string, string) (domain.PaymentIntent, error)
	GetIntentSummary(context.Context, string, string) (PaymentIntentSummary, error)
	ReplaceIntent(context.Context, string, string, money.Money, financialdate.Date, string, time.Time) (domain.PaymentIntent, error)
	CancelIntent(context.Context, string, string, time.Time) (domain.PaymentIntent, error)
	SettleIntent(context.Context, string, PaymentIntentSettlementInput, string, time.Time) (PaymentIntentSettlementResult, error)
	CreateInstallmentPlan(context.Context, string, InstallmentPlanInput, string, time.Time) (domain.InstallmentPlan, error)
	GetInstallmentPlan(context.Context, string, string) (domain.InstallmentPlan, error)
	ListInstallmentPlans(context.Context, string, string) ([]domain.InstallmentPlan, error)
	ListInstallmentAllocations(context.Context, string, string) ([]domain.InstallmentAllocation, error)
	GetInstallmentPlanSummary(context.Context, string, string) (InstallmentPlanSummary, error)
	RecordInstallmentPrincipalPayment(context.Context, string, string, money.Money, ledger.MutationIdentity, string, time.Time) (InstallmentPrincipalPaymentResult, error)
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
func (s *Service) GetIntentSummary(ctx context.Context, owner, cycle string) (PaymentIntentSummary, error) {
	return s.repo.GetIntentSummary(ctx, owner, cycle)
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
func (s *Service) SettleIntent(ctx context.Context, owner string, in PaymentIntentSettlementInput) (PaymentIntentSettlementResult, error) {
	if owner == "" || in.IntentID == "" || in.TransferID == "" {
		return PaymentIntentSettlementResult{}, domain.ErrInvalidPaymentIntentSettlement
	}
	id, err := s.id()
	if err != nil {
		return PaymentIntentSettlementResult{}, err
	}
	return s.repo.SettleIntent(ctx, owner, in, id, s.now().UTC())
}
func (s *Service) CreateInstallmentPlan(ctx context.Context, owner string, in InstallmentPlanInput) (domain.InstallmentPlan, error) {
	if owner == "" {
		return domain.InstallmentPlan{}, ErrNotFound
	}
	id, err := s.id()
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	return s.repo.CreateInstallmentPlan(ctx, owner, in, id, s.now().UTC())
}
func (s *Service) GetInstallmentPlan(ctx context.Context, owner, planID string) (domain.InstallmentPlan, error) {
	return s.repo.GetInstallmentPlan(ctx, owner, planID)
}
func (s *Service) ListInstallmentPlans(ctx context.Context, owner, accountID string) ([]domain.InstallmentPlan, error) {
	return s.repo.ListInstallmentPlans(ctx, owner, accountID)
}
func (s *Service) ListInstallmentAllocations(ctx context.Context, owner, planID string) ([]domain.InstallmentAllocation, error) {
	return s.repo.ListInstallmentAllocations(ctx, owner, planID)
}
func (s *Service) GetInstallmentPlanSummary(ctx context.Context, owner, planID string) (InstallmentPlanSummary, error) {
	return s.repo.GetInstallmentPlanSummary(ctx, owner, planID)
}
func (s *Service) RecordInstallmentPrincipalPayment(ctx context.Context, owner, allocationID string, amount money.Money, mutation ledger.MutationIdentity) (InstallmentPrincipalPaymentResult, error) {
	if owner == "" {
		return InstallmentPrincipalPaymentResult{}, ErrNotFound
	}
	id, err := s.id()
	if err != nil {
		return InstallmentPrincipalPaymentResult{}, err
	}
	return s.repo.RecordInstallmentPrincipalPayment(ctx, owner, allocationID, amount, mutation, id, s.now().UTC())
}
func newID() (string, error) {
	b := make([]byte, 18)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
