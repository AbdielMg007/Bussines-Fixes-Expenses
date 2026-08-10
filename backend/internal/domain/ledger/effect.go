package ledger

import (
	"runway/backend/internal/domain/account"
	"runway/backend/internal/domain/money"
)

type Effect struct {
	value string
}

var (
	assetInflowEffect      = Effect{value: "asset_inflow"}
	assetOutflowEffect     = Effect{value: "asset_outflow"}
	liabilityChargeEffect  = Effect{value: "liability_charge"}
	liabilityPaymentEffect = Effect{value: "liability_payment"}
)

func AssetInflow() Effect      { return assetInflowEffect }
func AssetOutflow() Effect     { return assetOutflowEffect }
func LiabilityCharge() Effect  { return liabilityChargeEffect }
func LiabilityPayment() Effect { return liabilityPaymentEffect }

func ParseEffect(value string) (Effect, error) {
	switch value {
	case assetInflowEffect.value:
		return assetInflowEffect, nil
	case assetOutflowEffect.value:
		return assetOutflowEffect, nil
	case liabilityChargeEffect.value:
		return liabilityChargeEffect, nil
	case liabilityPaymentEffect.value:
		return liabilityPaymentEffect, nil
	default:
		return Effect{}, ErrInvalidTransactionEffect
	}
}

func (e Effect) String() string {
	return e.value
}

func (e Effect) ValidateFor(accountType account.AccountType) error {
	if _, err := ParseEffect(e.value); err != nil {
		return err
	}

	if accountType.CanRepresentAsset() && (e == assetInflowEffect || e == assetOutflowEffect) {
		return nil
	}
	if accountType.CanRepresentLiability() && (e == liabilityChargeEffect || e == liabilityPaymentEffect) {
		return nil
	}
	return ErrIncompatibleEffect
}

func (e Effect) Apply(balance money.Balance, amount money.Money) (money.Balance, error) {
	delta, err := money.NewBalance(amount.MinorUnits(), amount.Currency())
	if err != nil {
		return money.Balance{}, err
	}

	switch e {
	case assetInflowEffect, liabilityChargeEffect:
		return balance.Add(delta)
	case assetOutflowEffect, liabilityPaymentEffect:
		return balance.Subtract(delta)
	default:
		return money.Balance{}, ErrInvalidTransactionEffect
	}
}
