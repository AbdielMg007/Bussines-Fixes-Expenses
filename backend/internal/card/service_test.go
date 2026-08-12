package card

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "runway/backend/internal/domain/card"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	ledger "runway/backend/internal/ledger"
)

func TestServiceRejectsInvalidStatementAuthorityBeforePersistence(t *testing.T) {
	repository := &fakeRepository{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	service.id = func() (string, error) { return "statement", nil }
	service.now = func() time.Time { return time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC) }
	balance, _ := money.New(100, money.MXN())
	date, _ := financialdate.Parse("2026-09-09")
	_, err = service.RegisterStatement(context.Background(), "owner", StatementInput{AccountID: "card", Start: date, End: date, Due: date, Authority: domain.Authority("invalid"), Balance: balance})
	if !errors.Is(err, domain.ErrInvalidStatement) {
		t.Fatalf("invalid authority error = %v", err)
	}
	if repository.registered {
		t.Fatal("repository was called for invalid authority")
	}
}

type fakeRepository struct{ registered bool }

func (f *fakeRepository) RegisterStatement(_ context.Context, _ string, _ StatementInput, _ string, _ time.Time) (domain.Statement, error) {
	f.registered = true
	return domain.Statement{}, nil
}
func (f *fakeRepository) ListStatements(context.Context, string, string) ([]domain.Statement, error) {
	return nil, nil
}
func (f *fakeRepository) GetStatement(context.Context, string, string) (domain.Statement, error) {
	return domain.Statement{}, nil
}
func (f *fakeRepository) GetIntent(context.Context, string, string) (domain.PaymentIntent, error) {
	return domain.PaymentIntent{}, nil
}
func (f *fakeRepository) ReplaceIntent(context.Context, string, string, money.Money, financialdate.Date, string, time.Time) (domain.PaymentIntent, error) {
	return domain.PaymentIntent{}, nil
}
func (f *fakeRepository) CancelIntent(context.Context, string, string, time.Time) (domain.PaymentIntent, error) {
	return domain.PaymentIntent{}, nil
}
func (f *fakeRepository) SettleIntent(context.Context, string, PaymentIntentSettlementInput, string, time.Time) (PaymentIntentSettlementResult, error) {
	return PaymentIntentSettlementResult{}, nil
}
func (f *fakeRepository) CreateInstallmentPlan(context.Context, string, InstallmentPlanInput, string, time.Time) (domain.InstallmentPlan, error) {
	return domain.InstallmentPlan{}, nil
}
func (f *fakeRepository) GetInstallmentPlan(context.Context, string, string) (domain.InstallmentPlan, error) {
	return domain.InstallmentPlan{}, nil
}
func (f *fakeRepository) ListInstallmentPlans(context.Context, string, string) ([]domain.InstallmentPlan, error) {
	return nil, nil
}
func (f *fakeRepository) ListInstallmentAllocations(context.Context, string, string) ([]domain.InstallmentAllocation, error) {
	return nil, nil
}
func (f *fakeRepository) GetInstallmentPlanSummary(context.Context, string, string) (InstallmentPlanSummary, error) {
	return InstallmentPlanSummary{}, nil
}
func (f *fakeRepository) RecordInstallmentPrincipalPayment(context.Context, string, string, money.Money, ledger.MutationIdentity, string, time.Time) (InstallmentPrincipalPaymentResult, error) {
	return InstallmentPrincipalPaymentResult{}, nil
}
