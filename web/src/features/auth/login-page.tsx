import { useState, type FormEvent } from "react";
import { useLocation, useNavigate } from "react-router";
import { KeyRound, LogIn } from "lucide-react";
import { useLogin } from "@/api/hooks";
import type { Me } from "@/api/types";
import { Button, Field, Input } from "@/components/ui";

export function LoginPage({ me }: { me?: Me }) {
  const login = useLogin();
  const nav = useNavigate();
  const loc = useLocation();
  const [key, setKey] = useState("");
  const next = new URLSearchParams(loc.search).get("next") || "/issues";
  const err = new URLSearchParams(loc.search).get("err");
  const azure = me?.auth_mode === "azure";

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!key.trim()) return;
    login.mutate(key.trim(), { onSuccess: () => nav(next.startsWith("/") ? next : "/issues", { replace: true }) });
  };

  return (
    <div className="flex h-full items-center justify-center bg-bg p-6">
      <div className="w-full max-w-sm rounded-xl border border-border bg-surface p-6 shadow-[var(--shadow)]">
        <div className="mb-5 flex items-center gap-2">
          <svg width="26" height="26" viewBox="0 0 24 24" aria-hidden>
            <rect x="2" y="2" width="20" height="20" rx="5" fill="var(--accent)" />
            <rect x="6" y="13" width="3" height="5" rx="1" fill="#fff" opacity=".7" />
            <rect x="10.5" y="9" width="3" height="9" rx="1" fill="#fff" opacity=".85" />
            <rect x="15" y="6" width="3" height="12" rx="1" fill="#fff" />
          </svg>
          <div>
            <div className="text-[15px] font-semibold text-ink">pyloggle</div>
            <div className="text-xs text-ink-3">logs · errors · traces</div>
          </div>
        </div>
        {azure ? (
          <div className="space-y-3">
            <p className="text-[13px] text-ink-2">Sign in with your organization account to continue.</p>
            <a href={`/ui/login?next=${encodeURIComponent("/ui" + next)}`} className="inline-flex h-9 w-full items-center justify-center gap-2 rounded-md bg-accent text-[13px] font-medium text-accent-ink hover:brightness-110">
              <LogIn size={14} /> Sign in with Microsoft
            </a>
          </div>
        ) : (
          <form onSubmit={submit} className="space-y-3">
            <Field label="Project API key" hint="The key is exchanged for a session cookie and never stored in the browser.">
              <Input autoFocus type="password" placeholder="pl_…" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" />
            </Field>
            {(login.isError || err) && <div className="text-xs text-critical">{login.error instanceof Error ? login.error.message : "Invalid API key"}</div>}
            <Button type="submit" variant="primary" size="md" className="w-full" loading={login.isPending} icon={<KeyRound size={14} />}>
              Sign in
            </Button>
          </form>
        )}
      </div>
    </div>
  );
}
