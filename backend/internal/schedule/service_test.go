package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

func TestServiceBuildsCanonicalOwnerScopedMutations(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	mutations := make([]applicationledger.MutationIdentity, 0)
	repository := &spyRepository{
		createObligation: func(value domainschedule.Obligation, mutation applicationledger.MutationIdentity) (domainschedule.Obligation, error) {
			if value.OwnerID() != "owner" {
				t.Fatalf("owner = %q", value.OwnerID())
			}
			mutations = append(mutations, mutation)
			return value, nil
		},
	}
	service := newScheduleTestService(t, repository, now)
	amount, _ := money.New(100, money.MXN())
	start, _ := financialdate.Parse("2026-08-15")
	key := scheduleTestKey(t, "canonical")
	if _, err := service.CreateObligation(context.Background(), "owner", " Payment ", amount, domainschedule.Monthly(), start, nil, key); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateObligation(context.Background(), "owner", "Payment", amount, domainschedule.Monthly(), start, nil, key); err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 2 || mutations[0].Fingerprint != mutations[1].Fingerprint || mutations[0].Fingerprint == ([32]byte{}) {
		t.Fatalf("canonical fingerprints = %x and %x", mutations[0].Fingerprint, mutations[1].Fingerprint)
	}
	if _, err := service.CreateObligation(context.Background(), "", "Payment", amount, domainschedule.Monthly(), start, nil, key); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("invalid owner error = %v", err)
	}
}

func TestServiceExpansionDoesNotPersistOccurrences(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	amount, _ := money.New(100, money.MXN())
	start, _ := financialdate.Parse("2026-01-31")
	obligation, _ := domainschedule.NewObligation("obligation", "owner", "Monthly", amount, domainschedule.Monthly(), start, nil, now)
	getCalls := 0
	repository := &spyRepository{getObligation: func(ownerID, id string) (domainschedule.Obligation, error) {
		getCalls++
		return obligation, nil
	}}
	service := newScheduleTestService(t, repository, now)
	from, _ := financialdate.Parse("2026-01-01")
	to, _ := financialdate.Parse("2026-03-31")
	flows, err := service.ExpandObligation(context.Background(), "owner", obligation.ID(), from, to)
	if err != nil || getCalls != 1 || len(flows) != 3 || flows[1].FinancialDate().String() != "2026-02-28" {
		t.Fatalf("derived flows = %+v, calls = %d, error = %v", flows, getCalls, err)
	}
}

type spyRepository struct {
	Repository
	createObligation func(domainschedule.Obligation, applicationledger.MutationIdentity) (domainschedule.Obligation, error)
	getObligation    func(string, string) (domainschedule.Obligation, error)
}

func (r *spyRepository) CreateObligation(_ context.Context, value domainschedule.Obligation, mutation applicationledger.MutationIdentity) (domainschedule.Obligation, error) {
	return r.createObligation(value, mutation)
}

func (r *spyRepository) GetObligation(_ context.Context, ownerID, id string) (domainschedule.Obligation, error) {
	return r.getObligation(ownerID, id)
}

func newScheduleTestService(t *testing.T, repository Repository, now time.Time) *Service {
	t.Helper()
	next := 0
	service, err := NewService(repository, ServiceOptions{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) {
		next++
		return "id-" + time.Unix(int64(next), 0).UTC().Format("150405"), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func scheduleTestKey(t *testing.T, value string) applicationledger.IdempotencyKey {
	t.Helper()
	key, err := applicationledger.ParseIdempotencyKey(value)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
