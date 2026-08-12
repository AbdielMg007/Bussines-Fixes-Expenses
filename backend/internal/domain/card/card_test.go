package card

import (
	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	"testing"
	"time"
)

func date(t *testing.T, s string) financialdate.Date {
	t.Helper()
	d, e := financialdate.Parse(s)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestCycleAndStatementValidation(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, e := NewCycle("c", "o", "a", date(t, "2026-01-31"), date(t, "2026-01-01"), now); e == nil {
		t.Fatal("reversed cycle accepted")
	}
	m, _ := money.New(5000, money.MXN())
	more, _ := money.New(5001, money.MXN())
	if _, e := NewStatement("s", "o", "a", "c", 1, Issued, m, &more, nil, date(t, "2026-02-01"), now); e == nil {
		t.Fatal("optional amount over balance accepted")
	}
	if _, e := NewStatement("s", "o", "a", "c", 1, Issued, m, nil, nil, date(t, "2026-02-01"), now); e != nil {
		t.Fatal(e)
	}
}
func TestPaymentIntentRequiresPositiveAmountAndValidStatus(t *testing.T) {
	now := time.Now().UTC()
	zero, _ := money.Zero(money.MXN())
	if _, e := NewPaymentIntent("i", "o", "a", "c", zero, date(t, "2026-01-01"), IntentActive, 1, now, now); e == nil {
		t.Fatal("zero intent accepted")
	}
	m, _ := money.New(1, money.MXN())
	if _, e := NewPaymentIntent("i", "o", "a", "c", m, date(t, "2026-01-01"), IntentStatus("bad"), 1, now, now); e == nil {
		t.Fatal("invalid status accepted")
	}
}
