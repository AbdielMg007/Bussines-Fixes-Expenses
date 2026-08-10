package ledger

import "crypto/sha256"

const MaxIdempotencyKeyLength = 128

type IdempotencyKey struct {
	value string
}

func ParseIdempotencyKey(value string) (IdempotencyKey, error) {
	if len(value) == 0 || len(value) > MaxIdempotencyKeyLength {
		return IdempotencyKey{}, ErrInvalidIdempotencyKey
	}
	for _, character := range value {
		if !isIdempotencyCharacter(character) {
			return IdempotencyKey{}, ErrInvalidIdempotencyKey
		}
	}
	return IdempotencyKey{value: value}, nil
}

func (k IdempotencyKey) String() string {
	return k.value
}

func (m MutationIdentity) Validate() error {
	if _, err := ParseIdempotencyKey(m.Key.String()); err != nil {
		return err
	}
	if m.Fingerprint == ([sha256.Size]byte{}) {
		return ErrInvalidIdempotencyKey
	}
	return nil
}

func isIdempotencyCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		character == '-' || character == '_' || character == '.' || character == ':'
}
