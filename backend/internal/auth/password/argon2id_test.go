package password

import (
	"errors"
	"strings"
	"testing"
)

func TestArgon2idHashAndVerify(t *testing.T) {
	hasher := newTestHasher(t)
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	valid, err := hasher.Verify("correct horse battery staple", encoded)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !valid {
		t.Fatal("Verify() = false, want true")
	}
	valid, err = hasher.Verify("wrong password", encoded)
	if err != nil {
		t.Fatalf("Verify(wrong password) error = %v", err)
	}
	if valid {
		t.Fatal("Verify(wrong password) = true, want false")
	}
}

func TestArgon2idUsesRandomSalt(t *testing.T) {
	hasher := newTestHasher(t)
	first, err := hasher.Hash("same password")
	if err != nil {
		t.Fatalf("first Hash() error = %v", err)
	}
	second, err := hasher.Hash("same password")
	if err != nil {
		t.Fatalf("second Hash() error = %v", err)
	}
	if first == second {
		t.Fatal("equal passwords produced equal hashes; want distinct random salts")
	}
}

func TestArgon2idRejectsMalformedHash(t *testing.T) {
	hasher := newTestHasher(t)
	valid, err := hasher.Hash("password value")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	tests := []struct {
		name string
		hash string
	}{
		{name: "empty", hash: ""},
		{name: "wrong algorithm", hash: strings.Replace(valid, "$argon2id$", "$argon2i$", 1)},
		{name: "wrong version", hash: strings.Replace(valid, "$v=19$", "$v=18$", 1)},
		{name: "trailing parameter data", hash: strings.Replace(valid, ",p=1$", ",p=1junk$", 1)},
		{name: "unsafe memory", hash: strings.Replace(valid, "m=8192", "m=999999", 1)},
		{name: "invalid base64", hash: valid + "!"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verified, err := hasher.Verify("password value", test.hash)
			if verified {
				t.Fatal("Verify() = true for malformed hash")
			}
			if !errors.Is(err, ErrInvalidEncodedHash) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidEncodedHash)
			}
		})
	}
}

func TestDefaultParametersAreExplicit(t *testing.T) {
	want := Parameters{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2, SaltBytes: 16, KeyBytes: 32}
	if DefaultParameters() != want {
		t.Fatalf("DefaultParameters() = %+v, want %+v", DefaultParameters(), want)
	}
}

func newTestHasher(t *testing.T) *Argon2id {
	t.Helper()
	hasher, err := NewArgon2id(Parameters{
		MemoryKiB:   8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltBytes:   16,
		KeyBytes:    32,
	})
	if err != nil {
		t.Fatalf("NewArgon2id() error = %v", err)
	}
	return hasher
}
