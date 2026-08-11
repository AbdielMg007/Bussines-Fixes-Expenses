package projection

import (
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
)

const (
	MinHorizonDays = 1
	MaxHorizonDays = 366
)

type AccountSelectionMode struct{ value string }

var (
	allActiveLiquidSelection = AccountSelectionMode{value: "all_active_liquid"}
	explicitSelection        = AccountSelectionMode{value: "explicit"}
)

func AllActiveLiquidSelection() AccountSelectionMode { return allActiveLiquidSelection }
func ExplicitSelection() AccountSelectionMode        { return explicitSelection }
func (m AccountSelectionMode) String() string        { return m.value }

func ParseAccountSelectionMode(value string) (AccountSelectionMode, error) {
	switch value {
	case allActiveLiquidSelection.value:
		return allActiveLiquidSelection, nil
	case explicitSelection.value:
		return explicitSelection, nil
	default:
		return AccountSelectionMode{}, fmt.Errorf("%w: %q", ErrInvalidAccountSelection, value)
	}
}

type AccountSelection struct {
	mode       AccountSelectionMode
	accountIDs []string
}

func NewAccountSelection(mode AccountSelectionMode, accountIDs []string) (AccountSelection, error) {
	if _, err := ParseAccountSelectionMode(mode.String()); err != nil {
		return AccountSelection{}, err
	}
	if mode == allActiveLiquidSelection && len(accountIDs) != 0 {
		return AccountSelection{}, ErrInvalidAccountSelection
	}
	ids := make([]string, 0, len(accountIDs))
	seen := make(map[string]struct{}, len(accountIDs))
	for _, raw := range accountIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return AccountSelection{}, ErrInvalidAccountSelection
		}
		if _, exists := seen[id]; exists {
			return AccountSelection{}, ErrInvalidAccountSelection
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return AccountSelection{mode: mode, accountIDs: ids}, nil
}

func (s AccountSelection) Mode() AccountSelectionMode { return s.mode }
func (s AccountSelection) AccountIDs() []string       { return append([]string(nil), s.accountIDs...) }

type InflowPolicy struct{ value string }

var (
	confirmedInflowsOnly = InflowPolicy{value: "confirmed_only"}
	includeExpected      = InflowPolicy{value: "include_expected"}
)

func ConfirmedInflowsOnly() InflowPolicy   { return confirmedInflowsOnly }
func IncludeExpectedInflows() InflowPolicy { return includeExpected }
func (p InflowPolicy) String() string      { return p.value }

func ParseInflowPolicy(value string) (InflowPolicy, error) {
	switch value {
	case confirmedInflowsOnly.value:
		return confirmedInflowsOnly, nil
	case includeExpected.value:
		return includeExpected, nil
	default:
		return InflowPolicy{}, fmt.Errorf("%w: %q", ErrInvalidInflowPolicy, value)
	}
}

func (p InflowPolicy) IncludesExpected() bool { return p == includeExpected }

type SameDayOrder struct{ value string }

var outflowsBeforeInflows = SameDayOrder{value: "outflows_before_inflows"}

func OutflowsBeforeInflows() SameDayOrder { return outflowsBeforeInflows }
func (o SameDayOrder) String() string     { return o.value }

func ParseSameDayOrder(value string) (SameDayOrder, error) {
	if value != outflowsBeforeInflows.value {
		return SameDayOrder{}, fmt.Errorf("%w: %q", ErrInvalidSameDayOrder, value)
	}
	return outflowsBeforeInflows, nil
}

type Policy struct {
	id           string
	ownerID      string
	currency     money.Currency
	horizonDays  int
	reserve      money.Money
	timezone     string
	selection    AccountSelection
	inflowPolicy InflowPolicy
	sameDayOrder SameDayOrder
	version      int64
	createdAt    time.Time
	updatedAt    time.Time
}

func NewPolicy(
	id, ownerID string,
	currency money.Currency,
	horizonDays int,
	reserve money.Money,
	timezone string,
	selection AccountSelection,
	inflowPolicy InflowPolicy,
	sameDayOrder SameDayOrder,
	now time.Time,
) (Policy, error) {
	return RestorePolicy(id, ownerID, currency, horizonDays, reserve, timezone, selection, inflowPolicy, sameDayOrder, 1, now, now)
}

func RestorePolicy(
	id, ownerID string,
	currency money.Currency,
	horizonDays int,
	reserve money.Money,
	timezone string,
	selection AccountSelection,
	inflowPolicy InflowPolicy,
	sameDayOrder SameDayOrder,
	version int64,
	createdAt, updatedAt time.Time,
) (Policy, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(ownerID) == "" || version < 1 || createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return Policy{}, ErrInvalidPolicy
	}
	if horizonDays < MinHorizonDays || horizonDays > MaxHorizonDays {
		return Policy{}, ErrInvalidHorizon
	}
	if _, err := money.Zero(currency); err != nil {
		return Policy{}, err
	}
	if _, err := money.New(reserve.MinorUnits(), reserve.Currency()); err != nil {
		return Policy{}, err
	}
	if equal, err := money.Zero(currency); err != nil {
		return Policy{}, err
	} else if _, err := reserve.Equal(equal); err != nil {
		return Policy{}, err
	}
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return Policy{}, ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Policy{}, fmt.Errorf("%w: %q", ErrInvalidTimezone, timezone)
	}
	if _, err := NewAccountSelection(selection.Mode(), selection.AccountIDs()); err != nil {
		return Policy{}, err
	}
	if _, err := ParseInflowPolicy(inflowPolicy.String()); err != nil {
		return Policy{}, err
	}
	if _, err := ParseSameDayOrder(sameDayOrder.String()); err != nil {
		return Policy{}, err
	}
	return Policy{
		id: strings.TrimSpace(id), ownerID: strings.TrimSpace(ownerID), currency: currency,
		horizonDays: horizonDays, reserve: reserve, timezone: timezone, selection: selection,
		inflowPolicy: inflowPolicy, sameDayOrder: sameDayOrder, version: version,
		createdAt: createdAt.UTC(), updatedAt: updatedAt.UTC(),
	}, nil
}

func FinancialDateAt(now time.Time, timezone string) (financialdate.Date, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return financialdate.Date{}, fmt.Errorf("%w: %q", ErrInvalidTimezone, timezone)
	}
	local := now.In(location)
	return financialdate.New(local.Year(), local.Month(), local.Day())
}

func (p Policy) ID() string                         { return p.id }
func (p Policy) OwnerID() string                    { return p.ownerID }
func (p Policy) Currency() money.Currency           { return p.currency }
func (p Policy) HorizonDays() int                   { return p.horizonDays }
func (p Policy) Reserve() money.Money               { return p.reserve }
func (p Policy) FinancialTimezone() string          { return p.timezone }
func (p Policy) AccountSelection() AccountSelection { return p.selection }
func (p Policy) InflowPolicy() InflowPolicy         { return p.inflowPolicy }
func (p Policy) SameDayOrder() SameDayOrder         { return p.sameDayOrder }
func (p Policy) Version() int64                     { return p.version }
func (p Policy) CreatedAt() time.Time               { return p.createdAt }
func (p Policy) UpdatedAt() time.Time               { return p.updatedAt }
