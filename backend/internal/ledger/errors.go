package ledger

import "errors"

var (
	ErrNotFound                  = errors.New("financial resource not found")
	ErrConflict                  = errors.New("financial resource conflict")
	ErrSnapshotCutoffUnknown     = errors.New("snapshot cutoff does not identify a transaction on this account")
	ErrSnapshotCutoffRegression  = errors.New("snapshot cutoff precedes an existing snapshot")
	ErrInvalidOwner              = errors.New("invalid owner identifier")
	ErrInvalidIdempotencyKey     = errors.New("invalid idempotency key")
	ErrIdempotencyConflict       = errors.New("idempotency key was already used for a different request")
	ErrLiabilityOverpayment      = errors.New("payment exceeds current liability")
	ErrNegativeLiabilitySnapshot = errors.New("liability snapshot cannot be negative")
)
