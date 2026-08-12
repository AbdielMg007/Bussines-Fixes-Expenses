"use client";

import { FormEvent, useState } from "react";

import { ApiError, NetworkError, api, type Account, type ProjectionPolicy } from "../../lib/api";
import { parseMinorUnits } from "../../lib/format";
import { t, type Language } from "../../lib/i18n";

type PolicyFormProps = {
  accounts: Account[];
  policy?: ProjectionPolicy;
  onSaved: () => void;
  onUnauthorized?: () => void;
  language: Language;
};

export function PolicyForm({ accounts, policy, onSaved, onUnauthorized, language }: PolicyFormProps) {
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
      if (!Number.isInteger(horizonDays)) throw new Error(t(language, "invalidHorizon"));
      await api.savePolicy({
        currency: "MXN",
        horizon_days: horizonDays,
        reserve_minor: parseMinorUnits(reserve, language),
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
      setError(cause instanceof ApiError || cause instanceof NetworkError || !(cause instanceof Error) ? t(language, "unableToSavePolicy") : cause.message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="panel policy-panel">
      <div>
        <p className="eyebrow">{t(language, "projectionSettings")}</p>
        <h2>{policy ? t(language, "updateProjection") : t(language, "setupProjection")}</h2>
        <p className="muted">{t(language, "policyDescription")}</p>
      </div>
      <form className="stack-form" onSubmit={submit}>
        <div className="form-grid">
          <label>{t(language, "horizonDays")}<input inputMode="numeric" value={horizon} onChange={(event) => setHorizon(event.target.value)} required /></label>
          <label>{t(language, "cashReserve")}<input inputMode="decimal" value={reserve} onChange={(event) => setReserve(event.target.value)} required /></label>
          <label>{t(language, "financialTimezone")}<input value={timezone} onChange={(event) => setTimezone(event.target.value)} required /></label>
          <label>{t(language, "inflowPolicy")}
            <select value={inflowPolicy} onChange={(event) => setInflowPolicy(event.target.value as typeof inflowPolicy)}>
              <option value="confirmed_only">{t(language, "confirmedInflowsOnly")}</option>
              <option value="include_expected">{t(language, "includeExpectedInflows")}</option>
            </select>
          </label>
        </div>
        <fieldset>
          <legend>{t(language, "liquidAccountSelection")}</legend>
          <label className="radio-row"><input type="radio" checked={mode === "all_active_liquid"} onChange={() => setMode("all_active_liquid")} />{t(language, "allActiveLiquid")}</label>
          <label className="radio-row"><input type="radio" checked={mode === "explicit"} onChange={() => setMode("explicit")} />{t(language, "chooseSpecificAccounts")}</label>
          {mode === "explicit" && <div className="check-list">
            {staleSelectionIDs.map((id) => {
              const account = accounts.find((value) => value.id === id);
              return <div key={id} className="stale-selection"><span><strong>{account?.name ?? `${t(language, "unavailableAccount")} (${id})`}</strong><small>{t(language, "staleSelection")}</small></span><button type="button" className="subtle-button" onClick={() => toggleAccount(id)}>{t(language, "remove")}</button></div>;
            })}
            {liquidAccounts.map((account) => <label key={account.id} className="check-row"><input type="checkbox" checked={selected.includes(account.id)} onChange={() => toggleAccount(account.id)} />{account.name}</label>)}
            {liquidAccounts.length === 0 && <p className="muted">{t(language, "createLiquidAccountFirst")}</p>}
          </div>}
        </fieldset>
        {error && <p className="form-error" role="alert">{error}</p>}
        <button type="submit" disabled={saving}>{saving ? t(language, "saving") : t(language, "saveProjectionSettings")}</button>
      </form>
    </section>
  );
}
