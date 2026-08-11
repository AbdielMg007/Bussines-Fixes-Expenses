"use client";

import { useCallback, useEffect, useState } from "react";

import { api, type Account, type User } from "../lib/api";
import { Accounts } from "./components/accounts";
import { Dashboard } from "./components/dashboard";
import { LoginForm } from "./components/login-form";

type View = "dashboard" | "accounts";
type AccountLoad = { accounts: Account[]; error: string };

export default function Home() {
  const [user, setUser] = useState<User | null>(null);
  const [checkingSession, setCheckingSession] = useState(true);

  useEffect(() => {
    let live = true;
    void api.me().then((identity) => { if (live) setUser(identity); }).catch(() => undefined).finally(() => { if (live) setCheckingSession(false); });
    return () => { live = false; };
  }, []);

  if (checkingSession) return <main className="login-shell"><p className="loading-mark">Loading Runway…</p></main>;
  if (!user) return <LoginForm onAuthenticated={setUser} />;
  return <AuthenticatedRunway user={user} onLoggedOut={() => setUser(null)} />;
}

function AuthenticatedRunway({ user, onLoggedOut }: { user: User; onLoggedOut: () => void }) {
  const [view, setView] = useState<View>("dashboard");
  const [refreshToken, setRefreshToken] = useState(0);
  const [accountLoad, setAccountLoad] = useState<AccountLoad>({ accounts: [], error: "" });
  const [loadingAccounts, setLoadingAccounts] = useState(true);

  const refreshAccounts = useCallback(async () => {
    setLoadingAccounts(true);
    try {
      setAccountLoad({ accounts: await api.listAccounts(), error: "" });
    } catch (cause) {
      if (cause instanceof Error && "status" in cause && cause.status === 401) onLoggedOut();
      else setAccountLoad({ accounts: [], error: cause instanceof Error ? cause.message : "Unable to load accounts." });
    } finally {
      setLoadingAccounts(false);
    }
  }, [onLoggedOut]);

  useEffect(() => { void refreshAccounts(); }, [refreshAccounts, refreshToken]);
  const onDataChanged = useCallback(() => setRefreshToken((value) => value + 1), []);

  async function logout() {
    try { await api.logout(); } finally { onLoggedOut(); }
  }

  return <main className="app-shell">
    <header className="app-header"><button className="wordmark" onClick={() => setView("dashboard")}>Runway</button><nav><button className={view === "dashboard" ? "active" : ""} onClick={() => setView("dashboard")}>Dashboard</button><button className={view === "accounts" ? "active" : ""} onClick={() => setView("accounts")}>Accounts</button></nav><div className="user-menu"><span>{user.email}</span><button className="subtle-button" onClick={() => void logout()}>Log out</button></div></header>
    <div className="app-content">
      {accountLoad.error && <section className="panel state-panel"><p className="form-error">{accountLoad.error}</p><button onClick={() => void refreshAccounts()}>Try again</button></section>}
      {!accountLoad.error && loadingAccounts && <section className="panel state-panel">Loading accounts…</section>}
      {!accountLoad.error && !loadingAccounts && view === "dashboard" && <Dashboard accounts={accountLoad.accounts} refreshToken={refreshToken} onDataChanged={onDataChanged} onUnauthorized={onLoggedOut} />}
      {!accountLoad.error && !loadingAccounts && view === "accounts" && <Accounts accounts={accountLoad.accounts} refreshToken={refreshToken} onDataChanged={onDataChanged} onUnauthorized={onLoggedOut} />}
    </div>
  </main>;
}
