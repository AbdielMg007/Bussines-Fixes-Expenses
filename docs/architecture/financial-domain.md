# Financial Domain

## Purpose and scope

Runway answers: “How much can I safely spend through a particular funding account without compromising obligations within the effective evaluation horizon?” Financial truth is calculated by deterministic application code under a `ProjectionPolicy`. AI may propose structured input or explain a result, but it cannot calculate or persist financial truth.

A monetary amount or magnitude is a non-negative integer number of minor units paired with a currency. Account balances and projected balances are signed integer minor units so deficits remain visible; they must never be clamped to zero. Liability principal and the required cash reserve are non-negative. The initial product supports MXN only. Financial dates use the user's timezone; audit timestamps use UTC.

## Terminology

### Account

A user-owned container for financial value or debt. An account has one currency, an account type, and an asset or liability classification. Its balance is account-specific; it is not automatically liquid cash.

### Asset account

An account holding value owned by the user, such as cash or a bank account. An asset account contributes to consolidated liquid cash only when it is marked liquid and included by policy.

### Liability account

An account holding an amount owed by the user, such as a credit card or loan. Liability balances are expressed as positive amounts owed. A liability account never contributes to liquidity.

### Current account balance

The reconciled, signed balance of one account as of a specific ledger position. For an asset it normally represents value held; for a liability its non-negative outstanding magnitude represents value owed. It is derived from one authoritative snapshot plus account effects strictly after that snapshot's cutoff. It is neither a forecast nor a cross-account total. Negative asset balances, such as overdrafts, remain negative unless an explicitly modeled overdraft facility says otherwise.

### BalanceSnapshot

An authoritative observation of an account balance at an effective instant and exact ledger cutoff/cursor. It establishes a reconciliation baseline without pretending that the observation itself is a purchase, income, or transfer. A reconstructed current balance is:

```text
snapshot_balance
+ account_effects_of_transactions_with_cursor_strictly_after(snapshot_cutoff)
```

The cursor is a total ordering, such as an institution ledger sequence or a stable internal sequence paired with timestamp, so equal timestamps are unambiguous. Transactions at or before the cutoff are already represented by the snapshot and are not reapplied. Each transaction contributes exactly once. For a liquid bank account, the snapshot should use the spendable/available bank balance when that differs from the ledger balance.

### Consolidated liquid cash

The sum of current balances in included liquid asset accounts, in one currency, at an as-of instant. It excludes liabilities, credit limits, available credit, receivables, investments, and restricted balances.

### Available credit

The remaining amount an issuer currently permits the user to borrow. It constrains new card purchases but is not an asset, cash, income, or liquidity.

Runway preserves two values with provenance and observation time:

- Issuer-reported available credit.
- Locally calculated available credit based on a known limit and a complete card ledger cutoff.

A value is eligible as a safe purchasing bound only when it satisfies the policy's maximum-age requirement and its inputs are complete. When both are eligible, the lower value is used. When one is eligible, it is used and missing or stale alternatives produce a warning. A stale value may be displayed but cannot be the sole bound. When neither is eligible, card-funded safe-to-spend is indeterminate.

### Total debt / liabilities

The sum, in one currency, of current positive amounts owed across liability accounts. It includes card principal immediately when a card purchase posts. It excludes interest or fees that have not been incurred or explicitly modeled. A planned payment does not reduce actual debt until payment posts.

### Transaction

A posted economic movement affecting one or more accounts. Its amount, currency, financial date, account effect, and origin are explicit. A future plan is not a transaction. When a scheduled event becomes a transaction, source identity links them so the projection does not count both.

### Linked transfer

Two account effects representing one movement of value. A transfer between included liquid asset accounts changes their individual balances but not consolidated liquid cash. A card payment may also use linked effects, but because its destination is a liability, it decreases consolidated liquid cash and debt.

### CreditCardStatement

An issuer-defined billing record with statement date, due date, statement balance, minimum payment, payment required to avoid interest, payment status, and component allocations. It aggregates regular purchases, MSI principal allocations, fees, interest, taxes, and other statement components. An issued statement is authoritative for its fields and supersedes the provisional statement estimate with the same card-and-cycle identity. Assigning any component to a statement does not create liability.

### PaymentIntent

A user's selected plan for satisfying exactly one card statement, such as minimum, interest-saving, full-statement, or fixed payment. Only one active intent exists per statement. It generates exactly one projected statement-level payment `ScheduledCashFlow` and does not change an actual account balance. If the strategy cannot determine a final amount or date, the projection is indeterminate for a candidate that depends on it.

### InstallmentPlan / MSI

A principal-allocation schedule attached to a card purchase. The purchase creates its entire principal liability exactly once on the purchase date. The plan allocates that existing principal among statement cycles; an allocation is not a cash flow and never creates principal. The statement-level `PaymentIntent`, not an installment allocation, creates the projected cash outflow.

For a 6,000 MXN purchase at 6 MSI:

- Card liability increases by 6,000 MXN at purchase.
- Available credit decreases by 6,000 MXN at purchase.
- Liquid cash does not change at purchase.
- Six 1,000 MXN principal allocations are assigned to statement cycles.
- Each posted statement payment reduces liquid cash, and only its explicit plan-linked principal component reduces outstanding MSI principal.
- No interest or extra liability is created for 0% MSI.

MSI principal obeys both conservation equations at all times:

```text
original_principal = paid_principal + outstanding_principal
sum(active_unpaid_principal_allocations) = outstanding_principal
```

Only one allocation schedule version may be active. A correction or issuer-provided schedule supersedes the prior version instead of appending to it. Total active and paid principal must never exceed or fall short of original principal.

For an estimated schedule, divide principal by installment count and assign any remainder one minor unit at a time to the earliest installments. Issuer statement allocations supersede estimates with the same logical identity without duplication.

### Obligation

A one-time or recurring commitment to pay, such as rent or a car payment. It is a definition or source record, not a posted transaction. It generates dated `ScheduledCashFlow` occurrences within a projection horizon.

### ScheduledCashFlow

A concrete, dated, future inflow or outflow consumed by the projection engine. It records amount, currency, financial date, source identity, inclusion decision, status, amount provenance, and date provenance. Settlement links it to a posted transaction and removes the unfulfilled projection effect.

For cards, exactly one active `ScheduledCashFlow` represents the total planned payment for one statement. An MSI allocation never creates an additional installment-level cash outflow for principal already included in that statement payment.

### Receivable

A claim for money owed to the user. It records amount, source, certainty, status, and an optional expected date. A receivable is neither cash nor income in a deterministic projection merely because its amount is known. An undated receivable stays visible outside the timeline.

### AccountSelection

An immutable value object identifying the liquid-eligible account IDs selected to participate in consolidated liquidity. Eligibility comes from account type and attributes; selection comes from policy. Both are required. A selected liability or otherwise ineligible account contributes nothing and produces a validation error.

### ProjectionPolicy

An immutable value object owned through user financial settings and snapshotted into every projection. It defines:

- Projection horizon.
- Required cash reserve.
- Income inclusion policy.
- Treatment of uncertain inflows.
- Default card payment strategy.
- Same-day ordering rule.
- `AccountSelection`.
- Maximum acceptable age for available-credit sources.
- User timezone.

It is a value object because it has no useful independent lifecycle: its meaning is its complete set of values. A saved version or complete snapshot makes results reproducible.

### ProjectionScenario

A baseline or what-if request combining a policy snapshot with overrides, hypothetical purchases, and explicit assumptions. An uncertain, undated inflow can enter a scenario only when the scenario both includes it and supplies an assumed date. Doing so does not make it confirmed: the original certainty is preserved, and the explanation trace records that it was uncertain, explicitly included, and assigned an assumed date. A scenario changes projected truth, not actual account state.

### Projected liquid cash

Consolidated liquid cash evolved through included dated cash flows. The value is signed and may become negative to expose a deficit. It is never clamped to zero. For financial date `d`:

```text
projected_liquid_cash(d) =
    starting_consolidated_liquid_cash
    + included_inflows_through(d)
    - included_outflows_through(d)
```

Internal transfers between included liquid accounts have zero consolidated effect.

### Safe-to-spend

The greatest non-negative hypothetical purchase amount, in integer minor units, that preserves the funding account's economic behavior, respects its constraints, and keeps projected liquid cash at or above the required reserve on every date in the effective evaluation horizon.

The normal dashboard horizon remains configurable. For a candidate purchase:

```text
effective_horizon_end =
    max(normal_horizon_end, candidate_final_deterministic_cash_effect_date)
```

All recurring obligations and other relevant cash flows are expanded through that effective horizon. For MSI, the final cash effect is the final deterministically scheduled statement payment containing candidate principal. If the candidate's final payment amount or date cannot be determined, the result is indeterminate with warnings. A payment falling outside the dashboard horizon is never grounds to certify card spending as safe.

For funding account `f`, candidate amount `x`, baseline `B(d)`, candidate cash impact `I(f,x,d)`, and reserve `R`, the candidate is safe only if:

```text
B(d) + I(f,x,d) >= R  for every date d in the effective horizon
```

A cash/debit purchase is feasible only when its funding account is liquidity-eligible, selected by policy, and can execute the purchase from its own spendable balance. Consolidated cash held elsewhere cannot make it feasible unless an overdraft or prerequisite transfer is explicitly modeled. A card purchase must also be no greater than a sufficiently reliable available-credit bound.

If the baseline already breaches the reserve, the result is:

```text
safe_to_spend = 0
status = already_below_reserve
```

It also returns the earliest breach date, deficit amount, and explanation trace. Safe-to-spend is never negative. Missing critical funding, available-credit, statement, timing, or payment data produces warnings and an indeterminate result when no safe bound can be established.

### Exact vs estimated financial data

Exactness describes provenance, not whether money is certain to arrive. Amount provenance and date provenance are recorded separately, allowing all four combinations:

- Exact amount / exact date.
- Exact amount / estimated date.
- Estimated amount / exact date.
- Estimated amount / estimated date.

Each provenance record identifies its authoritative or derivation source. Posted transactions, reconciled snapshots, and issued statement fields are exact for fields supplied by their authoritative source. Pre-statement values and values derived from incomplete recurrence information are estimated in the affected dimension. If an aggregate label is needed, an event is estimated when any calculation-critical field is estimated. Exact data supersedes matching estimates by logical source identity; it is not added alongside them.

### Confirmed vs uncertain inflows

Certainty describes whether an inflow should be relied upon and is independent of amount/date provenance. Confirmed payroll with a reliable date may enter the default projection. An uncertain inflow is excluded by default even when its amount and date are exact. An undated inflow cannot enter a deterministic projection unless a scenario provides an assumed date; scenario inclusion never changes its uncertain classification.

## Concepts that are not interchangeable

| Concept | What it answers | Must not be treated as |
|---|---|---|
| Current balance | What is held or owed in one account now? | Consolidated liquidity or forecast |
| Consolidated liquidity | How much included cash is usable now? | Credit capacity, debt, or safe-to-spend |
| Available credit | How much more may be borrowed on a card? | Cash, income, or liquidity |
| Debt | How much is currently owed? | A cash balance or future payment total |
| Projected balance | What will liquid cash be on a future date under a policy? | Current balance or guaranteed outcome |
| Safe-to-spend | What purchase amount is feasible through a specified account? | Balance, credit limit, or a universal account-independent value |

## Credit-card cash-flow lineage

There is one authoritative path from card activity to projected cash:

```text
InstallmentPlan
    -> allocates existing principal to statement cycles
CreditCardStatement
    -> aggregates regular purchases, MSI allocations, fees, interest,
       taxes, and other statement components
PaymentIntent
    -> selects the payment strategy for one statement
ScheduledCashFlow
    -> represents exactly one projected statement-level payment outflow
Posted payment
    -> settles that projected statement payment
```

Before issuance, a provisional statement and its provisional payment use the same logical card-and-cycle identity as the future issued statement. Issuance replaces the estimate and recomputes the single statement payment; it does not append another statement or cash flow. A posted payment settles that one projected outflow through source identity.

An installment allocation can contribute principal to a statement, but it cannot independently create a cash outflow. Creating both an installment-level outflow and a statement-level outflow for the same principal is forbidden.

## Economic behavior

### 1. Cash/debit purchase

A posted purchase decreases the funding asset account and consolidated liquid cash on the purchase date. It creates no card liability.

### 2. Regular credit-card purchase

The full purchase amount increases card liability and decreases locally projected available credit on the purchase date. Liquid cash is unchanged. Assignment to a statement does not recreate liability. The statement's single `PaymentIntent` generates the later statement-level cash effect.

### 3. Credit-card payment

Payment decreases the funding liquid account and consolidated liquid cash. It decreases total card liability by its posted account allocation and may restore available credit according to issuer-reported behavior. Its component allocation may include MSI principal, revolving principal, fees, interest, taxes, and other components.

MSI outstanding principal decreases only by an explicit principal component linked to the relevant `InstallmentPlan`. If that allocation is unavailable, the total card payment may still post, but MSI principal reconciliation remains unresolved and produces a warning; Runway never infers that the entire payment reduced an MSI plan. Linked source and liability effects represent one payment, not an expense plus a transfer.

### 4. Internal transfer

A transfer between included liquid accounts decreases one account and increases another by the same amount. It is atomic for consolidated projection and has zero effect on consolidated liquid cash.

### 5. 0% MSI purchase

The entire principal increases card liability once at purchase and decreases locally projected available credit by the same amount. Cash is unchanged. The installment plan allocates principal to future statement cycles only; it adds no interest, fees, recurring principal, or independent installment-level cash flow.

### 6. MSI installment payment

An installment becoming due does not create liability or a separate cash outflow. Its principal allocation contributes to the statement total. The active `PaymentIntent` creates one projected statement-level cash flow. A posted statement payment decreases liquid cash; MSI outstanding principal decreases only by the explicit component allocated to that plan. Principal cannot be reduced below zero.

### 7. Confirmed payroll

Before posting, confirmed payroll with a reliable date is an included scheduled inflow under the default policy. When posted, it increases the receiving asset account and is linked to/supersedes the scheduled occurrence so it is counted once.

### 8. Undated receivable

The claim, amount, and uncertainty classification are preserved, but it creates no scheduled inflow and contributes nothing to projected liquid cash or safe-to-spend. A scenario must explicitly include it and assign an assumed date before it can affect a projection. The trace continues to label it uncertain and records the scenario inclusion and assumed date.

### 9. Recurring obligation

The obligation deterministically generates occurrences inside the applicable normal or effective horizon. Each occurrence is a scheduled outflow until settled, cancelled, or superseded. Posted settlement replaces rather than duplicates the scheduled effect.

## Worked projection example

Assume today is August 9, the effective horizon includes September 10, the reserve is 0 MXN, and payroll is confirmed. All baseline event amounts and dates are exact. The hypothetical 500 MXN card purchase amount and purchase date are exact, but its unissued August 20 statement assignment and resulting September 10 payment event are provisional. For that payment event, the incremental 500 MXN amount is exact under the stated interest-saving strategy and no-fee assumption, while the date has estimated provenance until statement issuance. The card has a reliable available-credit bound of at least 500 MXN.

| Date and event | Baseline | 500 MXN debit today | 500 MXN card today |
|---|---:|---:|---:|
| Aug 9: starting cash / purchase | 2,600 | 2,100 | 2,600 |
| Aug 14: -1,413 | 1,187 | 687 | 1,187 |
| Aug 15: +7,887 payroll | 9,074 | 8,574 | 9,074 |
| Aug 16: -2,264 car payment | 6,810 | 6,310 | 6,810 |
| Aug 17: -2,508 card payment | 4,302 | 3,802 | 4,302 |
| Aug 27: -1,346 card payment | 2,956 | 2,456 | 2,956 |
| Sep 10: hypothetical card payment | 2,956 | 2,456 | 2,456 |

The baseline minimum is 1,187 MXN. The debit purchase is executable from its selected funding account, reduces cash immediately, and lowers the minimum to 687 MXN. The card purchase instead increases debt and reduces available credit by 500 MXN on August 9; its provisional cash effect occurs September 10, so the projection minimum remains 1,187 MXN. The issued statement supersedes the provisional cycle and payment event using the same logical identity. With these assumptions, both 500 MXN candidates are safe, but for different reasons and with different liability and timing effects.
