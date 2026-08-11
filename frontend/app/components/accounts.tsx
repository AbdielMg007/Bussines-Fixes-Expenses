"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";

import { ApiError, api, type Account, type Balance, type Transaction, type TransactionEffect } from "../../lib/api";
import { formatFinancialDate, formatMoney, parseMinorUnits, todayFinancialDate } from "../../lib/format";
import { useIdempotentMutation } from "../hooks/use-idempotent-mutation";

type AccountsProps = {
  accounts: Account[];
  refreshToken: number;
  onDataChanged: () => void;
  onUnauthorized: () => void;
};

export function Accounts({ accounts, refreshToken, onDataChanged, onUnauthorized }: AccountsProps) {
  const [selectedID, setSelectedID] = useState("");
  const [createError, setCreateError] = useState("");
  const [balances, setBalances] = useState<Record<string, Balance>>({});

  useEffect(() => {
    setSelectedID((current) => accounts.some((account) => account.id === current) ? current : (accounts[0]?.id ?? ""));
  }, [accounts]);

  useEffect(() => {
    let live = true;
    void Promise.all(accounts.map(async (account) => [account.id, await api.balance(account.id)] as const))
      .then((values) => { if (live) setBalances(Object.fromEntries(values)); })
      .catch((cause) => { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); });
    return () => { live = false; };
  }, [accounts, refreshToken, onUnauthorized]);

  async function createAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setCreateError("");
    try {
      const account = await api.createAccount({ name: String(form.get("name") ?? ""), type: String(form.get("type")) as Account["type"], currency: "MXN" });
      setSelectedID(account.id);
      event.currentTarget.reset();
      onDataChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setCreateError(cause instanceof Error ? cause.message : "Unable to create account.");
    }
  }

  const selected = accounts.find((account) => account.id === selectedID);
  return <div className="accounts-layout">
    <aside className="account-list panel">
      <div className="section-heading"><div><p className="eyebrow">Accounts</p><h2>Your ledger</h2></div></div>
      {accounts.length === 0 ? <p className="empty">Create your first account.</p> : <div className="account-items">{accounts.map((account) => <button key={account.id} className={`account-item ${selectedID === account.id ? "selected" : ""} ${account.status}`} onClick={() => setSelectedID(account.id)}><span>{account.name}</span><small>{account.type.replaceAll("_", " ")} · {account.status} · {balances[account.id] ? formatMoney(balances[account.id].balance_minor, account.currency) : "Loading balance…"}</small></button>)}</div>}
      <form className="stack-form create-account" onSubmit={createAccount}>
        <h3>Create account</h3>
        <label>Name<input name="name" required maxLength={120} /></label>
        <label>Type<select name="type" defaultValue="bank"><option value="cash">Cash</option><option value="bank">Bank</option><option value="credit_card">Credit card</option><option value="loan">Loan</option></select></label>
        {createError && <p className="form-error">{createError}</p>}
        <button type="submit">Add account</button>
      </form>
    </aside>
    <section>{selected ? <AccountDetail key={selected.id} account={selected} accounts={accounts} refreshToken={refreshToken} onDataChanged={onDataChanged} onUnauthorized={onUnauthorized} /> : <section className="panel state-panel"><h2>No account selected</h2><p className="muted">Create an account to start a ledger.</p></section>}</section>
  </div>;
}

function AccountDetail({ account, accounts, refreshToken, onDataChanged, onUnauthorized }: { account: Account; accounts: Account[]; refreshToken: number; onDataChanged: () => void; onUnauthorized: () => void }) {
  const [balance, setBalance] = useState<Balance | null>(null);
  const [transactions, setTransactions] = useState<Transaction[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const [nextBalance, nextTransactions] = await Promise.all([api.balance(account.id), api.transactions(account.id)]);
      setBalance(nextBalance);
      setTransactions(nextTransactions);
      setError("");
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(cause instanceof Error ? cause.message : "Unable to load account activity.");
    } finally {
      setLoading(false);
    }
  }, [account.id, onUnauthorized]);

  useEffect(() => { void reload(); }, [reload, refreshToken]);

  async function archive() {
    if (!window.confirm(`Archive ${account.name}? Historical activity stays visible.`)) return;
    try {
      await api.archiveAccount(account.id);
      onDataChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(cause instanceof Error ? cause.message : "Unable to archive account.");
    }
  }

  return <div className="account-detail">
    <section className={`panel account-header ${account.status}`}>
      <div><p className="eyebrow">{account.type.replaceAll("_", " ")}</p><h1>{account.name}</h1><p className="muted">{account.status === "archived" ? "Archived — historical records remain read-only." : "Active account"}</p></div>
      <div className="account-balance">{loading ? "Loading…" : balance ? formatMoney(balance.balance_minor, balance.currency) : "—"}</div>
      {account.status === "active" && <button className="subtle-button" onClick={archive}>Archive</button>}
    </section>
    {error && <p className="form-error">{error}</p>}
    {account.status === "active" && <div className="activity-forms">
      <ManualTransactionForm account={account} onSuccess={() => { onDataChanged(); void reload(); }} onUnauthorized={onUnauthorized} />
      <TransferForm account={account} accounts={accounts} onSuccess={() => { onDataChanged(); void reload(); }} onUnauthorized={onUnauthorized} />
    </div>}
    <section className="panel transactions-panel"><div className="section-heading"><div><p className="eyebrow">Movements</p><h2>Posted transactions</h2></div></div>
      {loading ? <p className="empty">Loading movements…</p> : transactions.length === 0 ? <p className="empty">No posted transactions yet.</p> : <div className="transaction-list">{transactions.map((transaction) => <TransactionRow key={transaction.id} transaction={transaction} currency={account.currency} />)}</div>}
    </section>
  </div>;
}

function ManualTransactionForm({ account, onSuccess, onUnauthorized }: { account: Account; onSuccess: () => void; onUnauthorized: () => void }) {
  type Input = { effect: TransactionEffect; amount_minor: number; currency: string; financial_date: string; memo: string };
  const mutation = useIdempotentMutation<Input, Transaction>((input, key) => api.postTransaction(account.id, input, key));
  const [error, setError] = useState("");
  const effects: TransactionEffect[] = account.type === "cash" || account.type === "bank" ? ["asset_inflow", "asset_outflow"] : ["liability_charge", "liability_payment"];

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    try {
      setError("");
      await mutation.submit({ effect: String(form.get("effect")) as TransactionEffect, amount_minor: parseMinorUnits(String(form.get("amount") ?? "")), currency: account.currency, financial_date: String(form.get("financial_date")), memo: String(form.get("memo") ?? "") });
      event.currentTarget.reset();
      onSuccess();
    } catch (cause) { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof Error ? cause.message : "Unable to post movement."); }
  }

  return <form className="panel compact-form" onSubmit={submit}>
    <p className="eyebrow">Manual movement</p><h3>Post a transaction</h3>
    <label>Effect<select name="effect" defaultValue={effects[0]}>{effects.map((effect) => <option key={effect} value={effect}>{effect.replaceAll("_", " ")}</option>)}</select></label>
    <label>Amount (MXN)<input name="amount" inputMode="decimal" required placeholder="0.00" /></label>
    <label>Financial date<input name="financial_date" type="date" required defaultValue={todayFinancialDate()} /></label>
    <label>Memo<input name="memo" maxLength={500} /></label>
    {error && <p className="form-error">{error}</p>}
    {mutation.hasRetry && <button type="button" className="subtle-button" onClick={() => void mutation.retry().then(onSuccess).catch((cause) => { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof Error ? cause.message : "Retry failed."); })}>Retry pending request</button>}
    <button type="submit" disabled={mutation.isSubmitting || mutation.hasRetry}>{mutation.isSubmitting ? "Posting…" : mutation.hasRetry ? "Retry the pending request first" : "Post transaction"}</button>
  </form>;
}

function TransferForm({ account, accounts, onSuccess, onUnauthorized }: { account: Account; accounts: Account[]; onSuccess: () => void; onUnauthorized: () => void }) {
  type Input = { source_account_id: string; destination_account_id: string; amount_minor: number; currency: string; financial_date: string; memo: string };
  const mutation = useIdempotentMutation<Input, unknown>((input, key) => api.transfer(input, key));
  const [error, setError] = useState("");
  const sourceAccounts = accounts.filter((value) => value.status === "active" && (value.type === "cash" || value.type === "bank"));
  const destinations = accounts.filter((value) => value.status === "active" && (value.type === "cash" || value.type === "bank" || value.type === "credit_card"));

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    try {
      setError("");
      await mutation.submit({ source_account_id: String(form.get("source")), destination_account_id: String(form.get("destination")), amount_minor: parseMinorUnits(String(form.get("amount") ?? "")), currency: account.currency, financial_date: String(form.get("financial_date")), memo: String(form.get("memo") ?? "") });
      event.currentTarget.reset();
      onSuccess();
    } catch (cause) { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof Error ? cause.message : "Unable to create transfer."); }
  }

  if (sourceAccounts.length === 0 || destinations.length < 2) return null;
  return <form className="panel compact-form" onSubmit={submit}>
    <p className="eyebrow">Internal transfer</p><h3>Move money or pay a card</h3>
    <label>From<select name="source" defaultValue={account.type === "cash" || account.type === "bank" ? account.id : sourceAccounts[0].id}>{sourceAccounts.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>
    <label>To<select name="destination" defaultValue={destinations.find((value) => value.id !== account.id)?.id}>{destinations.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>
    <label>Amount (MXN)<input name="amount" inputMode="decimal" required placeholder="0.00" /></label>
    <label>Financial date<input name="financial_date" type="date" required defaultValue={todayFinancialDate()} /></label>
    <label>Memo<input name="memo" maxLength={500} /></label>
    {error && <p className="form-error">{error}</p>}
    {mutation.hasRetry && <button type="button" className="subtle-button" onClick={() => void mutation.retry().then(onSuccess).catch((cause) => { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof Error ? cause.message : "Retry failed."); })}>Retry pending request</button>}
    <button type="submit" disabled={mutation.isSubmitting || mutation.hasRetry}>{mutation.isSubmitting ? "Moving…" : mutation.hasRetry ? "Retry the pending request first" : "Create transfer"}</button>
  </form>;
}

function TransactionRow({ transaction, currency }: { transaction: Transaction; currency: string }) {
  const outgoing = transaction.effect === "asset_outflow" || transaction.effect === "liability_payment";
  return <article className={`transaction-row ${outgoing ? "outflow" : "inflow"}`}><time>{formatFinancialDate(transaction.financial_date)}</time><div><strong>{transaction.memo || transaction.effect.replaceAll("_", " ")}</strong><span>{transaction.effect.replaceAll("_", " ")}{transaction.transfer_id ? " · linked transfer" : ""}</span></div><b>{outgoing ? "−" : "+"}{formatMoney(transaction.amount_minor, currency)}</b></article>;
}
