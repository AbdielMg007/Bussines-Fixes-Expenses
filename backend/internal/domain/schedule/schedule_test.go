package schedule

import (
	"errors"
	"testing"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

func TestObligationOccurrenceGeneration(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	amount, _ := money.New(100_00, money.MXN())
	tests := []struct {
		name       string
		recurrence Recurrence
		start      string
		end        string
		from       string
		to         string
		want       []string
	}{
		{name: "one time", recurrence: OneTime(), start: "2026-08-15", from: "2026-08-01", to: "2026-08-31", want: []string{"2026-08-15"}},
		{name: "weekly", recurrence: Weekly(), start: "2026-08-01", from: "2026-08-05", to: "2026-08-25", want: []string{"2026-08-08", "2026-08-15", "2026-08-22"}},
		{name: "biweekly exactly fourteen days", recurrence: Biweekly(), start: "2026-08-01", from: "2026-08-01", to: "2026-09-01", want: []string{"2026-08-01", "2026-08-15", "2026-08-29"}},
		{name: "month end non leap", recurrence: Monthly(), start: "2026-01-31", from: "2026-01-01", to: "2026-03-31", want: []string{"2026-01-31", "2026-02-28", "2026-03-31"}},
		{name: "month end leap year", recurrence: Monthly(), start: "2024-01-31", from: "2024-01-01", to: "2024-03-31", want: []string{"2024-01-31", "2024-02-29", "2024-03-31"}},
		{name: "January 30 non leap retains anchor", recurrence: Monthly(), start: "2026-01-30", from: "2026-01-01", to: "2026-03-31", want: []string{"2026-01-30", "2026-02-28", "2026-03-30"}},
		{name: "January 30 leap retains anchor", recurrence: Monthly(), start: "2024-01-30", from: "2024-01-01", to: "2024-03-31", want: []string{"2024-01-30", "2024-02-29", "2024-03-30"}},
		{name: "1900 is not leap", recurrence: Monthly(), start: "1900-01-31", from: "1900-01-01", to: "1900-03-31", want: []string{"1900-01-31", "1900-02-28", "1900-03-31"}},
		{name: "2000 is leap", recurrence: Monthly(), start: "2000-01-31", from: "2000-01-01", to: "2000-03-31", want: []string{"2000-01-31", "2000-02-29", "2000-03-31"}},
		{name: "2100 is not leap", recurrence: Monthly(), start: "2100-01-31", from: "2100-01-01", to: "2100-03-31", want: []string{"2100-01-31", "2100-02-28", "2100-03-31"}},
		{name: "inclusive obligation end", recurrence: Weekly(), start: "2026-08-01", end: "2026-08-15", from: "2026-08-01", to: "2026-09-01", want: []string{"2026-08-01", "2026-08-08", "2026-08-15"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := mustDate(t, test.start)
			var end *financialdate.Date
			if test.end != "" {
				value := mustDate(t, test.end)
				end = &value
			}
			obligation, err := NewObligation("obligation", "owner", "Payment", amount, test.recurrence, start, end, now)
			if err != nil {
				t.Fatal(err)
			}
			flows, err := ExpandObligation(obligation, mustDate(t, test.from), mustDate(t, test.to))
			if err != nil {
				t.Fatal(err)
			}
			if len(flows) != len(test.want) {
				t.Fatalf("occurrence count = %d, want %d", len(flows), len(test.want))
			}
			seen := make(map[string]bool)
			for index, flow := range flows {
				got := flow.FinancialDate().String()
				if got != test.want[index] || flow.Direction() != Outflow() || flow.SourceKind() != ObligationSource() || flow.SourceID() != obligation.ID() {
					t.Fatalf("flow[%d] = date %s direction %s source %s/%s", index, got, flow.Direction(), flow.SourceKind(), flow.SourceID())
				}
				if seen[flow.ID()] {
					t.Fatalf("duplicate occurrence ID %q", flow.ID())
				}
				seen[flow.ID()] = true
			}
		})
	}
}

func TestArchivedObligationPreservesOnlyHistoricalOccurrences(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	amount, _ := money.New(1, money.MXN())
	obligation, _ := NewObligation("obligation", "owner", "Payment", amount, Biweekly(), mustDate(t, "2026-08-01"), nil, now)
	inactiveFrom := mustDate(t, "2026-08-29")
	archived, err := obligation.Archive(inactiveFrom, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	flows, err := ExpandObligation(archived, mustDate(t, "2026-08-01"), mustDate(t, "2026-09-30"))
	if err != nil || len(flows) != 2 || flows[0].FinancialDate().String() != "2026-08-01" || flows[1].FinancialDate().String() != "2026-08-15" {
		t.Fatalf("archived flows = %+v, error = %v", flows, err)
	}
	if got, ok := archived.InactiveFrom(); !ok || got.String() != inactiveFrom.String() {
		t.Fatalf("inactive_from = %s, present = %t", got, ok)
	}
	if _, err := ExpandObligation(obligation, mustDate(t, "2026-09-01"), mustDate(t, "2026-08-01")); !errors.Is(err, ErrInvalidDateRange) {
		t.Fatalf("invalid range error = %v", err)
	}
}

func TestOccurrenceGenerationBounds(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	amount, _ := money.New(1, money.MXN())
	start := mustDate(t, "2000-01-01")
	weekly, _ := NewObligation("weekly", "owner", "Weekly", amount, Weekly(), start, nil, now)

	atLimit, err := start.AddDays((MaxOccurrenceCount - 1) * 7)
	if err != nil {
		t.Fatal(err)
	}
	flows, err := ExpandObligation(weekly, start, atLimit)
	if err != nil || len(flows) != MaxOccurrenceCount {
		t.Fatalf("at-limit flows = %d, error = %v", len(flows), err)
	}
	overLimit, err := start.AddDays(MaxOccurrenceCount * 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExpandObligation(weekly, start, overLimit); !errors.Is(err, ErrOccurrenceLimitExceeded) {
		t.Fatalf("limit+1 error = %v", err)
	}
	if _, err := ExpandObligation(weekly, mustDate(t, "0001-01-01"), mustDate(t, "9999-12-31")); !errors.Is(err, ErrOccurrenceLimitExceeded) {
		t.Fatalf("huge weekly range error = %v", err)
	}

	monthly, _ := NewObligation("monthly", "owner", "Monthly", amount, Monthly(), mustDate(t, "0001-01-31"), nil, now)
	if _, err := ExpandObligation(monthly, mustDate(t, "0001-01-01"), mustDate(t, "9999-12-31")); !errors.Is(err, ErrOccurrenceLimitExceeded) {
		t.Fatalf("huge monthly range error = %v", err)
	}

	nearUpper, _ := NewObligation("upper", "owner", "Upper", amount, Weekly(), mustDate(t, "9999-12-30"), nil, now)
	if _, err := ExpandObligation(nearUpper, mustDate(t, "9999-12-30"), mustDate(t, "9999-12-31")); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("upper-bound recurrence error = %v", err)
	}
	monthlyNearUpper, _ := NewObligation("monthly-upper", "owner", "Monthly upper", amount, Monthly(), mustDate(t, "9999-12-30"), nil, now)
	if _, err := ExpandObligation(monthlyNearUpper, mustDate(t, "9999-12-30"), mustDate(t, "9999-12-31")); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("monthly upper-bound recurrence error = %v", err)
	}
}

func TestScheduledCashFlowProvenanceAndCancellation(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	amount, _ := money.New(500, money.MXN())
	for _, test := range []struct {
		name             string
		direction        Direction
		amountProvenance AmountProvenance
		dateProvenance   DateProvenance
	}{
		{name: "exact amount exact date inflow", direction: Inflow(), amountProvenance: ExactAmount(), dateProvenance: ExactDate()},
		{name: "exact amount estimated date inflow", direction: Inflow(), amountProvenance: ExactAmount(), dateProvenance: EstimatedDate()},
		{name: "estimated amount exact date outflow", direction: Outflow(), amountProvenance: EstimatedAmount(), dateProvenance: ExactDate()},
		{name: "estimated amount estimated date outflow", direction: Outflow(), amountProvenance: EstimatedAmount(), dateProvenance: EstimatedDate()},
	} {
		t.Run(test.name, func(t *testing.T) {
			flow, err := NewManualScheduledCashFlow("flow", "owner", amount, test.direction, mustDate(t, "2026-08-15"), ManualOtherSource(), test.amountProvenance, test.dateProvenance, EligibleForPolicy(), now)
			if err != nil {
				t.Fatal(err)
			}
			cancelled, err := flow.Cancel(now.Add(time.Hour))
			if err != nil || !cancelled.Status().IsCancelled() || !flow.Status().IsScheduled() {
				t.Fatalf("cancelled flow = %+v, error = %v", cancelled, err)
			}
			if _, err := cancelled.Cancel(now.Add(2 * time.Hour)); !errors.Is(err, ErrScheduledFlowFinal) {
				t.Fatalf("second cancellation error = %v", err)
			}
		})
	}
	if _, err := NewManualScheduledCashFlow("flow", "owner", amount, Inflow(), mustDate(t, "2026-08-15"), ManualOtherSource(), ExactAmount(), AssumedByScenarioDate(), EligibleForPolicy(), now); !errors.Is(err, ErrInvalidScheduledFlow) {
		t.Fatalf("persisted scenario-assumed flow error = %v", err)
	}
}

func TestReceivableCollectionConservesAmount(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	original, _ := money.New(35_000_00, money.MXN())
	receivable, err := NewReceivable("receivable", "owner", "Family business", original, nil, Uncertain(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, dated := receivable.ExpectedDate(); dated || receivable.Certainty() != Uncertain() {
		t.Fatal("undated uncertain receivable did not preserve metadata")
	}
	partial, _ := money.New(5_000_00, money.MXN())
	receivable, first, err := receivable.RecordCollection("collection-1", partial, "", now.Add(time.Hour))
	if err != nil || receivable.Status() != PartialReceivable() || first.CollectedAfter().MinorUnits() != 5_000_00 || first.OutstandingAfter().MinorUnits() != 30_000_00 {
		t.Fatalf("partial collection = %+v / %+v, error = %v", receivable, first, err)
	}
	outstanding, _ := receivable.OutstandingAmount()
	receivable, second, err := receivable.RecordCollection("collection-2", outstanding, "ledger-tx", now.Add(2*time.Hour))
	if err != nil || receivable.Status() != CollectedReceivable() || second.OutstandingAfter().MinorUnits() != 0 {
		t.Fatalf("full collection = %+v / %+v, error = %v", receivable, second, err)
	}
	total, _ := receivable.CollectedAmount().Add(mustOutstanding(t, receivable))
	equal, _ := total.Equal(receivable.OriginalAmount())
	if !equal {
		t.Fatal("receivable conservation failed")
	}
	if _, _, err := receivable.RecordCollection("collection-3", partial, "", now.Add(3*time.Hour)); !errors.Is(err, ErrReceivableFinal) {
		t.Fatalf("collection after full collection error = %v", err)
	}
}

func TestReceivableRejectsOverCollectionAndCollectionAfterCancellation(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	original, _ := money.New(100, money.MXN())
	receivable, _ := NewReceivable("receivable", "owner", "Reimbursement", original, nil, Confirmed(), now)
	tooMuch, _ := money.New(101, money.MXN())
	if _, _, err := receivable.RecordCollection("collection", tooMuch, "", now.Add(time.Hour)); !errors.Is(err, ErrCollectionExceedsAmount) {
		t.Fatalf("over-collection error = %v", err)
	}
	cancelled, err := receivable.Cancel(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	one, _ := money.New(1, money.MXN())
	if _, _, err := cancelled.RecordCollection("collection", one, "", now.Add(2*time.Hour)); !errors.Is(err, ErrReceivableFinal) {
		t.Fatalf("collection after cancellation error = %v", err)
	}
}

func mustDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func mustOutstanding(t *testing.T, receivable Receivable) money.Money {
	t.Helper()
	value, err := receivable.OutstandingAmount()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
