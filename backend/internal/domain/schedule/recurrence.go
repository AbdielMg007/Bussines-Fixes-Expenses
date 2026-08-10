package schedule

import (
	"fmt"
	"time"

	"runway/backend/internal/domain/financialdate"
)

const MaxOccurrenceCount = 1000

func ExpandObligation(obligation Obligation, rangeStart, rangeEnd financialdate.Date) ([]ScheduledCashFlow, error) {
	comparison, err := rangeStart.Compare(rangeEnd)
	if err != nil || comparison > 0 {
		return nil, ErrInvalidDateRange
	}
	dates, err := occurrenceDates(obligation, rangeStart, rangeEnd)
	if err != nil {
		return nil, err
	}
	flows := make([]ScheduledCashFlow, 0, len(dates))
	for _, date := range dates {
		flow, err := NewObligationOccurrence(fmt.Sprintf("%s@%s", obligation.ID(), date.String()), obligation, date)
		if err != nil {
			return nil, err
		}
		flows = append(flows, flow)
	}
	return flows, nil
}

func occurrenceDates(obligation Obligation, rangeStart, rangeEnd financialdate.Date) ([]financialdate.Date, error) {
	start := obligation.StartDate()
	end, hasEnd := obligation.EndDate()
	inactiveFrom, hasInactiveFrom := obligation.InactiveFrom()
	within := func(date financialdate.Date) bool {
		beforeStart, _ := date.Compare(start)
		beforeRange, _ := date.Compare(rangeStart)
		afterRange, _ := date.Compare(rangeEnd)
		if beforeStart < 0 || beforeRange < 0 || afterRange > 0 {
			return false
		}
		if hasEnd {
			afterEnd, _ := date.Compare(end)
			if afterEnd > 0 {
				return false
			}
		}
		if hasInactiveFrom {
			inactiveComparison, _ := date.Compare(inactiveFrom)
			if inactiveComparison >= 0 {
				return false
			}
		}
		return true
	}
	appendDate := func(dates []financialdate.Date, date financialdate.Date) ([]financialdate.Date, error) {
		if len(dates) >= MaxOccurrenceCount {
			return nil, ErrOccurrenceLimitExceeded
		}
		return append(dates, date), nil
	}

	if obligation.Recurrence() == OneTime() {
		if within(start) {
			return []financialdate.Date{start}, nil
		}
		return []financialdate.Date{}, nil
	}

	dates := make([]financialdate.Date, 0)
	if obligation.Recurrence() == Monthly() {
		for monthOffset := 0; ; monthOffset++ {
			date, ok, err := anchoredMonth(start, monthOffset)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			afterRange, _ := date.Compare(rangeEnd)
			if afterRange > 0 {
				break
			}
			if hasEnd {
				afterEnd, _ := date.Compare(end)
				if afterEnd > 0 {
					break
				}
			}
			if hasInactiveFrom {
				inactiveComparison, _ := date.Compare(inactiveFrom)
				if inactiveComparison >= 0 {
					break
				}
			}
			if within(date) {
				dates, err = appendDate(dates, date)
				if err != nil {
					return nil, err
				}
			}
			rangeComparison, _ := date.Compare(rangeEnd)
			if rangeComparison == 0 {
				break
			}
			if hasEnd {
				endComparison, _ := date.Compare(end)
				if endComparison == 0 {
					break
				}
			}
		}
		return dates, nil
	}

	step := 7
	if obligation.Recurrence() == Biweekly() {
		step = 14
	}
	date := start
	for {
		afterRange, _ := date.Compare(rangeEnd)
		if afterRange > 0 {
			break
		}
		if hasEnd {
			afterEnd, _ := date.Compare(end)
			if afterEnd > 0 {
				break
			}
		}
		if hasInactiveFrom {
			inactiveComparison, _ := date.Compare(inactiveFrom)
			if inactiveComparison >= 0 {
				break
			}
		}
		if within(date) {
			updatedDates, appendErr := appendDate(dates, date)
			if appendErr != nil {
				return nil, appendErr
			}
			dates = updatedDates
		}
		rangeComparison, _ := date.Compare(rangeEnd)
		if rangeComparison == 0 {
			break
		}
		if hasEnd {
			endComparison, _ := date.Compare(end)
			if endComparison == 0 {
				break
			}
		}
		next, err := date.AddDays(step)
		if err != nil {
			return nil, err
		}
		date = next
	}
	return dates, nil
}

func anchoredMonth(start financialdate.Date, offset int) (financialdate.Date, bool, error) {
	monthIndex := (start.Year() * 12) + int(start.Month()-time.January) + offset
	year := monthIndex / 12
	month := time.Month(monthIndex%12) + time.January
	if year > 9999 {
		return financialdate.Date{}, false, financialdate.ErrInvalidDate
	}
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := start.Day()
	if day > lastDay {
		day = lastDay
	}
	date, err := financialdate.New(year, month, day)
	return date, err == nil, err
}
