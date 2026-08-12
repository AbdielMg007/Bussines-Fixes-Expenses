"use client";

import { useEffect, useRef, useState } from "react";

import { ApiError, api, type Account, type Projection, type SafeToSpend } from "../../lib/api";
import { formatFinancialDate, formatMoney } from "../../lib/format";
import { exclusionText, inclusionText, sourceText, t, type Language } from "../../lib/i18n";
import { PolicyForm } from "./policy-form";

type DashboardProps = {
  accounts: Account[];
  refreshToken: number;
  onDataChanged: () => void;
  onUnauthorized: () => void;
  language: Language;
};

const statusKeys: Record<SafeToSpend["status"], "safe" | "constrainedByFuture" | "constrainedByFunding" | "belowReserve" | "unsupportedFunding" | "indeterminate"> = {
  safe: "safe", constrained_by_future_cash_flow: "constrainedByFuture", constrained_by_funding_balance: "constrainedByFunding", already_below_reserve: "belowReserve", unsupported_funding_type: "unsupportedFunding", indeterminate: "indeterminate",
};

const cardIssueKeys = {
  card_payment_intent_missing: "cardPaymentIntentMissing",
  card_payment_intent_needs_review: "cardPaymentIntentNeedsReview",
  card_payment_due_today_unsettled: "cardPaymentDueTodayUnsettled",
  card_payment_past_due_unsettled: "cardPaymentPastDueUnsettled",
  invalid_card_payment_flow: "invalidCardPaymentFlow",
} as const;

export function Dashboard({ accounts, refreshToken, onDataChanged, onUnauthorized, language }: DashboardProps) {
  const eligible = accounts.filter((account) => account.status === "active" && (account.type === "cash" || account.type === "bank"));
  const [fundingID, setFundingID] = useState("");
  const [projection, setProjection] = useState<Projection | null>(null);
  const [safe, setSafe] = useState<SafeToSpend | null>(null);
  const [needsPolicy, setNeedsPolicy] = useState(false);
  const [policyNotice, setPolicyNotice] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const requestSequence = useRef(0);

  useEffect(() => {
    setFundingID((current) => eligible.some((account) => account.id === current) ? current : (eligible[0]?.id ?? ""));
  }, [accounts, refreshToken]);

  useEffect(() => {
    let live = true;
    const requestID = ++requestSequence.current;
    const isCurrent = () => live && requestID === requestSequence.current;
    async function load() {
      setLoading(true);
      setError(false);
      setNeedsPolicy(false);
      setPolicyNotice("");
      try {
        const timeline = await api.projection();
        if (!isCurrent()) return;
        setProjection(timeline);
        if (fundingID) {
          const nextSafe = await api.safeToSpend(fundingID);
          if (!isCurrent() || nextSafe.funding_account_id !== fundingID) return;
          setSafe(nextSafe);
        } else if (isCurrent()) setSafe(null);
      } catch (cause) {
        if (!isCurrent()) return;
        if (cause instanceof ApiError && cause.status === 401) return onUnauthorized();
        if (cause instanceof ApiError && cause.status === 409) {
          setNeedsPolicy(true);
          setPolicyNotice(cause.message);
          setProjection(null);
          setSafe(null);
        } else setError(true);
      } finally {
        if (isCurrent()) setLoading(false);
      }
    }
    void load();
    return () => { live = false; };
  }, [fundingID, refreshToken, onUnauthorized]);

  if (loading) return <section className="panel state-panel">{t(language, "loadingCashPosition")}</section>;
  if (needsPolicy) return <PolicyBootstrap language={language} accounts={accounts} notice={policyNotice} onDataChanged={onDataChanged} onUnauthorized={onUnauthorized} />;
  if (error) return <section className="panel state-panel"><p className="form-error">{t(language, "unableToLoadProjection")}</p><button onClick={onDataChanged}>{t(language, "retry")}</button></section>;

  return (
    <div className="dashboard-grid">
      <section className="hero-card">
        <p className="eyebrow">{t(language, "safeToSpendToday")}</p>
        {safe ? <>
          <p className="hero-amount">{safe.status === "indeterminate" ? t(language, "safeToSpendUnavailable") : formatMoney(safe.safe_to_spend_minor, safe.currency, language)}</p>
          <p className={`status ${safe.status}`}>{t(language, statusKeys[safe.status])}</p>
          {safe.status === "indeterminate" && <p className="warning">{cardIssueText(language, safe.indeterminate_reason)}</p>}
          {safe.status === "already_below_reserve" && <p className="warning">{t(language, "deficit")}: {formatMoney(safe.deficit_minor, safe.currency, language)}{safe.earliest_breach_date ? ` · ${t(language, "earliestBreach")} ${formatFinancialDate(safe.earliest_breach_date, language)}` : ""}</p>}
        </> : <p className="muted">{t(language, "chooseFunding")}</p>}
        <label className="funding-select">{t(language, "fundingAccount")}
          <select value={fundingID} onChange={(event) => setFundingID(event.target.value)} disabled={eligible.length === 0}>
            {eligible.length === 0 ? <option>{t(language, "noEligibleFunding")}</option> : eligible.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}
          </select>
        </label>
      </section>

      {safe && <section className="metrics-card">
        <Metric label={t(language, "fundingAccount")} value={formatMoney(safe.funding_account_balance_minor, safe.currency, language)} />
        <Metric label={t(language, "openingLiquidCash")} value={formatMoney(safe.opening_liquid_balance_minor, safe.currency, language)} />
        <Metric label={t(language, "reserve")} value={formatMoney(safe.reserve_minor, safe.currency, language)} />
        <Metric label={t(language, "projectedMinimum")} value={formatMoney(safe.baseline_minimum_balance_minor, safe.currency, language)} />
      </section>}

      {eligible.length === 0 && <section className="panel state-panel"><h2>{t(language, "cashOrBankRequired")}</h2><p className="muted">{t(language, "cashOrBankRequiredDetail")}</p></section>}
      {projection && <ProjectionTimeline language={language} projection={projection} />}
      {projection && <CardPaymentSummary accounts={accounts} projection={projection} language={language} onUnauthorized={onUnauthorized} />}
    </div>
  );
}

function PolicyBootstrap({ accounts, notice, onDataChanged, onUnauthorized, language }: Pick<DashboardProps, "accounts" | "onDataChanged" | "onUnauthorized" | "language"> & { notice: string }) {
  const [policyLoaded, setPolicyLoaded] = useState(false);
  const [policy, setPolicy] = useState<Awaited<ReturnType<typeof api.policy>> | undefined>();
  const [policyError, setPolicyError] = useState(false);
  const [unauthorized, setUnauthorized] = useState(false);
  useEffect(() => {
    let live = true;
    void api.policy()
      .then((value) => { if (live) setPolicy(value); })
      .catch((cause) => {
        if (!live) return;
        if (cause instanceof ApiError && cause.status === 401) {
          setUnauthorized(true);
          onUnauthorized();
          return;
        }
        // A missing policy is the legitimate bootstrap path. Other failures
        // are visible rather than being mistaken for missing configuration.
        if (!(cause instanceof ApiError && cause.status === 404)) {
          setPolicyError(true);
        }
      })
      .finally(() => { if (live) setPolicyLoaded(true); });
    return () => { live = false; };
  }, [onUnauthorized]);
  if (unauthorized) return <section className="panel state-panel">{t(language, "returningToSignIn")}</section>;
  if (!policyLoaded) return <section className="panel state-panel">{t(language, "loadingSettings")}</section>;
  if (policyError) return <section className="panel state-panel"><p className="form-error">{t(language, "unableToLoadPolicy")}</p></section>;
  return <>{notice && <section className="panel state-panel"><p className="form-error">{t(language, "invalidProjectionConfiguration")}</p><p className="muted">{t(language, "updatePolicy")}</p></section>}<PolicyForm language={language} accounts={accounts} policy={policy} onSaved={onDataChanged} onUnauthorized={onUnauthorized} /></>;
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><strong>{value}</strong></div>;
}

function ProjectionTimeline({ projection, language }: { projection: Projection; language: Language }) {
  return <section className="panel timeline-panel">
    <div className="section-heading"><div><p className="eyebrow">{t(language, "baselineProjection")}</p><h2>{t(language, "futureCashTimeline")}</h2></div><p className="muted">{t(language, "opening")} {formatMoney(projection.opening_liquid_balance_minor, projection.currency, language)}</p></div>
    {projection.completeness === "indeterminate" && <p className="warning">{t(language, "projectionIncomplete")} {projection.issues?.map((issue) => cardIssueText(language, issue.code)).join(" ")}</p>}
    {projection.events.length === 0 ? <p className="empty">{t(language, "noFutureEvents")}</p> : <ol className="timeline">
      {projection.events.map((event) => <li key={event.id} className={event.direction}>
        <time>{formatFinancialDate(event.financial_date, language)}</time>
        <div><strong>{event.label || sourceText(language, event.source_kind)}</strong><span>{inclusionText(language, event.inclusion_basis)}</span></div>
        <b>{event.direction === "outflow" ? "−" : "+"}{formatMoney(event.amount_minor, projection.currency, language)}</b>
        <strong className="after-balance">{formatMoney(event.balance_after_minor, projection.currency, language)}</strong>
      </li>)}
    </ol>}
    {projection.exclusions.length > 0 && <details className="exclusions"><summary>{t(language, "excludedInflows")} ({projection.exclusions.length})</summary><ul>{projection.exclusions.map((item) => <li key={`${item.source_kind}:${item.source_id}`}>{item.label || sourceText(language, item.source_kind)} — {item.reasons.map((reason) => exclusionText(language, reason)).join(", ")}</li>)}</ul></details>}
  </section>;
}

function cardIssueText(language: Language, reason?: string): string {
  if (reason && reason in cardIssueKeys) return t(language, cardIssueKeys[reason as keyof typeof cardIssueKeys]);
  return t(language, "unknownCardProjectionIssue");
}

function CardPaymentSummary({ accounts, projection, language, onUnauthorized }: Pick<DashboardProps, "accounts" | "language" | "onUnauthorized"> & { projection: Projection }) {
  type Summary = { account: Account; amountMinor?: number; date?: string; status?: string };
  const [items, setItems] = useState<Summary[]>([]);
  useEffect(() => {
    let live = true;
    const cards = accounts.filter((account) => account.type === "credit_card");
    if (cards.length === 0) { setItems([]); return; }
    void Promise.all(cards.map(async (account) => {
      const statements = await api.cardStatements(account.id);
      const current = statements.find((statement) => !statement.superseded_at);
      if (!current) return { account };
      try {
        const intent = await api.paymentIntent(current.cycle_id);
        // The amount/date come from the backend's projection event, not from a
        // client-side settlement or card-payment calculation.
        const event = projection.events.find((candidate) => candidate.source_kind === "credit_card_payment_intent" && candidate.source_id === intent.id);
        return { account, amountMinor: event?.amount_minor, date: event?.financial_date, status: intent.status };
      } catch (cause) {
        if (cause instanceof ApiError && cause.status === 404) return { account };
        throw cause;
      }
    })).then((next) => { if (live) setItems(next); }).catch((cause) => {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
    });
    return () => { live = false; };
  }, [accounts, language, onUnauthorized, projection]);
  if (items.length === 0) return null;
  return <section className="panel timeline-panel card-summary"><h2>{t(language, "upcomingCardPayments")}</h2>{items.map((item) => <div key={item.account.id} className="card-summary-row"><strong>{item.account.name}</strong>{item.amountMinor !== undefined && item.date ? <span>{formatFinancialDate(item.date, language)} · {formatMoney(item.amountMinor, projection.currency, language)}</span> : <span className="warning">{item.status === "needs_review" ? t(language, "cardPaymentIntentNeedsReview") : t(language, "projectionIncomplete")}</span>}</div>)}</section>;
}
