package projection

import (
	"errors"
	"math"
	"testing"
	"time"

	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	"runway/backend/internal/domain/schedule"
)

func TestSafeToSpendFutureCashFlowAndBaselineBreach(t *testing.T) {
	asOf := testDate(t, "2026-08-10")
	eventDate := testDate(t, "2026-08-15")
	endDate := testDate(t, "2026-09-01")
	events := []Event{
		testEvent(t, "aug-15-outflow", eventDate, 226_400, schedule.Outflow()),
		testEvent(t, "aug-15-inflow", eventDate, 788_700, schedule.Inflow()),
		testEvent(t, "sep-1-outflow", endDate, 222_300, schedule.Outflow()),
	}
	opening, _ := money.NewBalance(260_000, money.MXN())
	funding := safeFundingAccount(t, "bank", account.Bank())
	fundingBalance, _ := money.NewBalance(260_000, money.MXN())

	baseline := safeBaseline(t, asOf, 30, 30_000, opening, events)
	result, err := CalculateSafeToSpend(baseline, funding, fundingBalance)
	if err != nil {
		t.Fatal(err)
	}
	if result.SafeToSpend.MinorUnits() != 3_600 || result.Status != ConstrainedByFutureCashFlowStatus() ||
		result.LimitingEventID != "aug-15-outflow" || result.LimitingDate == nil || result.LimitingDate.String() != "2026-08-15" {
		t.Fatalf("future-constrained result = %+v", result)
	}

	below := safeBaseline(t, asOf, 30, 50_000, opening, events)
	result, err = CalculateSafeToSpend(below, funding, fundingBalance)
	if err != nil {
		t.Fatal(err)
	}
	if result.SafeToSpend.MinorUnits() != 0 || result.Status != AlreadyBelowReserveStatus() || result.Deficit.MinorUnits() != 16_400 ||
		result.EarliestBreachEventID != "aug-15-outflow" || result.EarliestBreachDate == nil || result.EarliestBreachDate.String() != "2026-08-15" {
		t.Fatalf("below-reserve result = %+v", result)
	}
}

func TestSafeToSpendFundingCapOpeningBreachAndNoEvents(t *testing.T) {
	asOf := testDate(t, "2026-08-10")
	bank := safeFundingAccount(t, "bank", account.Bank())

	opening, _ := money.NewBalance(60_000, money.MXN())
	fundingBalance, _ := money.NewBalance(20_000, money.MXN())
	result, err := CalculateSafeToSpend(safeBaseline(t, asOf, 30, 10_000, opening, nil), bank, fundingBalance)
	if err != nil || result.SafeToSpend.MinorUnits() != 20_000 || result.Status != ConstrainedByFundingBalanceStatus() {
		t.Fatalf("funding cap = %+v, %v", result, err)
	}

	openingBelow, _ := money.NewBalance(10_000, money.MXN())
	result, err = CalculateSafeToSpend(safeBaseline(t, asOf, 30, 30_000, openingBelow, nil), bank, fundingBalance)
	if err != nil || result.Status != AlreadyBelowReserveStatus() || result.Deficit.MinorUnits() != 20_000 || result.EarliestBreachEventID != "" ||
		result.EarliestBreachDate == nil || result.EarliestBreachDate.String() != asOf.String() {
		t.Fatalf("opening breach = %+v, %v", result, err)
	}

	openingNoEvents, _ := money.NewBalance(500_000, money.MXN())
	fullFunding, _ := money.NewBalance(500_000, money.MXN())
	result, err = CalculateSafeToSpend(safeBaseline(t, asOf, 30, 100_000, openingNoEvents, nil), bank, fullFunding)
	if err != nil || result.SafeToSpend.MinorUnits() != 400_000 || result.Status != ConstrainedByFutureCashFlowStatus() {
		t.Fatalf("no events = %+v, %v", result, err)
	}
}

func TestSafeToSpendExactReserveMultipleAccountsAndNegativeFunding(t *testing.T) {
	asOf := testDate(t, "2026-08-10")
	bank := safeFundingAccount(t, "bank", account.Bank())
	cash := safeFundingAccount(t, "cash", account.Cash())
	opening, _ := money.NewBalance(450_000, money.MXN())
	baseline := safeBaseline(t, asOf, 30, 100_000, opening, nil)
	for _, test := range []struct {
		name    string
		account account.Account
		balance int64
		want    int64
	}{
		{name: "bank", account: bank, balance: 300_000, want: 300_000},
		{name: "cash", account: cash, balance: 200_000, want: 200_000},
		{name: "negative bank", account: bank, balance: -100, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			balance, _ := money.NewBalance(test.balance, money.MXN())
			result, err := CalculateSafeToSpend(baseline, test.account, balance)
			if err != nil || result.SafeToSpend.MinorUnits() != test.want || result.Status != ConstrainedByFundingBalanceStatus() {
				t.Fatalf("result = %+v, %v", result, err)
			}
		})
	}

	exactOpening, _ := money.NewBalance(30_000, money.MXN())
	positiveFunding, _ := money.NewBalance(30_000, money.MXN())
	result, err := CalculateSafeToSpend(safeBaseline(t, asOf, 30, 30_000, exactOpening, nil), bank, positiveFunding)
	if err != nil || result.SafeToSpend.MinorUnits() != 0 || result.Status != ConstrainedByFutureCashFlowStatus() {
		t.Fatalf("exact reserve = %+v, %v", result, err)
	}
}

func TestSafeToSpendRejectsUnsupportedFundingAndArithmeticOverflow(t *testing.T) {
	asOf := testDate(t, "2026-08-10")
	opening, _ := money.NewBalance(100_000, money.MXN())
	baseline := safeBaseline(t, asOf, 30, 0, opening, nil)
	liabilityBalance, _ := money.NewBalance(50_000, money.MXN())
	for _, accountType := range []account.AccountType{account.CreditCard(), account.Loan()} {
		result, err := CalculateSafeToSpend(baseline, safeFundingAccount(t, accountType.String(), accountType), liabilityBalance)
		if err != nil || result.Status != UnsupportedFundingTypeStatus() || result.SafeToSpend.MinorUnits() != 0 {
			t.Fatalf("unsupported %s = %+v, %v", accountType.String(), result, err)
		}
	}

	reserve, _ := money.New(1, money.MXN())
	minimum, _ := money.NewBalance(math.MinInt64, money.MXN())
	invalid := baseline
	invalid.Reserve = reserve
	invalid.MinimumBalance = minimum
	if _, err := CalculateSafeToSpend(invalid, safeFundingAccount(t, "bank", account.Bank()), opening); !errors.Is(err, money.ErrMonetaryAmountOverflow) {
		t.Fatalf("deficit overflow error = %v", err)
	}
}

func safeBaseline(t *testing.T, asOf financialdate.Date, horizon int, reserveMinor int64, opening money.Balance, events []Event) Result {
	t.Helper()
	end, err := asOf.AddDays(horizon)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Calculate(Input{
		Policy: testPolicyWithReserve(t, horizon, reserveMinor), AsOf: asOf, HorizonEnd: end,
		SelectedAccountIDs: []string{"bank", "cash"}, OpeningBalance: opening, Events: events,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func safeFundingAccount(t *testing.T, id string, accountType account.AccountType) account.Account {
	t.Helper()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	value, err := account.Restore(id, "owner", id, accountType, money.MXN(), account.ActiveStatus(), now, now)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
