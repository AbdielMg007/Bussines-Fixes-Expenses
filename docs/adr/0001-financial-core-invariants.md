# ADR 0001: Financial Core Invariants

- Status: Accepted
- Date: 2026-08-10

## Context

Runway must determine projected liquidity and funding-account-specific safe-to-spend without confusing cash, borrowing capacity, or debt. Small modeling errors can produce unsafe recommendations. These rules therefore constrain the domain model, persistence, API, projection engine, imports, and future AI interface.

Terminology and the worked acceptance example are defined in [`financial-domain.md`](../architecture/financial-domain.md).

## Decision

Financial truth is produced only by deterministic application code. Calculations operate on validated domain data, a complete `ProjectionPolicy` snapshot, and an explicit as-of date. Results include their inputs, warnings, exactness, and an explanation trace sufficient to reconstruct every balance change.

The following invariants are non-negotiable.

### Money and currency

1. A monetary amount or magnitude is represented as non-negative integer minor units paired with an explicit currency.
2. Account balances and projected balances are signed integer minor units. Negative projected balances expose deficits and must never be clamped to zero.
3. Liability principal and the required cash reserve are non-negative integer minor units.
4. `float32` and `float64` must never be used for monetary storage, arithmetic, comparison, allocation, serialization, or tests.
5. Amounts of different currencies cannot be added or compared without an explicit conversion operation. The initial domain supports MXN only.
6. Rounding is explicit and deterministic; no minor unit may be lost or invented.

### Accounts, liquidity, and debt

7. Asset and liability account semantics are explicit. Liability balances are non-negative amounts owed; negative asset balances remain visible unless an overdraft facility is explicitly modeled.
8. `AccountSelection` is part of `ProjectionPolicy`. Consolidated liquid cash includes an account only when its type and attributes make it liquidity-eligible and its ID is selected by policy.
9. A cash/debit hypothetical purchase must be executable from the selected funding account's own spendable balance. Cash in other accounts cannot make the purchase feasible unless an overdraft or prerequisite transfer is explicitly modeled.
10. Available credit is never liquidity, cash, income, or an asset. It may only constrain card purchasing capacity.
11. Total debt is distinct from current cash and future scheduled payments.
12. A balance snapshot is an as-of observation, not a transaction or forecast. It contains an exact ledger cutoff/cursor.
13. Current balance reconstruction applies only transaction effects whose cursor is strictly after the snapshot cutoff. A cursor or stable sequence resolves equal timestamps, and each transaction contributes exactly once.

### Credit cards and statements

14. A credit-card purchase increases liability by its full principal immediately on the purchase date.
15. A credit-card purchase does not reduce liquid cash before a corresponding payment event.
16. Available credit preserves issuer-reported and locally calculated values separately, including provenance and observation time.
17. An available-credit source is eligible only when it meets the policy's maximum age and has complete inputs. If both sources are eligible, the lower value is the safe bound. If one is eligible, it is used and the missing or stale alternative produces a warning. A stale value cannot be the sole safe bound. If neither is eligible, card-funded safe-to-spend is indeterminate.
18. Assigning a regular purchase or MSI allocation to a statement does not create principal.
19. One logical card-and-cycle identity connects a provisional statement estimate to its future issued statement. Issuance supersedes the estimate; both must never be active or affect projection simultaneously.
20. A card statement aggregates regular purchases, MSI principal allocations, fees, interest, taxes, and other components.
21. Exactly one active `PaymentIntent` exists for one statement and generates exactly one statement-level payment `ScheduledCashFlow`.
22. An MSI allocation must never generate an additional installment-level cash outflow for principal already represented in the statement payment.
23. A posted card payment settles the one projected statement payment through source identity. The scheduled and posted effects are counted once.
24. A `PaymentIntent` is a plan, not a posted balance change. Only its scheduled statement payment affects projected cash, and only a posted payment affects actual balances.
25. Missing critical available-credit, statement, due-date, final-payment, or payment-strategy data must produce a warning when a safe conservative bound exists, or an indeterminate result when future cash timing or amount cannot be bounded. The engine must not silently assume favorable values.

### MSI principal

26. Creating an installment plan cannot increase liability beyond the original purchase principal.
27. A 6,000 MXN purchase at 6 MSI creates exactly 6,000 MXN of principal liability once, at purchase. The installments allocate repayment of that existing principal.
28. Statement allocation and an installment becoming due do not create principal.
29. MSI state always satisfies:

```text
original_principal = paid_principal + outstanding_principal
sum(active_unpaid_principal_allocations) = outstanding_principal
```

30. Only one allocation schedule version may be active. A replacement, correction, or issuer-provided schedule supersedes its predecessor rather than appending to it.
31. Only a posted payment with an explicit MSI principal component linked to the relevant `InstallmentPlan` reduces its actual outstanding principal. Principal can never become negative.
32. A mixed card payment may allocate value to MSI principal, revolving principal, fees, interest, taxes, or other components. Runway must never infer that the entire payment reduced an MSI plan.
33. If explicit MSI principal allocation is unavailable, the card payment may post against total card liability, but MSI reconciliation remains unresolved and produces a warning; the plan's principal is not silently reduced.
34. For 0% MSI, no interest, fee, or additional liability is generated beyond original principal.
35. MSI rounding must allocate every minor unit exactly once:

```text
principal = quotient * installment_count + remainder
sum(installment_principal) = principal
```

For estimates, the earliest `remainder` installments receive one additional minor unit. Issuer-provided exact allocations supersede estimates without changing total original principal. Active principal allocations must never exceed or fall short of outstanding principal.

### Transfers, schedules, and settlement

36. A linked transfer between included liquid accounts does not change consolidated liquid cash.
37. A card payment decreases consolidated liquid cash because value leaves a liquid asset account to reduce a liability.
38. A future `ScheduledCashFlow` and its posted settlement must be linked and counted once, never as two economic events.
39. Recurring obligations generate only occurrences inside the requested horizon. Cancelling or settling an occurrence removes its outstanding projected effect.

### Inflows and projection

40. Undated inflows never enter deterministic projections.
41. Uncertain inflows are excluded by default, even when their amount is known.
42. A scenario may include an uncertain inflow only explicitly; it must provide an assumed financial date when none exists. Inclusion never promotes it to confirmed. The trace records its original uncertainty, explicit scenario inclusion, and assumed date.
43. Amount/date provenance and certainty are independent. An exact receivable amount may still be uncertain and excluded.
44. Amount provenance and date provenance are recorded separately, supporting exact/exact, exact/estimated, estimated/exact, and estimated/estimated combinations. When any calculation-critical field is estimated, an aggregate event status is estimated.
45. Projected liquid cash begins with consolidated liquid cash and changes only through included explanation-trace events. It remains signed and is never clamped to zero.
46. Every projected balance change must have exactly one explanation-trace event identifying its source, logical identity, amount, date, direction, inclusion basis, amount/date provenance, and certainty where applicable.
47. Safe-to-spend is evaluated for a specified funding account. It must preserve that account's economic timing, remain above the policy reserve throughout the effective horizon, and obey local funding constraints and sufficiently reliable available credit.
48. The effective safe-to-spend horizon ends at the later of the normal policy horizon end date and the candidate's final deterministic cash-effect date. All other recurring obligations and relevant cash flows are expanded through it. If the candidate's final payment amount or date cannot be determined, the result is indeterminate; an out-of-dashboard payment must never be ignored.
49. If baseline projected liquid cash is already below reserve, `safe_to_spend` is zero with status `already_below_reserve`, earliest breach date, deficit amount, and explanation trace. Safe-to-spend is never negative.

### AI boundary

50. AI never calculates or establishes financial truth, including balances, liabilities, statement payments, installment allocations, projected cash, or safe-to-spend.
51. AI output is untrusted candidate data. Deterministic schema validation, domain validation, and required user confirmation occur before persistence or calculation.

## Same-day ordering

Financial dates do not by themselves establish intraday timing. The default order is:

1. Opening balance for the date.
2. Outflows.
3. Inflows.
4. Closing balance for the date.

Outflows therefore occur before inflows on the same financial date unless an explicit, authoritative order exists, such as posted timestamps or a stored sequence. Events in the same class use a stable deterministic tie-breaker. The explanation trace records the applied order.

This rule is intentionally conservative: it prevents Runway from claiming that an obligation is covered by income that may arrive later that day. It also exposes possible intraday reserve breaches that an end-of-day net calculation would hide. A linked transfer between included liquid accounts is atomic for consolidated liquidity and must not create an artificial interim deficit.

## Consequences

- The projection engine can be tested without a database, HTTP server, frontend, or AI provider.
- Card-funded and cash-funded safe-to-spend values may differ legitimately.
- Projections may be indeterminate when required card information is absent; displaying a confident but invented number is forbidden.
- Reconciliation requires stable source identity so estimates, scheduled events, statements, and posted settlements do not double-count.
- Future support for interest-bearing installments must model principal, interest, and fees as separate components and requires another explicit decision.
- Code review can reject any implementation that violates an invariant even when its sample totals appear correct.
