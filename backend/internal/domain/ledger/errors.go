package ledger

import "errors"

var (
	ErrInvalidTransaction       = errors.New("invalid transaction")
	ErrInvalidTransactionKind   = errors.New("invalid transaction kind")
	ErrInvalidTransactionEffect = errors.New("invalid transaction effect")
	ErrIncompatibleEffect       = errors.New("transaction effect is incompatible with account type")
	ErrZeroAmount               = errors.New("transaction amount must be greater than zero")
	ErrInvalidSnapshot          = errors.New("invalid balance snapshot")
	ErrNegativeLiabilityBalance = errors.New("liability balance cannot be negative")
	ErrInvalidTransfer          = errors.New("invalid linked transfer")
	ErrSameTransferAccount      = errors.New("transfer source and destination must differ")
)
