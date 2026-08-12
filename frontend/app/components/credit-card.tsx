"use client";

import { FormEvent, useCallback, useEffect, useRef, useState } from "react";

import {
  ApiError,
  api,
  type Account,
  type CreditCardStatement,
  type InstallmentAllocation,
  type InstallmentPlan,
  type PaymentIntent,
  type PaymentIntentSummary,
  type Transaction,
} from "../../lib/api";
import { formatFinancialDate, formatMoney, parseMinorUnits } from "../../lib/format";
import { t, type Language } from "../../lib/i18n";
import { useIdempotentMutation } from "../hooks/use-idempotent-mutation";

type CreditCardDetailProps = {
  account: Account;
  transactions: Transaction[];
  refreshToken: number;
  language: Language;
  onChanged: () => void;
  onUnauthorized: () => void;
};

export function CreditCardDetail({ account, transactions, refreshToken, language, onChanged, onUnauthorized }: CreditCardDetailProps) {
  const [statements, setStatements] = useState<CreditCardStatement[]>([]);
  const [plans, setPlans] = useState<InstallmentPlan[]>([]);
  const [intents, setIntents] = useState<Record<string, PaymentIntentSummary | null>>({});
  const [error, setError] = useState("");

  const reload = useCallback(async () => {
    try {
      const [nextStatements, nextPlans] = await Promise.all([
        api.cardStatements(account.id),
        api.installmentPlans(account.id),
      ]);
      const intentPairs = await Promise.all(nextStatements.map(async (statement) => {
        try {
          return [statement.cycle_id, await api.paymentIntent(statement.cycle_id)] as const;
        } catch (cause) {
          if (cause instanceof ApiError && cause.status === 404) return [statement.cycle_id, null] as const;
          throw cause;
        }
      }));
      setStatements(nextStatements);
      setPlans(nextPlans);
      setIntents(Object.fromEntries(intentPairs));
      setError("");
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(t(language, "unableToLoadCard"));
    }
  }, [account.id, language, onUnauthorized]);

  useEffect(() => { void reload(); }, [reload, refreshToken]);
  const changed = () => { onChanged(); void reload(); };

  return <section className="credit-card-detail" aria-label={t(language, "cardManagement")}>
    <div className="section-heading">
      <div><p className="eyebrow">{t(language, "creditCard")}</p><h2>{t(language, "cardManagement")}</h2></div>
    </div>
    {error && <p className="form-error">{error}</p>}
    {account.status === "active" && <div className="card-grid">
      <StatementForm account={account} language={language} onChanged={changed} onUnauthorized={onUnauthorized} />
      <MsiForm account={account} transactions={transactions} language={language} onChanged={changed} onUnauthorized={onUnauthorized} />
    </div>}
    <section className="panel card-section">
      <h3>{t(language, "statements")}</h3>
      {statements.length === 0
        ? <p className="empty">{t(language, "noStatements")}</p>
        : statements.map((statement) => <StatementRow key={statement.id} statement={statement} intent={intents[statement.cycle_id]} account={account} transactions={transactions} language={language} onChanged={changed} onUnauthorized={onUnauthorized} />)}
    </section>
    <MsiPlans plans={plans} language={language} onUnauthorized={onUnauthorized} />
  </section>;
}

function StatementForm({ account, language, onChanged, onUnauthorized }: Pick<CreditCardDetailProps, "account" | "language" | "onChanged" | "onUnauthorized">) {
  type Input = Parameters<typeof api.registerStatement>[1];
  const mutation = useIdempotentMutation<Input, CreditCardStatement>((input, key) => api.registerStatement(account.id, input, key));
  const [error, setError] = useState("");
  const submitting = useRef(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current) return;
    submitting.current = true;
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      setError("");
      await mutation.submit({
        cycle_start: String(data.get("cycle_start")),
        cycle_end: String(data.get("cycle_end")),
        authority: String(data.get("authority")) as "estimated" | "issued",
        statement_balance_minor: parseMinorUnits(String(data.get("statement_balance")), language),
        minimum_payment_minor: data.get("minimum_payment") ? parseMinorUnits(String(data.get("minimum_payment")), language) : undefined,
        payment_to_avoid_interest_minor: data.get("ppngi") ? parseMinorUnits(String(data.get("ppngi")), language) : undefined,
        currency: account.currency,
        due_date: String(data.get("due_date")),
      });
      form.reset();
      onChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(cause instanceof Error && !(cause instanceof ApiError) ? cause.message : t(language, "unableToSaveStatement"));
    } finally {
      submitting.current = false;
    }
  }

  return <form className="panel compact-form" onSubmit={submit}>
    <p className="eyebrow">{t(language, "registerStatement")}</p>
    <label>{t(language, "cycleStart")}<input name="cycle_start" type="date" required /></label>
    <label>{t(language, "cycleEnd")}<input name="cycle_end" type="date" required /></label>
    <label>{t(language, "statementAuthority")}<select name="authority" defaultValue="issued"><option value="estimated">{t(language, "estimated")}</option><option value="issued">{t(language, "issued")}</option></select></label>
    <label>{t(language, "statementBalance")}<input name="statement_balance" inputMode="decimal" required /></label>
    <label>{t(language, "minimumPayment")}<input name="minimum_payment" inputMode="decimal" /></label>
    <label>{t(language, "ppngi")}<input name="ppngi" inputMode="decimal" /></label>
    <label>{t(language, "dueDate")}<input name="due_date" type="date" required /></label>
    {error && <p className="form-error">{error}</p>}
    {mutation.hasRetry && <button type="button" className="subtle-button" onClick={() => void mutation.retry()} disabled={mutation.isSubmitting}>{t(language, "retryPending")}</button>}
    <button disabled={mutation.isSubmitting || mutation.hasRetry}>{mutation.isSubmitting ? t(language, "saving") : mutation.hasRetry ? t(language, "retryPendingFirst") : t(language, "registerStatement")}</button>
  </form>;
}

function StatementRow({ statement, intent, account, transactions, language, onChanged, onUnauthorized }: {
  statement: CreditCardStatement; intent: PaymentIntentSummary | null | undefined; account: Account; transactions: Transaction[]; language: Language; onChanged: () => void; onUnauthorized: () => void;
}) {
  const authoritative = !statement.superseded_at;
  return <article className={`statement-row ${authoritative ? "current" : "superseded"}`}>
    <div>
      <strong>{formatFinancialDate(statement.due_date, language)} · {t(language, statement.authority === "issued" ? "issued" : "estimated")}</strong>
      <span>{t(language, "cycle")}: {statement.cycle_id} · {t(language, "revision")} {statement.revision}{!authoritative && ` · ${t(language, "superseded")}`}</span>
      <b>{formatMoney(statement.statement_balance_minor, statement.currency, language)}</b>
      {statement.minimum_payment_minor !== undefined && <small>{t(language, "minimumPayment")}: {formatMoney(statement.minimum_payment_minor, statement.currency, language)}</small>}
      {statement.payment_to_avoid_interest_minor !== undefined && <small>{t(language, "ppngi")}: {formatMoney(statement.payment_to_avoid_interest_minor, statement.currency, language)}</small>}
    </div>
    {authoritative && <IntentPanel account={account} cycleID={statement.cycle_id} intent={intent ?? null} transactions={transactions} language={language} onChanged={onChanged} onUnauthorized={onUnauthorized} />}
  </article>;
}

function IntentPanel({ account, cycleID, intent, transactions, language, onChanged, onUnauthorized }: {
  account: Account; cycleID: string; intent: PaymentIntentSummary | null; transactions: Transaction[]; language: Language; onChanged: () => void; onUnauthorized: () => void;
}) {
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const savingRef = useRef(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (savingRef.current) return;
    savingRef.current = true;
    setSaving(true);
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      setError("");
      await api.replacePaymentIntent(cycleID, {
        amount_minor: parseMinorUnits(String(data.get("amount")), language),
        currency: account.currency,
        planned_date: String(data.get("planned_date")),
      });
      form.reset();
      onChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(cause instanceof Error && !(cause instanceof ApiError) ? cause.message : t(language, "unableToSaveIntent"));
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  }

  async function cancel() {
    if (!intent || !window.confirm(t(language, "cancelIntentConfirm")) || savingRef.current) return;
    savingRef.current = true;
    setSaving(true);
    try {
      setError("");
      await api.cancelPaymentIntent(cycleID);
      onChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(t(language, "unableToSaveIntent"));
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  }

  const canEdit = account.status === "active" && intent?.status !== "cancelled" && intent?.status !== "settled";
  return <div className="intent-panel">
    <h4>{t(language, "paymentIntent")}</h4>
    {intent ? <>
      <p><b>{t(language, intent.status === "needs_review" ? "needsReview" : intent.status)}</b> · {formatMoney(intent.amount_minor, intent.currency, language)} · {formatFinancialDate(intent.planned_date, language)}</p>
      <IntentAmounts intent={intent} language={language} />
      {intent.status === "settled" && intent.remaining_amount_minor === 0 && <p className="muted"><b>{t(language, "fullySettled")}</b> · {t(language, "noRemainingPlannedPayment")}</p>}
      {intent.status === "needs_review" && <p className="warning">{t(language, "intentNeedsReviewWarning")}</p>}
      {intent.status === "active" && <SettlementForm intent={intent} transactions={transactions} language={language} onChanged={onChanged} onUnauthorized={onUnauthorized} />}
      {canEdit && <button className="subtle-button" disabled={saving} onClick={() => void cancel()}>{t(language, "cancel")}</button>}
    </> : <p className="empty">{t(language, "noPaymentIntent")}</p>}
    {canEdit || (account.status === "active" && intent === null) ? <form className="inline-form" onSubmit={submit}>
      <label>{t(language, "plannedPayment")}<input name="amount" required inputMode="decimal" /></label>
      <label>{t(language, "plannedDate")}<input name="planned_date" required type="date" /></label>
      <button disabled={saving}>{saving ? t(language, "saving") : t(language, "save")}</button>
    </form> : null}
    {error && <p className="form-error">{error}</p>}
  </div>;
}

function IntentAmounts({ intent, language }: { intent: PaymentIntentSummary; language: Language }) {
  return <dl className="intent-amounts">
    <div><dt>{t(language, "intendedAmount")}</dt><dd>{formatMoney(intent.amount_minor, intent.currency, language)}</dd></div>
    <div><dt>{t(language, "explicitlySettledAmount")}</dt><dd>{formatMoney(intent.settled_amount_minor, intent.currency, language)}</dd></div>
    <div><dt>{t(language, "remainingFutureAmount")}</dt><dd>{formatMoney(intent.remaining_amount_minor, intent.currency, language)}</dd></div>
  </dl>;
}

function SettlementForm({ intent, transactions, language, onChanged, onUnauthorized }: {
  intent: PaymentIntent; transactions: Transaction[]; language: Language; onChanged: () => void; onUnauthorized: () => void;
}) {
  // These are posted liability payments to this exact card account. The user
  // explicitly chooses one; no generic transfer is ever auto-matched.
  const candidates = transactions.filter((transaction) => transaction.transfer_id && transaction.effect === "liability_payment");
  type Input = { transfer_id: string };
  const mutation = useIdempotentMutation<Input, unknown>((input, key) => api.settlePaymentIntent(intent.id, input.transfer_id, key));
  const [error, setError] = useState("");
  const submitting = useRef(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current) return;
    submitting.current = true;
    const form = event.currentTarget;
    try {
      setError("");
      await mutation.submit({ transfer_id: String(new FormData(form).get("transfer_id")) });
      form.reset();
      onChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(t(language, "unableToSettleIntent"));
    } finally {
      submitting.current = false;
    }
  }

  if (candidates.length === 0) return <p className="muted">{t(language, "noEligibleCardTransfer")}</p>;
  return <form className="inline-form" onSubmit={submit}>
    <label>{t(language, "explicitSettlement")}<select name="transfer_id">{candidates.map((transaction) => <option key={transaction.transfer_id} value={transaction.transfer_id}>{formatFinancialDate(transaction.financial_date, language)} · {formatMoney(transaction.amount_minor, transaction.currency, language)}</option>)}</select></label>
    {error && <p className="form-error">{error}</p>}
    {mutation.hasRetry && <button type="button" className="subtle-button" onClick={() => void mutation.retry()} disabled={mutation.isSubmitting}>{t(language, "retryPending")}</button>}
    <button disabled={mutation.isSubmitting || mutation.hasRetry}>{t(language, "linkSettlement")}</button>
  </form>;
}

function MsiForm({ account, transactions, language, onChanged, onUnauthorized }: Pick<CreditCardDetailProps, "account" | "transactions" | "language" | "onChanged" | "onUnauthorized">) {
  type Input = Parameters<typeof api.createInstallmentPlan>[1];
  const mutation = useIdempotentMutation<Input, InstallmentPlan>((input, key) => api.createInstallmentPlan(account.id, input, key));
  const [error, setError] = useState("");
  const charges = transactions.filter((transaction) => transaction.effect === "liability_charge");
  const submitting = useRef(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current) return;
    submitting.current = true;
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      setError("");
      await mutation.submit({
        description: String(data.get("description")), purchase_transaction_id: String(data.get("charge_id")),
        original_principal_minor: parseMinorUnits(String(data.get("principal")), language), currency: account.currency,
        installment_count: Number(data.get("installment_count")), first_cycle_start: String(data.get("cycle_start")), first_cycle_end: String(data.get("cycle_end")),
      });
      form.reset();
      onChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(cause instanceof Error && !(cause instanceof ApiError) ? cause.message : t(language, "unableToCreateMsi"));
    } finally {
      submitting.current = false;
    }
  }

  return <form className="panel compact-form" onSubmit={submit}>
    <p className="eyebrow">MSI</p><h3>{t(language, "createMsi")}</h3>
    {charges.length === 0 ? <p className="empty">{t(language, "noCardCharges")}</p> : <>
      <label>{t(language, "sourceCharge")}<select name="charge_id">{charges.map((charge) => <option key={charge.id} value={charge.id}>{formatFinancialDate(charge.financial_date, language)} · {formatMoney(charge.amount_minor, charge.currency, language)}</option>)}</select></label>
      <label>{t(language, "description")}<input name="description" required /></label>
      <label>{t(language, "principal")}<input name="principal" required inputMode="decimal" /></label>
      <label>{t(language, "installmentCount")}<input name="installment_count" required type="number" min="1" /></label>
      <label>{t(language, "cycleStart")}<input name="cycle_start" required type="date" /></label>
      <label>{t(language, "cycleEnd")}<input name="cycle_end" required type="date" /></label>
      {mutation.hasRetry && <button type="button" className="subtle-button" onClick={() => void mutation.retry()} disabled={mutation.isSubmitting}>{t(language, "retryPending")}</button>}
      <button disabled={mutation.isSubmitting || mutation.hasRetry}>{t(language, "createMsi")}</button>
    </>}
    {error && <p className="form-error">{error}</p>}
  </form>;
}

function MsiPlans({ plans, language, onUnauthorized }: { plans: InstallmentPlan[]; language: Language; onUnauthorized: () => void }) {
  const [selected, setSelected] = useState("");
  const [detail, setDetail] = useState<InstallmentPlan | null>(null);
  const [allocations, setAllocations] = useState<InstallmentAllocation[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!selected) return;
    void Promise.all([api.installmentPlan(selected), api.installmentAllocations(selected)])
      .then(([plan, items]) => { setDetail(plan); setAllocations(items); setError(""); })
      .catch((cause) => { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(t(language, "unableToLoadCard")); });
  }, [language, onUnauthorized, selected]);
  return <section className="panel card-section">
    <h3>{t(language, "msiPlans")}</h3>
    {plans.length === 0 ? <p className="empty">{t(language, "noMsiPlans")}</p> : <div className="plan-list">{plans.map((plan) => <button className="subtle-button" key={plan.id} onClick={() => setSelected(plan.id)}>{plan.description} · {formatMoney(plan.original_principal_minor, plan.currency, language)} · {plan.installment_count}×</button>)}</div>}
    {error && <p className="form-error">{error}</p>}
    {detail && <div className="allocation-list">
      <p>{t(language, "paidPrincipal")}: {detail.paid_principal_minor !== undefined ? formatMoney(detail.paid_principal_minor, detail.currency, language) : "—"} · {t(language, "outstandingPrincipal")}: {detail.outstanding_principal_minor !== undefined ? formatMoney(detail.outstanding_principal_minor, detail.currency, language) : "—"}</p>
      <h4>{t(language, "allocations")}</h4>
      {allocations.map((allocation) => <div key={allocation.id}>#{allocation.installment_number} · {formatMoney(allocation.principal_minor, allocation.currency, language)} · {allocation.cycle_id} · {allocation.status}</div>)}
    </div>}
  </section>;
}
