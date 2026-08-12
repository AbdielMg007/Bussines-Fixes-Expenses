"use client";

import { FormEvent, useState } from "react";

import { ApiError, api, type User } from "../../lib/api";
import { t, type Language } from "../../lib/i18n";

export function LoginForm({ onAuthenticated, language }: { onAuthenticated: (user: User) => void; language: Language }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setIsSubmitting(true);
    try {
      const user = await api.login(email, password);
      setPassword("");
      onAuthenticated(user);
    } catch (cause) {
      setError(cause instanceof ApiError && cause.status === 401 ? t(language, "invalidCredentials") : t(language, "signInUnavailable"));
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <main className="login-shell">
      <form className="login-card" onSubmit={submit}>
        <p className="brand">Runway</p>
        <h1>{t(language, "signInTitle")}</h1>
        <p className="muted">{t(language, "signInDescription")}</p>
        <label>
          {t(language, "email")}
          <input autoComplete="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required />
        </label>
        <label>
          {t(language, "password")}
          <input autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required />
        </label>
        {error && <p className="form-error" role="alert">{error}</p>}
        <button type="submit" disabled={isSubmitting}>{isSubmitting ? t(language, "signingIn") : t(language, "signIn")}</button>
      </form>
    </main>
  );
}
