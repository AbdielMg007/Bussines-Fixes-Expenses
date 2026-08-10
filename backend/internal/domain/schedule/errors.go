package schedule

import "errors"

var (
	ErrInvalidObligation       = errors.New("invalid obligation")
	ErrInvalidName             = errors.New("invalid display name")
	ErrInvalidRecurrence       = errors.New("invalid recurrence")
	ErrInvalidDateRange        = errors.New("invalid date range")
	ErrOccurrenceLimitExceeded = errors.New("obligation occurrence limit exceeded")
	ErrObligationArchived      = errors.New("obligation is archived")
	ErrInvalidDirection        = errors.New("invalid cash-flow direction")
	ErrInvalidSourceKind       = errors.New("invalid scheduled cash-flow source kind")
	ErrInvalidFlowStatus       = errors.New("invalid scheduled cash-flow status")
	ErrInvalidProvenance       = errors.New("invalid financial provenance")
	ErrInvalidInclusion        = errors.New("invalid inclusion eligibility")
	ErrInvalidScheduledFlow    = errors.New("invalid scheduled cash flow")
	ErrScheduledFlowFinal      = errors.New("scheduled cash flow is already final")
	ErrInvalidReceivable       = errors.New("invalid receivable")
	ErrInvalidCertainty        = errors.New("invalid receivable certainty")
	ErrInvalidReceivableStatus = errors.New("invalid receivable status")
	ErrReceivableFinal         = errors.New("receivable is already final")
	ErrCollectionExceedsAmount = errors.New("collection exceeds outstanding receivable")
	ErrInvalidCollection       = errors.New("invalid receivable collection")
)
