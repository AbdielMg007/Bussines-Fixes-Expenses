package projection

import "errors"

var (
	ErrInvalidPolicy           = errors.New("invalid projection policy")
	ErrInvalidHorizon          = errors.New("invalid projection horizon")
	ErrInvalidTimezone         = errors.New("invalid financial timezone")
	ErrInvalidAccountSelection = errors.New("invalid projection account selection")
	ErrInvalidInflowPolicy     = errors.New("invalid inflow inclusion policy")
	ErrInvalidSameDayOrder     = errors.New("invalid same-day event ordering")
	ErrInvalidProjectionEvent  = errors.New("invalid projection event")
	ErrEventOutsideHorizon     = errors.New("projection event is outside the projection interval")
)
