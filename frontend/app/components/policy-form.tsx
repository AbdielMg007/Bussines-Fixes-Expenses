"use client";

import { FormEvent, useState } from "react";

import { ApiError, api, type Account, type ProjectionPolicy } from "../../lib/api";
import { parseMinorUnits } from "../../lib/format";

type PolicyFormProps = {
  accounts: Account[];
  policy?: ProjectionPolicy;
  onSaved: () => void;
  onUnauthorized?: () => void;
};

export function PolicyForm({ accounts, policy, onSaved, onUnauthorized }: PolicyFormProps) {
  const liquidAccounts = accounts.filter((account) => account.status === "active" && (account.type === "cash" || account.type === "bank"));
  const [horizon, setHorizon] = useState(String(policy?.horizon_days ?? 60));
  const [reserve, setReserve] = useState(policy ? String(policy.reserve_minor / 100) : "0");
  const [timezone, setTimezone] = useState(policy?.financial_timezone ?? "America/Mexico_City");
  const [mode, setMode] = useState<"all_active_liquid" | "explicit">(policy?.account_selection.mode ?? "all_active_liquid");
  const [selected, setSelected] = useState<string[]>(policy?.account_selection.account_ids ?? []);
  const [inflowPolicy, setInflowPolicy] = useState<"confirmed_only" | "include_expected">(policy?.inflow_policy ?? "confirmed_only");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  function toggleAccount(id: string) {
    setSelected((current) => current.includes(id) ? current.filter((value) => value !== id) : [...current, id]);
  }

  const liquidAccountIDs = new Set(liquidAccounts.map((account) => account.id));
  const staleSelectionIDs = selected.filter((id) => !liquidAccountIDs.has(id));

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSaving(true);
    try {
      const horizonDays = Number(horizon);
      if (!Number.isInteger(horizonDays)) throw new Error("Horizon must be a whole number of days.");
      await api.savePolicy({
        currency: "MXN",
        horizon_days: horizonDays,
        reserve_minor: parseMinorUnits(reserve),
        financial_timezone: timezone,
        account_selection: { mode, account_ids: mode === "explicit" ? selected : [] },
        inflow_policy: inflowPolicy,
        same_day_order: "outflows_before_inflows",
      });
      onSaved();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) {
        onUnauthorized?.();
        return;
      }
      setError(cause instanceof Error ? cause.message : "Unable to save projection settings.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="panel policy-panel">
      <div>
        <p className="eyebrow">Projection settings</p>
        <h2>{policy ? "Update your cash projection" : "Set up your cash projection"}</h2>
        <p className="muted">These settings define which cash accounts Runway projects. Nothing is saved until you choose Save.</p>
      </div>
      <form className="stack-form" onSubmit={submit}>
        <div className="form-grid">
          <label>Horizon days<input inputMode="numeric" value={horizon} onChange={(event) => setHorizon(event.target.value)} required /></label>
          <label>Cash reserve (MXN)<input inputMode="decimal" value={reserve} onChange={(event) => setReserve(event.target.value)} required /></label>
          <label>Financial timezone<input value={timezone} onChange={(event) => setTimezone(event.target.value)} required /></label>
          <label>Inflow policy
            <select value={inflowPolicy} onChange={(event) => setInflowPolicy(event.target.value as typeof inflowPolicy)}>
              <option value="confirmed_only">Confirmed inflows only</option>
              <option value="include_expected">Include expected inflows</option>
            </select>
          </label>
        </div>
        <fieldset>
          <legend>Liquid account selection</legend>
          <label className="radio-row"><input type="radio" checked={mode === "all_active_liquid"} onChange={() => setMode("all_active_liquid")} />All active cash and bank accounts</label>
          <label className="radio-row"><input type="radio" checked={mode === "explicit"} onChange={() => setMode("explicit")} />Choose specific accounts</label>
          {mode === "explicit" && <div className="check-list">
            {staleSelectionIDs.map((id) => {
              const account = accounts.find((value) => value.id === id);
              return <div key={id} className="stale-selection"><span><strong>{account?.name ?? `Unavailable account (${id})`}</strong><small>This saved selection can no longer participate in liquid cash.</small></span><button type="button" className="subtle-button" onClick={() => toggleAccount(id)}>Remove</button></div>;
            })}
            {liquidAccounts.map((account) => <label key={account.id} className="check-row"><input type="checkbox" checked={selected.includes(account.id)} onChange={() => toggleAccount(account.id)} />{account.name}</label>)}
            {liquidAccounts.length === 0 && <p className="muted">Create an active cash or bank account first.</p>}
          </div>}
        </fieldset>
        {error && <p className="form-error" role="alert">{error}</p>}
        <button type="submit" disabled={saving}>{saving ? "Saving…" : "Save projection settings"}</button>
      </form>
    </section>
  );
}
