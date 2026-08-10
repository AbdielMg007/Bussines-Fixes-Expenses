package account

import "errors"

var ErrInvalidStatus = errors.New("invalid account status")

type Status struct {
	value string
}

var (
	activeStatus   = Status{value: "active"}
	archivedStatus = Status{value: "archived"}
)

func ActiveStatus() Status {
	return activeStatus
}

func ArchivedStatus() Status {
	return archivedStatus
}

func ParseStatus(value string) (Status, error) {
	switch value {
	case activeStatus.value:
		return activeStatus, nil
	case archivedStatus.value:
		return archivedStatus, nil
	default:
		return Status{}, ErrInvalidStatus
	}
}

func (s Status) String() string {
	return s.value
}

func (s Status) IsActive() bool {
	return s == activeStatus
}

func (s Status) IsArchived() bool {
	return s == archivedStatus
}
