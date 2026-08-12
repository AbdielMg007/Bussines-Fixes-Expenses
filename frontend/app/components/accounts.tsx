"use client";

import { FormEvent, useCallback, useEffect, useRef, useState } from "react";

import { ApiError, NetworkError, api, type Account, type Balance, type Transaction, type TransactionEffect } from "../../lib/api";
import { formatFinancialDate, formatMoney, parseMinorUnits, todayFinancialDate } from "../../lib/format";
import { accountStatusText, accountTypeText, effectText, t, type Language } from "../../lib/i18n";
import { useIdempotentMutation } from "../hooks/use-idempotent-mutation";
import { CreditCardDetail } from "./credit-card";

type AccountsProps = {
  accounts: Account[];
  refreshToken: number;
  onDataChanged: () => void;
  onUnauthorized: () => void;
  language: Language;
};

export function Accounts({ accounts, refreshToken, onDataChanged, onUnauthorized, language }: AccountsProps) {
  const [selectedID, setSelectedID] = useState("");
  const [createError, setCreateError] = useState("");
  const [creatingAccount, setCreatingAccount] = useState(false);
  const creatingAccountRef = useRef(false);
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
    if (creatingAccountRef.current) return;

    creatingAccountRef.current = true;
    setCreatingAccount(true);
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    setCreateError("");
    try {
      const account = await api.createAccount({ name: String(form.get("name") ?? ""), type: String(form.get("type")) as Account["type"], currency: "MXN" });
      setSelectedID(account.id);
      formElement.reset();
      onDataChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setCreateError(t(language, "unableToCreateAccount"));
    } finally {
      creatingAccountRef.current = false;
      setCreatingAccount(false);
    }
  }

  const selected = accounts.find((account) => account.id === selectedID);
  return <div className="accounts-layout">
    <aside className="account-list panel">
      <div className="section-heading"><div><p className="eyebrow">{t(language, "accounts")}</p><h2>{t(language, "yourLedger")}</h2></div></div>
      {accounts.length === 0 ? <p className="empty">{t(language, "createFirstAccount")}</p> : <div className="account-items">{accounts.map((account) => <button key={account.id} className={`account-item ${selectedID === account.id ? "selected" : ""} ${account.status}`} onClick={() => setSelectedID(account.id)}><span>{account.name}</span><small>{accountTypeText(language, account.type)} · {accountStatusText(language, account.status)} · {balances[account.id] ? formatMoney(balances[account.id].balance_minor, account.currency, language) : t(language, "loadingBalance")}</small></button>)}</div>}
      <form className="stack-form create-account" onSubmit={createAccount}>
        <h3>{t(language, "createAccount")}</h3>
        <label>{t(language, "name")}<input name="name" required maxLength={120} /></label>
        <label>{t(language, "accountType")}<select name="type" defaultValue="bank"><option value="cash">{t(language, "cash")}</option><option value="bank">{t(language, "bank")}</option><option value="credit_card">{t(language, "creditCard")}</option><option value="loan">{t(language, "loan")}</option></select></label>
        {createError && <p className="form-error">{createError}</p>}
        <button type="submit" disabled={creatingAccount}>{creatingAccount ? t(language, "addingAccount") : t(language, "addAccount")}</button>
      </form>
    </aside>
    <section>{selected ? <AccountDetail key={selected.id} language={language} account={selected} accounts={accounts} refreshToken={refreshToken} onDataChanged={onDataChanged} onUnauthorized={onUnauthorized} /> : <section className="panel state-panel"><h2>{t(language, "noAccountSelected")}</h2><p className="muted">{t(language, "createAccountToStart")}</p></section>}</section>
  </div>;
}

function AccountDetail({ account, accounts, refreshToken, onDataChanged, onUnauthorized, language }: { account: Account; accounts: Account[]; refreshToken: number; onDataChanged: () => void; onUnauthorized: () => void; language: Language }) {
  const [balance, setBalance] = useState<Balance | null>(null);
  const [transactions, setTransactions] = useState<Transaction[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const [nextBalance, nextTransactions] = await Promise.all([api.balance(account.id), api.transactions(account.id)]);
      setBalance(nextBalance);
      setTransactions(nextTransactions);
      setError(false);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(true);
    } finally {
      setLoading(false);
    }
  }, [account.id, onUnauthorized]);

  useEffect(() => { void reload(); }, [reload, refreshToken]);

  async function archive() {
    if (!window.confirm(t(language, "archiveConfirm", { name: account.name }))) return;
    try {
      await api.archiveAccount(account.id);
      onDataChanged();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) onUnauthorized();
      else setError(true);
    }
  }

  return <div className="account-detail">
    <section className={`panel account-header ${account.status}`}>
      <div><p className="eyebrow">{accountTypeText(language, account.type)}</p><h1>{account.name}</h1><p className="muted">{account.status === "archived" ? t(language, "archivedDetail") : t(language, "activeAccount")}</p></div>
      <div className="account-balance">{loading ? t(language, "loadingRunway") : balance ? formatMoney(balance.balance_minor, balance.currency, language) : "—"}</div>
      {account.status === "active" && <button className="subtle-button" onClick={archive}>{t(language, "archive")}</button>}
    </section>
    {error && <p className="form-error">{t(language, "unableToLoadProjection")}</p>}
    {account.status === "active" && <div className="activity-forms">
      <ManualTransactionForm language={language} account={account} onSuccess={() => { onDataChanged(); void reload(); }} onUnauthorized={onUnauthorized} />
      <TransferForm language={language} account={account} accounts={accounts} onSuccess={() => { onDataChanged(); void reload(); }} onUnauthorized={onUnauthorized} />
    </div>}
    {account.type === "credit_card" && <CreditCardDetail account={account} transactions={transactions} refreshToken={refreshToken} language={language} onChanged={() => { onDataChanged(); void reload(); }} onUnauthorized={onUnauthorized} />}
    <section className="panel transactions-panel"><div className="section-heading"><div><p className="eyebrow">{t(language, "movements")}</p><h2>{t(language, "postTransaction")}</h2></div></div>
      {loading ? <p className="empty">{t(language, "loadingRunway")}</p> : transactions.length === 0 ? <p className="empty">{t(language, "noTransactions")}</p> : <div className="transaction-list">{transactions.map((transaction) => <TransactionRow language={language} key={transaction.id} transaction={transaction} currency={account.currency} />)}</div>}
    </section>
  </div>;
}

function ManualTransactionForm({ account, onSuccess, onUnauthorized, language }: { account: Account; onSuccess: () => void; onUnauthorized: () => void; language: Language }) {
  type Input = { effect: TransactionEffect; amount_minor: number; currency: string; financial_date: string; memo: string };
  const mutation = useIdempotentMutation<Input, Transaction>((input, key) => api.postTransaction(account.id, input, key));
  const [error, setError] = useState("");
  const submittingRef = useRef(false);
  const effects: TransactionEffect[] = account.type === "cash" || account.type === "bank" ? ["asset_inflow", "asset_outflow"] : ["liability_charge", "liability_payment"];

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submittingRef.current) return;

    submittingRef.current = true;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      setError("");
      await mutation.submit({ effect: String(form.get("effect")) as TransactionEffect, amount_minor: parseMinorUnits(String(form.get("amount") ?? ""), language), currency: account.currency, financial_date: String(form.get("financial_date")), memo: String(form.get("memo") ?? "") });
      formElement.reset();
      onSuccess();
    } catch (cause) { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof ApiError || cause instanceof NetworkError || !(cause instanceof Error) ? t(language, "unableToPost") : cause.message); } finally { submittingRef.current = false; }
  }

  async function retry() {
    if (submittingRef.current || !mutation.hasRetry) return;

    submittingRef.current = true;
    try {
      setError("");
      await mutation.retry();
      onSuccess();
    } catch (cause) { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof ApiError || cause instanceof NetworkError || !(cause instanceof Error) ? t(language, "retryFailed") : cause.message); } finally { submittingRef.current = false; }
  }

  return <form className="panel compact-form" onSubmit={submit}>
    <p className="eyebrow">{t(language, "manualMovement")}</p><h3>{t(language, "postTransaction")}</h3>
    <label>{t(language, "effect")}<select name="effect" defaultValue={effects[0]}>{effects.map((effect) => <option key={effect} value={effect}>{effectText(language, effect)}</option>)}</select></label>
    <label>{t(language, "amount")}<input name="amount" inputMode="decimal" required placeholder="0.00" /></label>
    <label>{t(language, "financialDate")}<input name="financial_date" type="date" required defaultValue={todayFinancialDate()} /></label>
    <label>{t(language, "memo")}<input name="memo" maxLength={500} /></label>
    {error && <p className="form-error">{error}</p>}
    {mutation.hasRetry && <button type="button" className="subtle-button" disabled={mutation.isSubmitting} onClick={() => void retry()}>{t(language, "retryPending")}</button>}
    <button type="submit" disabled={mutation.isSubmitting || mutation.hasRetry}>{mutation.isSubmitting ? t(language, "posting") : mutation.hasRetry ? t(language, "retryPendingFirst") : t(language, "post")}</button>
  </form>;
}

function TransferForm({ account, accounts, onSuccess, onUnauthorized, language }: { account: Account; accounts: Account[]; onSuccess: () => void; onUnauthorized: () => void; language: Language }) {
  type Input = { source_account_id: string; destination_account_id: string; amount_minor: number; currency: string; financial_date: string; memo: string };
  const mutation = useIdempotentMutation<Input, unknown>((input, key) => api.transfer(input, key));
  const [error, setError] = useState("");
  const submittingRef = useRef(false);
  const sourceAccounts = accounts.filter((value) => value.status === "active" && (value.type === "cash" || value.type === "bank"));
  const destinations = accounts.filter((value) => value.status === "active" && (value.type === "cash" || value.type === "bank" || value.type === "credit_card"));

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submittingRef.current) return;

    submittingRef.current = true;
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    try {
      setError("");
      await mutation.submit({ source_account_id: String(form.get("source")), destination_account_id: String(form.get("destination")), amount_minor: parseMinorUnits(String(form.get("amount") ?? ""), language), currency: account.currency, financial_date: String(form.get("financial_date")), memo: String(form.get("memo") ?? "") });
      formElement.reset();
      onSuccess();
    } catch (cause) { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof ApiError || cause instanceof NetworkError || !(cause instanceof Error) ? t(language, "unableToTransfer") : cause.message); } finally { submittingRef.current = false; }
  }

  async function retry() {
    if (submittingRef.current || !mutation.hasRetry) return;

    submittingRef.current = true;
    try {
      setError("");
      await mutation.retry();
      onSuccess();
    } catch (cause) { if (cause instanceof ApiError && cause.status === 401) onUnauthorized(); else setError(cause instanceof ApiError || cause instanceof NetworkError || !(cause instanceof Error) ? t(language, "retryFailed") : cause.message); } finally { submittingRef.current = false; }
  }

  if (sourceAccounts.length === 0 || destinations.length < 2) return null;
  return <form className="panel compact-form" onSubmit={submit}>
    <p className="eyebrow">{t(language, "internalTransfer")}</p><h3>{t(language, "moveMoneyOrPayCard")}</h3>
    <label>{t(language, "from")}<select name="source" defaultValue={account.type === "cash" || account.type === "bank" ? account.id : sourceAccounts[0].id}>{sourceAccounts.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>
    <label>{t(language, "to")}<select name="destination" defaultValue={destinations.find((value) => value.id !== account.id)?.id}>{destinations.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}</select></label>
    <label>{t(language, "amount")}<input name="amount" inputMode="decimal" required placeholder="0.00" /></label>
    <label>{t(language, "financialDate")}<input name="financial_date" type="date" required defaultValue={todayFinancialDate()} /></label>
    <label>{t(language, "memo")}<input name="memo" maxLength={500} /></label>
    {error && <p className="form-error">{error}</p>}
    {mutation.hasRetry && <button type="button" className="subtle-button" disabled={mutation.isSubmitting} onClick={() => void retry()}>{t(language, "retryPending")}</button>}
    <button type="submit" disabled={mutation.isSubmitting || mutation.hasRetry}>{mutation.isSubmitting ? t(language, "moving") : mutation.hasRetry ? t(language, "retryPendingFirst") : t(language, "createTransfer")}</button>
  </form>;
}

function TransactionRow({ transaction, currency, language }: { transaction: Transaction; currency: string; language: Language }) {
  const outgoing = transaction.effect === "asset_outflow" || transaction.effect === "liability_payment";
  return <article className={`transaction-row ${outgoing ? "outflow" : "inflow"}`}><time>{formatFinancialDate(transaction.financial_date, language)}</time><div><strong>{transaction.memo || effectText(language, transaction.effect)}</strong><span>{effectText(language, transaction.effect)}{transaction.transfer_id ? ` · ${t(language, "linkedTransfer")}` : ""}</span></div><b>{outgoing ? "−" : "+"}{formatMoney(transaction.amount_minor, currency, language)}</b></article>;
}
