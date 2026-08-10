package auth

import (
	"errors"
	"testing"
)

func TestSessionCredentialsAreRandomAndDigestible(t *testing.T) {
	first, err := GenerateSessionCredential()
	if err != nil {
		t.Fatalf("first GenerateSessionCredential() error = %v", err)
	}
	second, err := GenerateSessionCredential()
	if err != nil {
		t.Fatalf("second GenerateSessionCredential() error = %v", err)
	}
	if first.Token == second.Token || first.Digest == second.Digest {
		t.Fatal("independent session credentials must differ")
	}
	digest, err := DigestSessionToken(first.Token)
	if err != nil {
		t.Fatalf("DigestSessionToken() error = %v", err)
	}
	if digest != first.Digest {
		t.Fatal("session token digest does not match generated digest")
	}
}

func TestDigestSessionTokenRejectsMalformedValues(t *testing.T) {
	for _, token := range []string{"", "not-base64!", "c2hvcnQ"} {
		t.Run(token, func(t *testing.T) {
			if _, err := DigestSessionToken(token); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("error = %v, want %v", err, ErrUnauthenticated)
			}
		})
	}
}
