package card

import (
	"testing"

	"runway/backend/internal/domain/money"
)

func TestBuildInstallmentScheduleConservesPrincipalAndAssignsRemainderEarliest(t *testing.T) {
	principal, _ := money.New(1_000_000, money.MXN())
	entries, err := BuildInstallmentSchedule(principal, 3, date(t, "2026-01-31"), date(t, "2026-02-28"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Principal.MinorUnits() != 333_334 || entries[1].Principal.MinorUnits() != 333_333 || entries[2].Principal.MinorUnits() != 333_333 {
		t.Fatalf("remainder allocation = %+v", entries)
	}
	if entries[0].CycleStart.String() != "2026-01-31" || entries[1].CycleStart.String() != "2026-02-28" || entries[2].CycleStart.String() != "2026-03-31" {
		t.Fatalf("cycle anchors = %+v", entries)
	}
	total, _ := money.Zero(money.MXN())
	for index, entry := range entries {
		if entry.InstallmentNumber != index+1 {
			t.Fatalf("installment number %d", entry.InstallmentNumber)
		}
		total, err = total.Add(entry.Principal)
		if err != nil {
			t.Fatal(err)
		}
	}
	if equal, _ := total.Equal(principal); !equal {
		t.Fatalf("total = %d, want %d", total.MinorUnits(), principal.MinorUnits())
	}
}

func TestBuildInstallmentScheduleRejectsInvalidInputsAndRangeOverflow(t *testing.T) {
	principal, _ := money.New(1, money.MXN())
	if _, err := BuildInstallmentSchedule(principal, 0, date(t, "2026-01-01"), date(t, "2026-01-31")); err == nil {
		t.Fatal("zero installment count accepted")
	}
	if _, err := BuildInstallmentSchedule(principal, 2, date(t, "9999-12-01"), date(t, "9999-12-31")); err == nil {
		t.Fatal("out-of-range schedule accepted")
	}
	if _, err := BuildInstallmentSchedule(principal, 12, date(t, "2026-01-01"), date(t, "2026-01-31")); err == nil {
		t.Fatal("principal smaller than installment count accepted")
	}
}

func TestBuildInstallmentScheduleRequiresPositiveAllocationForEveryInstallment(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal int64
		count     int
		wantMinor []int64
		wantErr   bool
	}{
		{name: "five over twelve rejected", principal: 5, count: 12, wantErr: true},
		{name: "twelve over twelve", principal: 12, count: 12, wantMinor: []int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
		{name: "thirteen over twelve", principal: 13, count: 12, wantMinor: []int64{2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			principal, err := money.New(test.principal, money.MXN())
			if err != nil {
				t.Fatal(err)
			}
			entries, err := BuildInstallmentSchedule(principal, test.count, date(t, "2026-01-01"), date(t, "2026-01-31"))
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(test.wantMinor) {
				t.Fatalf("entries = %d, want %d", len(entries), len(test.wantMinor))
			}
			for index, entry := range entries {
				if entry.Principal.MinorUnits() != test.wantMinor[index] || entry.Principal.MinorUnits() <= 0 {
					t.Fatalf("entry %d = %d", index, entry.Principal.MinorUnits())
				}
			}
		})
	}
}
