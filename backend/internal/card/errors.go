package card

import "errors"

var (
	ErrNotFound = errors.New("credit card resource not found")
	ErrConflict = errors.New("credit card resource conflict")
)
