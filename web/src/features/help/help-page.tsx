import { useNavigate } from "react-router";
import { ArrowUpRight, Bell, FileText, Rocket, Search, Server, Webhook } from "lucide-react";
import type { ComponentType } from "react";
import type { Me } from "@/api/types";
import { Page, Section } from "@/components/ui";
import { LearnMore } from "@/components/learn-more";
import { ShortcutList } from "@/components/shell/app-shell";
import { ConnectGuide } from "@/features/projects/connect-guide";
import { LOG_SEARCH_EXAMPLES, type SearchExample } from "@/features/logs/search-bar";
import { TRACE_SEARCH_EXAMPLES } from "@/features/traces/trace-words";
import { GLOSSARY } from "@/lib/glossary";
import { docsUrl, type DocKey } from "@/lib/docs";

const REPO = "https://github.com/koneb71/pyloggle";

const GUIDES: { doc: DocKey; title: string; text: string; icon: ComponentType<{ size?: number; className?: string }> }[] = [
  { doc: "quickStart", title: "Quick start", text: "Pyloggle running and your first error in five minutes.", icon: Rocket },
  { doc: "python", title: "Send data from Python", text: "Logs, errors and context from any Python app.", icon: FileText },
  { doc: "search", title: "Search syntax", text: "Every field and rule for Logs and Requests.", icon: Search },
  { doc: "alerting", title: "Alerts", text: "E-mail, Slack, Telegram, and how often you hear about it.", icon: Bell },
  { doc: "deployment", title: "Run your own server", text: "Docker, the release binary, HTTPS and sign-in.", icon: Server },
  { doc: "httpApi", title: "HTTP API", text: "The same data for scripts, with an access key.", icon: Webhook },
];

/** "How do I…?" in one place: guides, sending data, search, shortcuts, words, and this server. */
export function HelpPage({ me }: { me: Me }) {
  const docs = me.features.docs_url;
  const serverUrl = me.features.public_url || window.location.origin;
  return (
    <Page title="Help" description="How to use Pyloggle, and where to find more." actions={docs && <LearnMore doc="home">All documentation</LearnMore>}>
      <div className="space-y-6">
        {docs && (
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-label="Guides">
            {GUIDES.map((g) => (
              <li key={g.doc}>
                <a
                  href={docsUrl(docs, g.doc) ?? undefined}
                  target="_blank"
                  rel="noreferrer"
                  className="group flex h-full gap-3 rounded-lg border border-border bg-surface p-4 transition-colors hover:border-accent/50 hover:bg-accent-soft/40"
                >
                  <g.icon size={18} className="mt-0.5 flex-none text-accent" />
                  <span className="min-w-0">
                    <span className="flex items-center gap-1 text-[13px] font-semibold text-ink">
                      {g.title}
                      <ArrowUpRight size={13} aria-hidden className="text-ink-3 group-hover:text-accent" />
                    </span>
                    <span className="mt-0.5 block text-[13px] text-ink-3">{g.text}</span>
                  </span>
                </a>
              </li>
            ))}
          </ul>
        )}

        <ConnectGuide serverUrl={serverUrl} projectId={me.project?.id ?? "your-project"} />

        <Section
          title="Search"
          description="One search language works on Logs, Requests and alert rules. Click an example to try it."
          actions={<LearnMore doc="search">Full search syntax</LearnMore>}
        >
          <div className="grid gap-6 md:grid-cols-2">
            <SearchExamples title="On Logs" path="/logs" examples={LOG_SEARCH_EXAMPLES} />
            <SearchExamples title="On Requests" path="/traces" examples={TRACE_SEARCH_EXAMPLES} />
          </div>
        </Section>

        <Section title="Keyboard shortcuts" description="Press ? on any page to see them again.">
          <ShortcutList me={me} />
        </Section>

        <Section title="Words used in Pyloggle" description="The app uses plain words. Here is what each one means, and what engineers call it." flush>
          <table className="w-full text-left text-[13px]">
            <thead className="border-b border-border text-xs text-ink-3">
              <tr>
                <th className="px-4 py-2 font-medium">In the app</th>
                <th className="px-4 py-2 font-medium">Also called</th>
                <th className="px-4 py-2 font-medium">What it means</th>
              </tr>
            </thead>
            <tbody>
              {Object.values(GLOSSARY).map((t) => (
                <tr key={t.label + t.technical} className="border-b border-border last:border-0">
                  <td className="px-4 py-2 align-top font-medium text-ink">{t.label}</td>
                  <td className="px-4 py-2 align-top text-ink-3">{t.technical}</td>
                  <td className="px-4 py-2 align-top text-ink-2">{t.explain}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Section>

        <Section title="About this server">
          <dl className="grid grid-cols-[max-content_1fr] gap-x-8 gap-y-2 text-[13px]">
            <dt className="text-ink-3">Version</dt>
            <dd className="font-mono text-[12px] text-ink">{me.version || "unknown"}</dd>
            <dt className="text-ink-3">Sign-in</dt>
            <dd className="text-ink">{signInWords(me)}</dd>
            <dt className="text-ink-3">You can</dt>
            <dd className="text-ink">{me.is_admin ? "Change projects, access keys and alert rules" : "Look at everything and work on issues"}</dd>
          </dl>
          <ul className="mt-4 flex flex-wrap gap-x-5 gap-y-2 text-[13px]">
            <li>
              <ExternalA href={`${REPO}/issues/new/choose`}>Report a problem</ExternalA>
            </li>
            <li>
              <ExternalA href={`${REPO}/blob/main/CHANGELOG.md`}>What's new</ExternalA>
            </li>
            <li>
              <ExternalA href={REPO}>Source code</ExternalA>
            </li>
            <li>
              <ExternalA href="/ui/licenses.txt">Open-source licenses</ExternalA>
            </li>
          </ul>
          <p className="mt-3 text-xs text-ink-3">Pyloggle is open source under the Apache License 2.0.</p>
        </Section>
      </div>
    </Page>
  );
}

function SearchExamples({ title, path, examples }: { title: string; path: string; examples: SearchExample[] }) {
  const nav = useNavigate();
  return (
    <div>
      <h3 className="mb-2 text-[13px] font-semibold text-ink">{title}</h3>
      <ul className="space-y-1">
        {examples.map((ex) => (
          <li key={ex.query}>
            <button
              type="button"
              onClick={() => nav(`${path}?from=-24h&q=${encodeURIComponent(ex.query)}`)}
              className="flex w-full items-baseline gap-3 rounded-md px-2 py-1.5 text-left hover:bg-surface-2"
            >
              <code className="flex-none font-mono text-[12px] text-accent">{ex.query}</code>
              <span className="min-w-0 text-[13px] text-ink-3">{ex.says}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ExternalA({ href, children }: { href: string; children: string }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-accent hover:underline">
      {children}
      <ArrowUpRight size={12} aria-hidden />
    </a>
  );
}

/** How people sign in to this server, in words. */
export function signInWords(me: Pick<Me, "auth_mode" | "features">): string {
  switch (me.auth_mode) {
    case "azure":
      return "With Microsoft accounts";
    case "oidc":
      return me.features.auth_provider ? `With ${me.features.auth_provider} accounts` : "With company accounts";
    case "disabled":
      return "Off: anyone who can reach this server can use it";
  }
  return me.auth_mode;
}
