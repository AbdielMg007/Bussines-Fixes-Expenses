package account

import (
	"errors"
	"testing"
	"time"

	"runway/backend/internal/domain/money"
)

func TestAccountCreationSupportsApprovedTypes(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for _, accountType := range []AccountType{Cash(), Bank(), CreditCard(), Loan()} {
		t.Run(accountType.String(), func(t *testing.T) {
			financialAccount, err := New("account-id", "owner-id", "  Main account  ", accountType, money.MXN(), now)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if financialAccount.DisplayName() != "Main account" || !financialAccount.Status().IsActive() {
				t.Fatalf("account = name %q status %q", financialAccount.DisplayName(), financialAccount.Status())
			}
		})
	}
}

func TestAccountRejectsInvalidInputAndArchivesWithoutDeletion(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	if _, err := New("id", "owner", "", Bank(), money.MXN(), now); !errors.Is(err, ErrInvalidDisplayName) {
		t.Fatalf("empty name error = %v", err)
	}
	if _, err := New("id", "owner", "name", AccountType{}, money.MXN(), now); !errors.Is(err, ErrInvalidAccountType) {
		t.Fatalf("invalid type error = %v", err)
	}
	financialAccount, err := New("id", "owner", "Bank", Bank(), money.MXN(), now)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := financialAccount.Archive(now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if !archived.Status().IsArchived() || !financialAccount.Status().IsActive() {
		t.Fatal("Archive() did not preserve immutable-style account state")
	}
	if _, err := archived.Archive(now.Add(2 * time.Hour)); !errors.Is(err, ErrAccountArchived) {
		t.Fatalf("second Archive() error = %v", err)
	}
}
