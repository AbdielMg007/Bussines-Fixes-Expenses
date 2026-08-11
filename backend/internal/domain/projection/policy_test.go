package projection

import (
	"errors"
	"testing"
	"time"

	"runway/backend/internal/domain/money"
)

func TestProjectionPolicyValidation(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	zero, _ := money.Zero(money.MXN())
	selection, _ := NewAccountSelection(AllActiveLiquidSelection(), nil)
	valid := func(horizon int, timezone string) error {
		_, err := NewPolicy("policy", "owner", money.MXN(), horizon, zero, timezone, selection, ConfirmedInflowsOnly(), OutflowsBeforeInflows(), now)
		return err
	}
	for _, horizon := range []int{MinHorizonDays, MaxHorizonDays} {
		if err := valid(horizon, "America/Mexico_City"); err != nil {
			t.Fatalf("valid horizon %d: %v", horizon, err)
		}
	}
	for _, horizon := range []int{0, MaxHorizonDays + 1} {
		if err := valid(horizon, "America/Mexico_City"); !errors.Is(err, ErrInvalidHorizon) {
			t.Fatalf("horizon %d error = %v", horizon, err)
		}
	}
	if err := valid(60, "Not/A_Real_Zone"); !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("invalid timezone error = %v", err)
	}
}

func TestAccountSelectionCanonicalizesAndRejectsDuplicates(t *testing.T) {
	selection, err := NewAccountSelection(ExplicitSelection(), []string{"bank-b", "bank-a"})
	if err != nil {
		t.Fatal(err)
	}
	ids := selection.AccountIDs()
	if len(ids) != 2 || ids[0] != "bank-a" || ids[1] != "bank-b" {
		t.Fatalf("canonical IDs = %v", ids)
	}
	ids[0] = "mutated"
	if selection.AccountIDs()[0] != "bank-a" {
		t.Fatal("selection exposed mutable account IDs")
	}
	if _, err := NewAccountSelection(ExplicitSelection(), []string{"bank", "bank"}); !errors.Is(err, ErrInvalidAccountSelection) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := NewAccountSelection(AllActiveLiquidSelection(), []string{"bank"}); !errors.Is(err, ErrInvalidAccountSelection) {
		t.Fatalf("all-active IDs error = %v", err)
	}
	if empty, err := NewAccountSelection(ExplicitSelection(), nil); err != nil || len(empty.AccountIDs()) != 0 {
		t.Fatalf("empty explicit selection = %+v, %v", empty, err)
	}
}

func TestFinancialDateAtUsesPolicyTimezone(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time
		timezone string
		want     string
	}{
		{name: "Mexico before local midnight", now: time.Date(2026, 8, 11, 5, 59, 0, 0, time.UTC), timezone: "America/Mexico_City", want: "2026-08-10"},
		{name: "Mexico after local midnight", now: time.Date(2026, 8, 11, 6, 1, 0, 0, time.UTC), timezone: "America/Mexico_City", want: "2026-08-11"},
		{name: "New York DST offset before fall-back", now: time.Date(2026, 11, 1, 4, 30, 0, 0, time.UTC), timezone: "America/New_York", want: "2026-11-01"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			date, err := FinancialDateAt(test.now, test.timezone)
			if err != nil {
				t.Fatal(err)
			}
			if date.String() != test.want {
				t.Fatalf("financial date = %s, want %s", date.String(), test.want)
			}
		})
	}
}
