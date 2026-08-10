package ledger

type TransactionKind struct {
	value string
}

var (
	manualKind   = TransactionKind{value: "manual"}
	transferKind = TransactionKind{value: "transfer"}
)

func ManualKind() TransactionKind   { return manualKind }
func TransferKind() TransactionKind { return transferKind }

func ParseTransactionKind(value string) (TransactionKind, error) {
	switch value {
	case manualKind.value:
		return manualKind, nil
	case transferKind.value:
		return transferKind, nil
	default:
		return TransactionKind{}, ErrInvalidTransactionKind
	}
}

func (k TransactionKind) String() string {
	return k.value
}
