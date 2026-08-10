package ledger

import (
	"errors"
	"strings"
	"testing"
)

func TestParseIdempotencyKey(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "minimum", value: "a"},
		{name: "maximum", value: strings.Repeat("a", MaxIdempotencyKeyLength)},
		{name: "allowed punctuation", value: "client.request_2026-08-10:001"},
		{name: "empty", value: "", wantErr: true},
		{name: "too long", value: strings.Repeat("a", MaxIdempotencyKeyLength+1), wantErr: true},
		{name: "whitespace", value: "invalid key", wantErr: true},
		{name: "non ascii", value: "inválida", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key, err := ParseIdempotencyKey(test.value)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidIdempotencyKey) {
					t.Fatalf("ParseIdempotencyKey() error = %v", err)
				}
				return
			}
			if err != nil || key.String() != test.value {
				t.Fatalf("ParseIdempotencyKey() = %q, %v", key.String(), err)
			}
		})
	}
}

func TestMutationIdentityRejectsZeroFingerprint(t *testing.T) {
	key, _ := ParseIdempotencyKey("valid")
	if err := (MutationIdentity{Key: key}).Validate(); !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("MutationIdentity.Validate() error = %v", err)
	}
}
