"use client";

import { FormEvent, useState } from "react";

import { ApiError, api, type User } from "../../lib/api";

export function LoginForm({ onAuthenticated }: { onAuthenticated: (user: User) => void }) {
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
      setError(cause instanceof ApiError && cause.status === 401 ? "Invalid email or password." : "Unable to sign in right now.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <main className="login-shell">
      <form className="login-card" onSubmit={submit}>
        <p className="brand">Runway</p>
        <h1>See the room in your money.</h1>
        <p className="muted">Sign in to view your current runway and upcoming commitments.</p>
        <label>
          Email
          <input autoComplete="email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required />
        </label>
        <label>
          Password
          <input autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required />
        </label>
        {error && <p className="form-error" role="alert">{error}</p>}
        <button type="submit" disabled={isSubmitting}>{isSubmitting ? "Signing in…" : "Sign in"}</button>
      </form>
    </main>
  );
}
