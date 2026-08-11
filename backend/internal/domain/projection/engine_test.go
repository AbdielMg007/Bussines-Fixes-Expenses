package projection

import (
	"errors"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	"runway/backend/internal/domain/schedule"
)

func TestCalculateOrdersSameDayOutflowsBeforeInflowsDeterministically(t *testing.T) {
	policy := testPolicy(t, 30)
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(30)
	eventDate := testDate(t, "2026-08-15")
	outflow := testEvent(t, "z-outflow", eventDate, 226_400, schedule.Outflow())
	inflow := testEvent(t, "a-inflow", eventDate, 788_700, schedule.Inflow())
	opening, _ := money.NewBalance(260_000, money.MXN())

	first, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: opening, Events: []Event{inflow, outflow}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: opening, Events: []Event{outflow, inflow}})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []Result{first, second} {
		if len(result.Events) != 2 || result.Events[0].Event.ID() != "z-outflow" || result.Events[0].BalanceAfter.MinorUnits() != 33_600 || result.Events[1].BalanceAfter.MinorUnits() != 822_300 {
			t.Fatalf("ordered result = %+v", result.Events)
		}
		if result.ClosingBalance.MinorUnits() != 822_300 || result.MinimumBalance.MinorUnits() != 33_600 || result.MinimumEventID != "z-outflow" {
			t.Fatalf("summary = close %d min %d event %q", result.ClosingBalance.MinorUnits(), result.MinimumBalance.MinorUnits(), result.MinimumEventID)
		}
	}
}

func TestCalculateMinimumIncludesOpeningAndEarliestEqualLow(t *testing.T) {
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(30)
	date := testDate(t, "2026-08-15")
	policy := testPolicy(t, 30)

	openingNegative, _ := money.NewBalance(-500, money.MXN())
	inflow := testEvent(t, "inflow", date, 1_000, schedule.Inflow())
	result, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: openingNegative, Events: []Event{inflow}})
	if err != nil || result.MinimumBalance.MinorUnits() != -500 || result.MinimumEventID != "" || result.MinimumDate != nil {
		t.Fatalf("opening minimum = %+v, %v", result, err)
	}

	opening, _ := money.NewBalance(1_000, money.MXN())
	result, err = Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: opening})
	if err != nil || result.OpeningBalance.MinorUnits() != 1_000 || result.ClosingBalance.MinorUnits() != 1_000 || result.MinimumBalance.MinorUnits() != 1_000 {
		t.Fatalf("no-event projection = %+v, %v", result, err)
	}

	outflow := testEvent(t, "outflow", date, 900, schedule.Outflow())
	largeInflow := testEvent(t, "large-inflow", date, 2_000, schedule.Inflow())
	result, err = Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: opening, Events: []Event{largeInflow, outflow}})
	if err != nil || result.MinimumBalance.MinorUnits() != 100 || result.MinimumEventID != "outflow" {
		t.Fatalf("same-day temporary minimum = %+v, %v", result, err)
	}

	openingTwo, _ := money.NewBalance(2_000, money.MXN())
	firstOut := testEvent(t, "a-first-low", date, 1_000, schedule.Outflow())
	reset := testEvent(t, "reset", date, 1_000, schedule.Inflow())
	secondDate := testDate(t, "2026-08-16")
	secondOut := testEvent(t, "z-second-low", secondDate, 1_000, schedule.Outflow())
	result, err = Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: openingTwo, Events: []Event{secondOut, reset, firstOut}})
	if err != nil || result.MinimumBalance.MinorUnits() != 1_000 || result.MinimumEventID != "a-first-low" || result.MinimumDate == nil || result.MinimumDate.String() != "2026-08-15" {
		t.Fatalf("equal minimum = %+v, %v", result, err)
	}
}

func TestCalculateHorizonIntervalAndSupportedDateOverflow(t *testing.T) {
	asOf := testDate(t, "2026-08-10")
	end := testDate(t, "2026-08-11")
	policy := testPolicy(t, MinHorizonDays)
	zero, _ := money.ZeroBalance(money.MXN())
	tomorrow := testEvent(t, "tomorrow", end, 1, schedule.Outflow())
	if result, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: zero, Events: []Event{tomorrow}}); err != nil || len(result.Events) != 1 {
		t.Fatalf("inclusive horizon end = %+v, %v", result, err)
	}
	for _, event := range []Event{
		testEvent(t, "today", asOf, 1, schedule.Outflow()),
		testEvent(t, "after", testDate(t, "2026-08-12"), 1, schedule.Outflow()),
	} {
		if _, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: zero, Events: []Event{event}}); !errors.Is(err, ErrEventOutsideHorizon) {
			t.Fatalf("event %s boundary error = %v", event.ID(), err)
		}
	}

	upper := testDate(t, "9999-12-31")
	if _, err := Calculate(Input{Policy: policy, AsOf: upper, HorizonEnd: upper, OpeningBalance: zero}); !errors.Is(err, financialdate.ErrInvalidDate) {
		t.Fatalf("upper horizon error = %v", err)
	}
}

func TestCalculateShuffledEventsAndReserveAreArithmeticIndependent(t *testing.T) {
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(30)
	date := testDate(t, "2026-08-15")
	events := []Event{
		testEventWithSource(t, "manual-b", ManualScheduledFlowSource(), date, 20, schedule.Outflow()),
		testEventWithSource(t, "obligation", ObligationOccurrenceSource(), date, 10, schedule.Outflow()),
		testEventWithSource(t, "manual-a", ManualScheduledFlowSource(), date, 30, schedule.Outflow()),
		testEvent(t, "inflow", date, 100, schedule.Inflow()),
	}
	opening, _ := money.NewBalance(1_000, money.MXN())
	baseline, err := Calculate(Input{Policy: testPolicyWithReserve(t, 30, 0), AsOf: asOf, HorizonEnd: end, OpeningBalance: opening, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	random := rand.New(rand.NewSource(7))
	for iteration := 0; iteration < 50; iteration++ {
		shuffled := append([]Event(nil), events...)
		random.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		result, err := Calculate(Input{Policy: testPolicyWithReserve(t, 30, 5_000), AsOf: asOf, HorizonEnd: end, OpeningBalance: opening, Events: shuffled})
		if err != nil {
			t.Fatal(err)
		}
		if result.OpeningBalance != baseline.OpeningBalance || result.ClosingBalance != baseline.ClosingBalance || result.MinimumBalance != baseline.MinimumBalance ||
			result.MinimumEventID != baseline.MinimumEventID || !reflect.DeepEqual(result.MinimumDate, baseline.MinimumDate) ||
			!reflect.DeepEqual(result.Events, baseline.Events) {
			t.Fatalf("iteration %d changed arithmetic/order: %+v", iteration, result)
		}
	}
}

func TestCalculateStableTieBreakerForSameDirection(t *testing.T) {
	policy := testPolicy(t, 10)
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(10)
	date := testDate(t, "2026-08-11")
	events := []Event{
		testEventWithSource(t, "b", ManualScheduledFlowSource(), date, 1, schedule.Outflow()),
		testEventWithSource(t, "z", ObligationOccurrenceSource(), date, 1, schedule.Outflow()),
		testEventWithSource(t, "a", ManualScheduledFlowSource(), date, 1, schedule.Outflow()),
	}
	zero, _ := money.ZeroBalance(money.MXN())
	result, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: zero, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"z", "a", "b"}
	for i, id := range want {
		if result.Events[i].Event.ID() != id {
			t.Fatalf("event %d = %s, want %s", i, result.Events[i].Event.ID(), id)
		}
	}
}

func TestCalculateCheckedArithmeticAndNegativeBalances(t *testing.T) {
	policy := testPolicy(t, 10)
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(10)
	date := testDate(t, "2026-08-11")
	oneIn := testEvent(t, "in", date, 1, schedule.Inflow())
	oneOut := testEvent(t, "out", date, 1, schedule.Outflow())

	max, _ := money.NewBalance(math.MaxInt64, money.MXN())
	if _, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: max, Events: []Event{oneIn}}); !errors.Is(err, money.ErrMonetaryAmountOverflow) {
		t.Fatalf("inflow overflow error = %v", err)
	}
	min, _ := money.NewBalance(math.MinInt64, money.MXN())
	if _, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: min, Events: []Event{oneOut}}); !errors.Is(err, money.ErrMonetaryAmountOverflow) {
		t.Fatalf("outflow underflow error = %v", err)
	}
	zero, _ := money.ZeroBalance(money.MXN())
	amount, _ := money.New(500, money.MXN())
	negativeEvent, _ := NewEvent("expense", ManualScheduledFlowSource(), "flow", date, amount, schedule.Outflow(), schedule.ExactAmount(), schedule.ExactDate(), MandatoryManualOutflow(), nil, "")
	result, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: zero, Events: []Event{negativeEvent}})
	if err != nil || result.ClosingBalance.MinorUnits() != -500 || result.MinimumBalance.MinorUnits() != -500 {
		t.Fatalf("negative result = %+v, %v", result, err)
	}
}

func TestCalculateRejectsCurrentDayEventAndDoesNotSubtractReserve(t *testing.T) {
	policy := testPolicy(t, 10)
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(10)
	opening, _ := money.NewBalance(1_000, money.MXN())
	event := testEvent(t, "today", asOf, 1, schedule.Outflow())
	if _, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: opening, Events: []Event{event}}); !errors.Is(err, ErrEventOutsideHorizon) {
		t.Fatalf("current-day error = %v", err)
	}
	result, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: opening})
	if err != nil || result.ClosingBalance.MinorUnits() != 1_000 || result.Reserve.MinorUnits() != 10_000 {
		t.Fatalf("reserve changed balance: %+v, %v", result, err)
	}
}

func TestCalculateRejectsDuplicateStableEventIdentity(t *testing.T) {
	policy := testPolicy(t, 10)
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(10)
	zero, _ := money.ZeroBalance(money.MXN())
	event := testEvent(t, "duplicate", testDate(t, "2026-08-11"), 1, schedule.Outflow())
	if _, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: zero, Events: []Event{event, event}}); !errors.Is(err, ErrInvalidProjectionEvent) {
		t.Fatalf("duplicate event error = %v", err)
	}
}

func TestCalculateExclusionsAreCanonicalAndDoNotAffectBalances(t *testing.T) {
	policy := testPolicy(t, 10)
	asOf := testDate(t, "2026-08-10")
	end, _ := asOf.AddDays(10)
	opening, _ := money.NewBalance(1_000, money.MXN())
	uncertain, _ := NewExclusion(ReceivableSource(), "z-uncertain", []ExclusionReason{ExclusionUncertainReceivable}, "Uncertain")
	undated, _ := NewExclusion(ReceivableSource(), "a-undated", []ExclusionReason{ExclusionUncertainReceivable, ExclusionUndatedReceivable, ExclusionUndatedReceivable}, "Undated")
	policyExcluded, _ := NewExclusion(ManualScheduledFlowSource(), "flow", []ExclusionReason{ExclusionPolicyExcludedInflow}, "")
	input := []Exclusion{uncertain, undated, policyExcluded}
	random := rand.New(rand.NewSource(11))
	for iteration := 0; iteration < 20; iteration++ {
		shuffled := append([]Exclusion(nil), input...)
		random.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		result, err := Calculate(Input{Policy: policy, AsOf: asOf, HorizonEnd: end, OpeningBalance: opening, Exclusions: shuffled})
		if err != nil {
			t.Fatal(err)
		}
		if result.ClosingBalance != opening || len(result.Events) != 0 || len(result.Exclusions) != 3 ||
			result.Exclusions[0].SourceID != "flow" || result.Exclusions[1].SourceID != "a-undated" || result.Exclusions[2].SourceID != "z-uncertain" {
			t.Fatalf("iteration %d exclusions = %+v", iteration, result.Exclusions)
		}
		if len(result.Exclusions[1].Reasons) != 2 || result.Exclusions[1].Reasons[0] != ExclusionUncertainReceivable || result.Exclusions[1].Reasons[1] != ExclusionUndatedReceivable {
			t.Fatalf("canonical reasons = %+v", result.Exclusions[1].Reasons)
		}
	}
}

func testPolicy(t *testing.T, horizon int) Policy {
	return testPolicyWithReserve(t, horizon, 10_000)
}

func testPolicyWithReserve(t *testing.T, horizon int, reserveMinor int64) Policy {
	t.Helper()
	reserve, _ := money.New(reserveMinor, money.MXN())
	selection, _ := NewAccountSelection(AllActiveLiquidSelection(), nil)
	policy, err := NewPolicy("policy", "owner", money.MXN(), horizon, reserve, "America/Mexico_City", selection, ConfirmedInflowsOnly(), OutflowsBeforeInflows(), time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func testDate(t *testing.T, value string) financialdate.Date {
	t.Helper()
	date, err := financialdate.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func testEvent(t *testing.T, id string, date financialdate.Date, minor int64, direction schedule.Direction) Event {
	t.Helper()
	return testEventWithSource(t, id, ManualScheduledFlowSource(), date, minor, direction)
}

func testEventWithSource(t *testing.T, id string, source EventSourceKind, date financialdate.Date, minor int64, direction schedule.Direction) Event {
	t.Helper()
	amount, _ := money.New(minor, money.MXN())
	basis := MandatoryManualOutflow()
	if direction == schedule.Inflow() {
		basis = EligibleManualInflow()
	}
	if source == ObligationOccurrenceSource() {
		basis = MandatoryObligationOutflow()
	}
	event, err := NewEvent(id, source, "source-"+id, date, amount, direction, schedule.ExactAmount(), schedule.ExactDate(), basis, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return event
}
