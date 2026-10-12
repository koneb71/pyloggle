import { useState, type FormEvent } from "react";
import { useLocation, useNavigate } from "react-router";
import { KeyRound, LogIn } from "lucide-react";
import { useLogin } from "@/api/hooks";
import type { Me } from "@/api/types";
import { ApiRequestError } from "@/api/client";
import { Button, Field, Input, buttonClass } from "@/components/ui";

const ERR_WORDS: Record<string, string> = {
  invalid_api_key: "That access key isn't valid. Check it and try again.",
  insufficient_scope: "That key can only send data. Sign in with a key that can view (read).",
};

/**
 * Sign in. `loginUrl` comes from the server's 401: when it is set the server signs people in
 * with a company account (an expired session lands here), so offer that instead of a key form.
 */
export function LoginPage({ me, loginUrl }: { me?: Me; loginUrl?: string }) {
  const login = useLogin();
  const nav = useNavigate();
  const loc = useLocation();
  const [key, setKey] = useState("");
  const params = new URLSearchParams(loc.search);
  const next = params.get("next") || "/overview";
  const err = params.get("err");
  const sso = me?.auth_mode === "azure" || me?.auth_mode === "oidc" || !!loginUrl;
  const provider = me?.features.auth_provider || (me?.auth_mode === "azure" ? "Microsoft" : "your company account");

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!key.trim()) return;
    login.mutate(key.trim(), { onSuccess: () => nav(next.startsWith("/") ? next : "/overview", { replace: true }) });
  };
  const loginError = login.error instanceof ApiRequestError ? (ERR_WORDS[login.error.body.error] ?? login.error.message) : login.error ? "Couldn't sign in. Check your connection and try again." : null;
  const shownError = loginError ?? (err ? (ERR_WORDS[err] ?? err) : null);

  return (
    <div className="flex h-full items-center justify-center bg-bg p-6">
      <div className="w-full max-w-[380px]">
        <div className="mb-6 flex flex-col items-center text-center">
          <svg width="40" height="40" viewBox="0 0 24 24" aria-hidden>
            <rect x="2" y="2" width="20" height="20" rx="6" fill="var(--accent-strong)" />
            <rect x="6" y="13" width="3" height="5" rx="1" fill="#fff" opacity=".7" />
            <rect x="10.5" y="9" width="3" height="9" rx="1" fill="#fff" opacity=".85" />
            <rect x="15" y="6" width="3" height="12" rx="1" fill="#fff" />
          </svg>
          <h1 className="mt-4 text-xl font-semibold tracking-tight text-ink">{loginUrl && !me ? "Please sign in again" : "Sign in to Pyloggle"}</h1>
          <p className="mt-1 text-[13px] text-ink-3">See what your apps are doing: errors, messages and requests in one place.</p>
        </div>
        <div className="rounded-xl border border-border bg-surface p-6 shadow-[var(--shadow)]">
          {shownError && (
            <div role="alert" className="mb-4 rounded-md border border-critical/30 bg-critical/[0.06] px-3 py-2 text-[13px] text-critical-text">
              {shownError}
            </div>
          )}
          {sso ? (
            <div className="space-y-3">
              <a href={`${loginUrl || "/ui/login"}?next=${encodeURIComponent("/ui" + next)}`} className={buttonClass({ variant: "primary", size: "md", className: "w-full" })}>
                <LogIn size={16} /> Sign in with {provider}
              </a>
              <p className="text-center text-xs text-ink-3">You'll come back here after signing in.</p>
            </div>
          ) : (
            <form onSubmit={submit} className="space-y-4">
              <Field label="Access key" hint="A project key that can view data. It's swapped for a sign-in cookie and never stored in the browser.">
                <Input autoFocus type="password" placeholder="pl_…" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" aria-invalid={!!loginError || undefined} />
              </Field>
              <Button type="submit" variant="primary" size="md" className="w-full" loading={login.isPending} disabled={!key.trim()} icon={<KeyRound size={15} />}>
                Sign in
              </Button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
