"use client";

import { useEffect, useRef, useState } from "react";

import { ApiError, api, type Account, type Projection, type SafeToSpend } from "../../lib/api";
import { formatFinancialDate, formatMoney } from "../../lib/format";
import { PolicyForm } from "./policy-form";

type DashboardProps = {
  accounts: Account[];
  refreshToken: number;
  onDataChanged: () => void;
  onUnauthorized: () => void;
};

const statusCopy: Record<SafeToSpend["status"], string> = {
  safe: "Your selected account and current projection both support this amount.",
  constrained_by_future_cash_flow: "An upcoming cash-flow low is limiting today’s spend.",
  constrained_by_funding_balance: "The selected funding account balance is the limiting factor.",
  already_below_reserve: "Your baseline projection is already below the configured reserve.",
  unsupported_funding_type: "This account cannot fund Safe-to-Spend yet.",
};

export function Dashboard({ accounts, refreshToken, onDataChanged, onUnauthorized }: DashboardProps) {
  const eligible = accounts.filter((account) => account.status === "active" && (account.type === "cash" || account.type === "bank"));
  const [fundingID, setFundingID] = useState("");
  const [projection, setProjection] = useState<Projection | null>(null);
  const [safe, setSafe] = useState<SafeToSpend | null>(null);
  const [needsPolicy, setNeedsPolicy] = useState(false);
  const [policyNotice, setPolicyNotice] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
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
      setError("");
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
        } else setError(cause instanceof Error ? cause.message : "Unable to load the projection.");
      } finally {
        if (isCurrent()) setLoading(false);
      }
    }
    void load();
    return () => { live = false; };
  }, [fundingID, refreshToken, onUnauthorized]);

  if (loading) return <section className="panel state-panel">Loading your cash position…</section>;
  if (needsPolicy) return <PolicyBootstrap accounts={accounts} notice={policyNotice} onDataChanged={onDataChanged} onUnauthorized={onUnauthorized} />;
  if (error) return <section className="panel state-panel"><p className="form-error">{error}</p><button onClick={onDataChanged}>Try again</button></section>;

  return (
    <div className="dashboard-grid">
      <section className="hero-card">
        <p className="eyebrow">Safe to spend today</p>
        {safe ? <>
          <p className="hero-amount">{formatMoney(safe.safe_to_spend_minor, safe.currency)}</p>
          <p className={`status ${safe.status}`}>{statusCopy[safe.status]}</p>
          {safe.status === "already_below_reserve" && <p className="warning">Deficit: {formatMoney(safe.deficit_minor, safe.currency)}{safe.earliest_breach_date ? ` · earliest breach ${formatFinancialDate(safe.earliest_breach_date)}` : ""}</p>}
        </> : <p className="muted">Choose an active cash or bank account to see Safe-to-Spend.</p>}
        <label className="funding-select">Funding account
          <select value={fundingID} onChange={(event) => setFundingID(event.target.value)} disabled={eligible.length === 0}>
            {eligible.length === 0 ? <option>No eligible funding account</option> : eligible.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}
          </select>
        </label>
      </section>

      {safe && <section className="metrics-card">
        <Metric label="Funding account" value={formatMoney(safe.funding_account_balance_minor, safe.currency)} />
        <Metric label="Opening liquid cash" value={formatMoney(safe.opening_liquid_balance_minor, safe.currency)} />
        <Metric label="Reserve" value={formatMoney(safe.reserve_minor, safe.currency)} />
        <Metric label="Projected minimum" value={formatMoney(safe.baseline_minimum_balance_minor, safe.currency)} />
      </section>}

      {eligible.length === 0 && <section className="panel state-panel"><h2>A cash or bank account is required</h2><p className="muted">Create an active cash or bank account, then include it in ProjectionPolicy.</p></section>}
      {projection && <ProjectionTimeline projection={projection} />}
    </div>
  );
}

function PolicyBootstrap({ accounts, notice, onDataChanged, onUnauthorized }: Pick<DashboardProps, "accounts" | "onDataChanged" | "onUnauthorized"> & { notice: string }) {
  const [policyLoaded, setPolicyLoaded] = useState(false);
  const [policy, setPolicy] = useState<Awaited<ReturnType<typeof api.policy>> | undefined>();
  const [policyError, setPolicyError] = useState("");
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
          setPolicyError(cause instanceof Error ? cause.message : "Unable to load projection settings.");
        }
      })
      .finally(() => { if (live) setPolicyLoaded(true); });
    return () => { live = false; };
  }, [onUnauthorized]);
  if (unauthorized) return <section className="panel state-panel">Returning to sign in…</section>;
  if (!policyLoaded) return <section className="panel state-panel">Loading settings…</section>;
  if (policyError) return <section className="panel state-panel"><p className="form-error">{policyError}</p><p className="muted">Unable to load ProjectionPolicy. Try again after the service is available.</p></section>;
  return <>{notice && <section className="panel state-panel"><p className="form-error">{notice}</p><p className="muted">Update ProjectionPolicy before Runway can calculate your cash timeline.</p></section>}<PolicyForm accounts={accounts} policy={policy} onSaved={onDataChanged} onUnauthorized={onUnauthorized} /></>;
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><strong>{value}</strong></div>;
}

function ProjectionTimeline({ projection }: { projection: Projection }) {
  return <section className="panel timeline-panel">
    <div className="section-heading"><div><p className="eyebrow">Baseline projection</p><h2>Future cash timeline</h2></div><p className="muted">Opening {formatMoney(projection.opening_liquid_balance_minor, projection.currency)}</p></div>
    {projection.events.length === 0 ? <p className="empty">No future projected events inside this horizon.</p> : <ol className="timeline">
      {projection.events.map((event) => <li key={event.id} className={event.direction}>
        <time>{formatFinancialDate(event.financial_date)}</time>
        <div><strong>{event.label || event.source_kind.replaceAll("_", " ")}</strong><span>{event.inclusion_basis.replaceAll("_", " ")}</span></div>
        <b>{event.direction === "outflow" ? "−" : "+"}{formatMoney(event.amount_minor, projection.currency)}</b>
        <strong className="after-balance">{formatMoney(event.balance_after_minor, projection.currency)}</strong>
      </li>)}
    </ol>}
    {projection.exclusions.length > 0 && <details className="exclusions"><summary>Excluded future inflows ({projection.exclusions.length})</summary><ul>{projection.exclusions.map((item) => <li key={`${item.source_kind}:${item.source_id}`}>{item.label || item.source_kind.replaceAll("_", " ")} — {item.reasons.join(", ").replaceAll("_", " ")}</li>)}</ul></details>}
  </section>;
}
