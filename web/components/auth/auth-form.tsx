"use client";
import { useState } from "react";
import { useSearchParams } from "next/navigation";
import { authClient } from "@/lib/auth-client";
import { sanitiseRedirectPath } from "@/lib/app-origin";

export function AuthForm() {
  const search = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <form
      className="space-y-6"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError("");
        try {
          const result = await authClient.signIn.email({ email, password });
          if (result.error)
            throw new Error(result.error.message ?? "Sign-in failed");
          window.location.assign(
            sanitiseRedirectPath(search.get("redirect_url")),
          );
        } catch (err) {
          setError(err instanceof Error ? err.message : "Sign-in failed");
          setBusy(false);
        }
      }}
    >
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">
          Sign in to Fact0
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Use the owner account created for this installation.
        </p>
      </div>
      <label className="block space-y-2 text-sm">
        Email
        <input
          type="email"
          autoComplete="username"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          className="block w-full rounded-lg border bg-background p-3"
        />
      </label>
      <label className="block space-y-2 text-sm">
        Password
        <input
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="block w-full rounded-lg border bg-background p-3"
        />
      </label>
      {error && (
        <p role="alert" className="text-sm text-red-600">
          {error}
        </p>
      )}
      <button
        disabled={busy}
        className="w-full rounded-lg bg-foreground px-5 py-3 text-sm font-semibold text-background disabled:opacity-50"
      >
        {busy ? "Signing in…" : "Sign in"}
      </button>
      <p className="text-xs text-muted-foreground">
        No public signup. Create or reset the owner with the local setup command
        in the installation guide.
      </p>
    </form>
  );
}
