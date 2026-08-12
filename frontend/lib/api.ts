export type User = { id: string; email: string };

export type AccountType = "cash" | "bank" | "credit_card" | "loan";
export type AccountStatus = "active" | "archived";
export type Account = {
  id: string;
  name: string;
  type: AccountType;
  currency: string;
  status: AccountStatus;
  created_at: string;
  updated_at: string;
};

export type Balance = {
  account_id: string;
  balance_minor: number;
  currency: string;
  snapshot_id?: string;
  snapshot_cutoff_sequence: number;
  last_applied_sequence: number;
  applied_transactions: number;
};

export type TransactionEffect = "asset_inflow" | "asset_outflow" | "liability_charge" | "liability_payment";
export type Transaction = {
  id: string;
  account_id: string;
  kind: string;
  effect: TransactionEffect;
  amount_minor: number;
  currency: string;
  financial_date: string;
  ledger_sequence: number;
  memo: string;
  transfer_id?: string;
  created_at: string;
};

export type ProjectionEvent = {
  id: string;
  source_kind: string;
  source_id: string;
  financial_date: string;
  amount_minor: number;
  direction: "inflow" | "outflow";
  amount_provenance: string;
  date_provenance: string;
  inclusion_basis: string;
  source_certainty?: string;
  label?: string;
  balance_before_minor: number;
  balance_after_minor: number;
};

export type ProjectionExclusion = {
  source_kind: string;
  source_id: string;
  reasons: string[];
  label?: string;
};

export type Projection = {
  as_of: string;
  horizon_end: string;
  currency: string;
  policy_id: string;
  policy_version: number;
  reserve_minor: number;
  selected_account_ids: string[];
  opening_liquid_balance_minor: number;
  events: ProjectionEvent[];
  closing_projected_balance_minor: number;
  minimum_projected_balance_minor: number;
  minimum_event_id?: string;
  minimum_date?: string;
  exclusions: ProjectionExclusion[];
	completeness?: "complete" | "indeterminate";
	issues?: ProjectionIssue[];
};
export type ProjectionIssue = { code: string; cycle_id: string; payment_intent_id?: string };

export type SafeToSpendStatus =
  | "safe"
  | "constrained_by_future_cash_flow"
  | "constrained_by_funding_balance"
  | "already_below_reserve"
  | "unsupported_funding_type"
  | "indeterminate";

export type SafeToSpend = {
  as_of: string;
  currency: string;
  funding_account_id: string;
  funding_account_balance_minor: number;
  reserve_minor: number;
  opening_liquid_balance_minor: number;
  baseline_minimum_balance_minor: number;
  safe_to_spend_minor: number;
  status: SafeToSpendStatus;
  limiting_event_id?: string;
  limiting_date?: string;
  earliest_breach_event_id?: string;
  earliest_breach_date?: string;
  deficit_minor: number;
  policy_id: string;
  policy_version: number;
  baseline_projection: Projection;
	indeterminate_reason?: string;
};

export type CreditCardStatement = {
  id: string; account_id: string; cycle_id: string; revision: number;
  authority: "estimated" | "issued"; statement_balance_minor: number; currency: string;
  minimum_payment_minor?: number; payment_to_avoid_interest_minor?: number;
  due_date: string; created_at: string; superseded_at?: string; superseded_by_id?: string;
};
export type PaymentIntent = { id: string; account_id: string; cycle_id: string; amount_minor: number; currency: string; planned_date: string; status: "active" | "needs_review" | "cancelled" | "settled"; version: number; created_at: string; updated_at: string };
// GET payment-intent is an authoritative backend read model. Settlement totals
// are supplied by the API instead of derived in the browser.
export type PaymentIntentSummary = PaymentIntent & { settled_amount_minor: number; remaining_amount_minor: number };
export type InstallmentPlan = { id: string; account_id: string; description: string; purchase_transaction_id: string; original_principal_minor: number; currency: string; installment_count: number; first_cycle_id: string; status: string; schedule_version: number; created_at: string; updated_at: string; paid_principal_minor?: number; outstanding_principal_minor?: number };
export type InstallmentAllocation = { id: string; plan_id: string; cycle_id: string; installment_number: number; schedule_version: number; principal_minor: number; currency: string; status: string; created_at: string };

export type AccountSelection = { mode: "all_active_liquid" | "explicit"; account_ids: string[] };
export type ProjectionPolicy = {
  id: string;
  currency: string;
  horizon_days: number;
  reserve_minor: number;
  financial_timezone: string;
  account_selection: AccountSelection;
  inflow_policy: "confirmed_only" | "include_expected";
  same_day_order: "outflows_before_inflows";
  version: number;
  created_at: string;
  updated_at: string;
};

export type ApiErrorShape = { error?: string };

export class ApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

export class NetworkError extends Error {
  constructor() {
    super("Network request failed. You can safely retry this pending action.");
    this.name = "NetworkError";
  }
}

type RequestOptions = Omit<RequestInit, "body"> & { body?: unknown; idempotencyKey?: string };

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  if (options.body !== undefined) headers.set("Content-Type", "application/json");
  if (options.idempotencyKey) headers.set("Idempotency-Key", options.idempotencyKey);

  let response: Response;
  try {
    response = await fetch(path, {
      ...options,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      credentials: "include",
    });
  } catch {
    throw new NetworkError();
  }

  if (response.status === 204) return undefined as T;
  const payload = (await response.json().catch(() => ({}))) as T & ApiErrorShape;
  if (!response.ok) throw new ApiError(response.status, payload.error ?? "Request failed.");
  return payload;
}

export const api = {
  me: () => request<User>("/api/v1/auth/me"),
  login: (email: string, password: string) => request<User>("/api/v1/auth/login", { method: "POST", body: { email, password } }),
  logout: () => request<void>("/api/v1/auth/logout", { method: "POST" }),
  listAccounts: () => request<Account[]>("/api/v1/accounts"),
  createAccount: (input: Pick<Account, "name" | "type" | "currency">) => request<Account>("/api/v1/accounts", { method: "POST", body: input }),
  archiveAccount: (id: string) => request<Account>(`/api/v1/accounts/${id}/archive`, { method: "POST" }),
  balance: (id: string) => request<Balance>(`/api/v1/accounts/${id}/balance`),
  transactions: (id: string) => request<Transaction[]>(`/api/v1/accounts/${id}/transactions`),
  postTransaction: (id: string, input: Omit<Transaction, "id" | "account_id" | "kind" | "ledger_sequence" | "created_at" | "transfer_id">, idempotencyKey: string) =>
    request<Transaction>(`/api/v1/accounts/${id}/transactions`, { method: "POST", body: input, idempotencyKey }),
  transfer: (input: { source_account_id: string; destination_account_id: string; amount_minor: number; currency: string; financial_date: string; memo: string }, idempotencyKey: string) =>
    request<unknown>("/api/v1/transfers", { method: "POST", body: input, idempotencyKey }),
  policy: () => request<ProjectionPolicy>("/api/v1/projection-policy"),
  savePolicy: (input: Omit<ProjectionPolicy, "id" | "version" | "created_at" | "updated_at">) =>
    request<ProjectionPolicy>("/api/v1/projection-policy", { method: "PUT", body: input }),
  projection: () => request<Projection>("/api/v1/projection"),
  safeToSpend: (fundingAccountID: string) => request<SafeToSpend>(`/api/v1/safe-to-spend?funding_account_id=${encodeURIComponent(fundingAccountID)}`),
	cardStatements: (accountID: string) => request<CreditCardStatement[]>(`/api/v1/credit-cards/${accountID}/statements`),
	registerStatement: (accountID: string, input: { cycle_start: string; cycle_end: string; authority: "estimated" | "issued"; statement_balance_minor: number; minimum_payment_minor?: number; payment_to_avoid_interest_minor?: number; currency: string; due_date: string }, key: string) => request<CreditCardStatement>(`/api/v1/credit-cards/${accountID}/statements`, { method: "POST", body: input, idempotencyKey: key }),
	paymentIntent: (cycleID: string) => request<PaymentIntentSummary>(`/api/v1/credit-card-cycles/${cycleID}/payment-intent`),
	replacePaymentIntent: (cycleID: string, input: { amount_minor: number; currency: string; planned_date: string }) => request<PaymentIntent>(`/api/v1/credit-card-cycles/${cycleID}/payment-intent`, { method: "PUT", body: input }),
	cancelPaymentIntent: (cycleID: string) => request<PaymentIntent>(`/api/v1/credit-card-cycles/${cycleID}/payment-intent/cancel`, { method: "POST" }),
	settlePaymentIntent: (intentID: string, transferID: string, key: string) => request<{ id: string; intent: PaymentIntent }>(`/api/v1/credit-card-payment-intents/${intentID}/settlements`, { method: "POST", body: { transfer_id: transferID }, idempotencyKey: key }),
	installmentPlans: (accountID: string) => request<InstallmentPlan[]>(`/api/v1/credit-cards/${accountID}/installment-plans`),
	createInstallmentPlan: (accountID: string, input: { description: string; purchase_transaction_id: string; original_principal_minor: number; currency: string; installment_count: number; first_cycle_start: string; first_cycle_end: string }, key: string) => request<InstallmentPlan>(`/api/v1/credit-cards/${accountID}/installment-plans`, { method: "POST", body: input, idempotencyKey: key }),
	installmentPlan: (planID: string) => request<InstallmentPlan>(`/api/v1/installment-plans/${planID}`),
	installmentAllocations: (planID: string) => request<InstallmentAllocation[]>(`/api/v1/installment-plans/${planID}/allocations`),
};
