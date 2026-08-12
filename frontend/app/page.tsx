"use client";

import { useCallback, useEffect, useState } from "react";

import { api, type Account, type User } from "../lib/api";
import { defaultLanguage, languageStorageKey, readLanguage, t, type Language } from "../lib/i18n";
import { Accounts } from "./components/accounts";
import { Dashboard } from "./components/dashboard";
import { LoginForm } from "./components/login-form";

type View = "dashboard" | "accounts";
type AccountLoad = { accounts: Account[]; error: boolean };

export default function Home() {
  const [user, setUser] = useState<User | null>(null);
  const [checkingSession, setCheckingSession] = useState(true);
  const [language, setLanguage] = useState<Language>(defaultLanguage);

  useEffect(() => {
    let preference = defaultLanguage;
    try { preference = readLanguage(window.localStorage.getItem(languageStorageKey)); } catch { /* Browser storage may be unavailable. */ }
    setLanguage(preference);
    document.documentElement.lang = preference;
  }, []);

  function changeLanguage(next: Language) {
    setLanguage(next);
    try { window.localStorage.setItem(languageStorageKey, next); } catch { /* Keep the in-memory preference when storage is unavailable. */ }
    document.documentElement.lang = next;
  }

  useEffect(() => {
    let live = true;
    void api.me().then((identity) => { if (live) setUser(identity); }).catch(() => undefined).finally(() => { if (live) setCheckingSession(false); });
    return () => { live = false; };
  }, []);

  if (checkingSession) return <main className="login-shell"><p className="loading-mark">{t(language, "loadingRunway")}</p></main>;
  if (!user) return <LoginForm language={language} onAuthenticated={setUser} />;
  return <AuthenticatedRunway language={language} onLanguageChange={changeLanguage} user={user} onLoggedOut={() => setUser(null)} />;
}

function AuthenticatedRunway({ user, onLoggedOut, language, onLanguageChange }: { user: User; onLoggedOut: () => void; language: Language; onLanguageChange: (language: Language) => void }) {
  const [view, setView] = useState<View>("dashboard");
  const [refreshToken, setRefreshToken] = useState(0);
  const [accountLoad, setAccountLoad] = useState<AccountLoad>({ accounts: [], error: false });
  const [loadingAccounts, setLoadingAccounts] = useState(true);

  const refreshAccounts = useCallback(async () => {
    setLoadingAccounts(true);
    try {
      setAccountLoad({ accounts: await api.listAccounts(), error: false });
    } catch (cause) {
      if (cause instanceof Error && "status" in cause && cause.status === 401) onLoggedOut();
      else setAccountLoad({ accounts: [], error: true });
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
    <header className="app-header"><button className="wordmark" onClick={() => setView("dashboard")}>Runway</button><nav><button className={view === "dashboard" ? "active" : ""} onClick={() => setView("dashboard")}>{t(language, "dashboard")}</button><button className={view === "accounts" ? "active" : ""} onClick={() => setView("accounts")}>{t(language, "accounts")}</button></nav><div className="user-menu"><label className="language-selector"><span className="sr-only">{t(language, "language")}</span><select aria-label={t(language, "language")} value={language} onChange={(event) => onLanguageChange(event.target.value as Language)}><option value="es">ES</option><option value="en">EN</option></select></label><span>{user.email}</span><button className="subtle-button" onClick={() => void logout()}>{t(language, "logOut")}</button></div></header>
    <div className="app-content">
      {accountLoad.error && <section className="panel state-panel"><p className="form-error">{t(language, "unableToLoadProjection")}</p><button onClick={() => void refreshAccounts()}>{t(language, "retry")}</button></section>}
      {!accountLoad.error && loadingAccounts && <section className="panel state-panel">{t(language, "loadingAccounts")}</section>}
      {!accountLoad.error && !loadingAccounts && view === "dashboard" && <Dashboard language={language} accounts={accountLoad.accounts} refreshToken={refreshToken} onDataChanged={onDataChanged} onUnauthorized={onLoggedOut} />}
      {!accountLoad.error && !loadingAccounts && view === "accounts" && <Accounts language={language} accounts={accountLoad.accounts} refreshToken={refreshToken} onDataChanged={onDataChanged} onUnauthorized={onLoggedOut} />}
    </div>
  </main>;
}
