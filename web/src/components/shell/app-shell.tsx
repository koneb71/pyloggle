import { useEffect, useState, type ComponentType } from "react";
import { NavLink, Outlet, useNavigate } from "react-router";
import clsx from "clsx";
import { Activity, AlertOctagon, Bell, Check, ChevronsUpDown, FolderKanban, GitBranch, Home, Keyboard, LayoutDashboard, LogOut, Moon, PanelLeftClose, PanelLeftOpen, ScrollText, Sun, SunMoon } from "lucide-react";
import type { Me } from "@/api/types";
import { useLogout, useProjectMutations } from "@/api/hooks";
import { Dialog, IconButton, Kbd, MenuItem, Popover, Tooltip } from "@/components/ui";
import { useTheme } from "./theme";
import { useMediaQuery } from "@/lib/use-media";

interface NavItem {
  to: string;
  label: string;
  hint: string;
  icon: ComponentType<{ size?: number; className?: string }>;
  key: string;
  show?: (me: Me) => boolean;
}

/** Navigation grouped by what people come to do: look into problems, keep watch, set things up. */
const NAV: { title?: string; items: NavItem[] }[] = [
  { items: [{ to: "/overview", label: "Overview", hint: "Is everything OK right now?", icon: Home, key: "o" }] },
  {
    title: "Investigate",
    items: [
      { to: "/issues", label: "Issues", hint: "Errors grouped by cause", icon: AlertOctagon, key: "i" },
      { to: "/logs", label: "Logs", hint: "Every message your apps wrote", icon: ScrollText, key: "l" },
      { to: "/traces", label: "Requests", hint: "Each request step by step (traces)", icon: GitBranch, key: "t", show: (me) => me.features.traces !== false },
    ],
  },
  {
    title: "Monitor",
    items: [
      { to: "/dashboards", label: "Dashboards", hint: "How the project is doing over time", icon: LayoutDashboard, key: "d" },
      { to: "/alerts", label: "Alerts", hint: "Get told when something goes wrong", icon: Bell, key: "a", show: (me) => !!me.features.alerting || me.is_admin },
    ],
  },
  {
    title: "Settings",
    items: [
      { to: "/projects", label: "Projects", hint: "Projects, access keys and storage", icon: FolderKanban, key: "p" },
      { to: "/system", label: "System", hint: "Health of the Pyloggle server", icon: Activity, key: "s", show: (me) => me.is_admin },
    ],
  },
];

const ALL_ITEMS = NAV.flatMap((g) => g.items);

export function AppShell({ me }: { me: Me }) {
  const [pref, setPref] = useState<"open" | "collapsed">(() => {
    try {
      return localStorage.getItem("pyloggle.sidebar") === "collapsed" ? "collapsed" : "open";
    } catch {
      return "open";
    }
  });
  // Below 1200px the sidebar starts as icons; opening it there is not remembered.
  const narrow = useMediaQuery("(max-width: 1199px)");
  const [narrowOpen, setNarrowOpen] = useState(false);
  const collapsed = narrow ? !narrowOpen : pref === "collapsed";
  const toggle = () => {
    if (narrow) return setNarrowOpen(!narrowOpen);
    const next = pref === "collapsed" ? "open" : "collapsed";
    setPref(next);
    try {
      localStorage.setItem("pyloggle.sidebar", next);
    } catch {
      /* ignore */
    }
  };
  const [help, setHelp] = useState(false);
  const nav = useNavigate();

  // "g" then a letter jumps to a page (like GitHub); "?" lists the shortcuts.
  useEffect(() => {
    let pendingG = 0;
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return;
      if (e.key === "?") {
        e.preventDefault();
        setHelp(true);
        return;
      }
      if (e.key === "g") {
        pendingG = Date.now();
        return;
      }
      if (pendingG && Date.now() - pendingG < 1200) {
        const item = ALL_ITEMS.find((n) => n.key === e.key && (!n.show || n.show(me)));
        if (item) {
          e.preventDefault();
          nav(item.to);
        }
        pendingG = 0;
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [nav, me]);

  return (
    <div className="flex h-full min-h-0">
      <aside className={clsx("flex h-full flex-none flex-col border-r border-border bg-surface transition-[width] duration-150", collapsed ? "w-14" : "w-60")} aria-label="Main navigation">
        <div className={clsx("flex h-14 flex-none items-center", collapsed ? "justify-center" : "gap-2.5 px-4")}>
          <Logo />
          {!collapsed && <span className="text-[15px] font-semibold tracking-tight text-ink">Pyloggle</span>}
        </div>
        {!collapsed && <ProjectSwitcher me={me} />}
        <nav className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-2 py-3">
          {NAV.map((group, gi) => {
            const items = group.items.filter((n) => !n.show || n.show(me));
            if (items.length === 0) return null;
            return (
              <div key={gi} className="flex flex-col gap-0.5">
                {group.title && !collapsed && <div className="px-2.5 pb-1 text-xs font-medium text-ink-3">{group.title}</div>}
                {group.title && collapsed && <div className="mx-3 mb-1 h-px bg-border" aria-hidden />}
                {items.map((n) => (
                  <NavEntry key={n.to} item={n} collapsed={collapsed} />
                ))}
              </div>
            );
          })}
        </nav>
        <div className={clsx("flex flex-none items-center gap-0.5 border-t border-border p-2", collapsed && "flex-col")}>
          <UserMenu me={me} collapsed={collapsed} />
          <IconButton label="Keyboard shortcuts (?)" icon={<Keyboard size={15} />} onClick={() => setHelp(true)} />
          <ThemeToggle />
          <IconButton label={collapsed ? "Expand sidebar" : "Collapse sidebar"} onClick={toggle} icon={collapsed ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />} />
        </div>
      </aside>
      <main className="flex min-h-0 min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
      <ShortcutsDialog open={help} onClose={() => setHelp(false)} me={me} />
    </div>
  );
}

function NavEntry({ item: n, collapsed }: { item: NavItem; collapsed: boolean }) {
  const link = (
    <NavLink
      to={n.to}
      aria-label={collapsed ? n.label : undefined}
      className={({ isActive }) =>
        clsx(
          "flex h-8 items-center gap-2.5 rounded-md px-2.5 text-[13px] font-medium transition-colors",
          isActive ? "bg-accent-soft text-accent" : "text-ink-2 hover:bg-surface-2 hover:text-ink",
          collapsed && "justify-center px-0",
        )
      }
    >
      <n.icon size={16} className="flex-none" />
      {!collapsed && <span className="truncate">{n.label}</span>}
    </NavLink>
  );
  return (
    <Tooltip
      content={
        <>
          <span className="font-medium">{n.label}</span> · {n.hint}
          <span className="ml-1 opacity-70">(g {n.key})</span>
        </>
      }
      delay={collapsed ? 100 : 700}
    >
      {link}
    </Tooltip>
  );
}

function ShortcutsDialog({ open, onClose, me }: { open: boolean; onClose: () => void; me: Me }) {
  const rows: [string[], string][] = [
    ...ALL_ITEMS.filter((n) => !n.show || n.show(me)).map((n): [string[], string] => [["g", n.key], `Go to ${n.label}`]),
    [["/"], "Search (on Logs and Requests)"],
    [["j", "k"], "Next and previous message (on Logs)"],
    [["Esc"], "Close a panel or dialog"],
    [["?"], "Show these shortcuts"],
  ];
  return (
    <Dialog open={open} onClose={onClose} title="Keyboard shortcuts">
      <dl className="grid grid-cols-[max-content_1fr] items-center gap-x-6 gap-y-2 text-[13px]">
        {rows.map(([keys, what]) => (
          <div key={what} className="contents">
            <dt className="flex items-center gap-1">
              {keys.map((k, i) => (
                <span key={k} className="flex items-center gap-1">
                  {i > 0 && <span className="text-xs text-ink-3">{keys[0] === "g" ? "then" : "/"}</span>}
                  <Kbd>{k}</Kbd>
                </span>
              ))}
            </dt>
            <dd className="text-ink-2">{what}</dd>
          </div>
        ))}
      </dl>
    </Dialog>
  );
}

function Logo() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" aria-hidden className="flex-none">
      <rect x="2" y="2" width="20" height="20" rx="6" fill="var(--accent-strong)" />
      <rect x="6" y="13" width="3" height="5" rx="1" fill="#fff" opacity=".7" />
      <rect x="10.5" y="9" width="3" height="9" rx="1" fill="#fff" opacity=".85" />
      <rect x="15" y="6" width="3" height="12" rx="1" fill="#fff" />
    </svg>
  );
}

function ProjectSwitcher({ me }: { me: Me }) {
  const { select } = useProjectMutations();
  const current = me.project;
  return (
    <div className="flex-none px-2 pb-1">
      <Popover
        width={260}
        anchorClassName="flex w-full"
        trigger={({ toggle, open }) => (
          <button type="button" onClick={toggle} aria-label="Switch project" className={clsx("flex h-9 w-full items-center gap-2 rounded-md border border-border-strong px-2 text-left transition-colors hover:bg-surface-2", open && "bg-surface-2")}>
            <span className="flex h-5 w-5 flex-none items-center justify-center rounded bg-surface-3 text-[11px] font-semibold uppercase text-ink-2">{(current?.name ?? "?").slice(0, 1)}</span>
            <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-ink">{current?.name ?? "Choose a project"}</span>
            <ChevronsUpDown size={14} className="flex-none text-ink-3" />
          </button>
        )}
      >
        {(close) => (
          <div className="max-h-80 overflow-y-auto">
            <div className="px-2 pb-1 pt-1.5 text-xs font-medium text-ink-3">Switch project</div>
            {me.projects.length === 0 && <div className="px-2 py-1.5 text-[13px] text-ink-3">No projects yet</div>}
            {me.projects.map((p) => (
              <MenuItem
                key={p.id}
                icon={p.id === current?.id ? <Check size={14} className="text-accent" /> : <span className="inline-block w-[14px]" />}
                onClick={() => {
                  close();
                  if (p.id !== current?.id) select.mutate(p.id);
                }}
              >
                <span className="min-w-0 flex-1 truncate">{p.name}</span>
                <span className="ml-2 truncate font-mono text-[11px] text-ink-3">{p.id}</span>
              </MenuItem>
            ))}
            <div className="my-1 h-px bg-border" />
            <NavLink to="/projects" onClick={close} className="block rounded px-2 py-1.5 text-[13px] text-ink-2 hover:bg-surface-2 hover:text-ink">
              Manage projects…
            </NavLink>
          </div>
        )}
      </Popover>
    </div>
  );
}

function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const next = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
  const Icon = theme === "dark" ? Moon : theme === "light" ? Sun : SunMoon;
  const names = { system: "same as your device", light: "light", dark: "dark" };
  return <IconButton label={`Appearance: ${names[theme]}. Click for ${names[next]}.`} onClick={() => setTheme(next)} icon={<Icon size={15} />} />;
}

function signInWords(me: Me): string {
  switch (me.auth_mode) {
    case "disabled":
      return "Sign-in is turned off on this server";
    case "azure":
      return "Signed in with Microsoft";
    case "oidc":
      return `Signed in with ${me.features.auth_provider || "your company account"}`;
    default:
      return "Signed in with an access key";
  }
}

function UserMenu({ me, collapsed }: { me: Me; collapsed: boolean }) {
  const logout = useLogout();
  const nav = useNavigate();
  const name = me.user?.name || me.user?.email || (me.auth_mode === "disabled" ? "Local user" : "Signed in");
  const signOut = () => logout.mutate(undefined, { onSuccess: () => nav("/login", { replace: true }) });
  return (
    <Popover
      width={248}
      anchorClassName={collapsed ? "inline-flex" : "flex min-w-0 flex-1"}
      trigger={({ toggle }) => (
        <button type="button" onClick={toggle} aria-label="Account" className={clsx("flex min-w-0 items-center gap-2 rounded-md text-left hover:bg-surface-2", collapsed ? "h-8 w-8 justify-center" : "h-8 flex-1 px-1.5")}>
          <span className="flex h-6 w-6 flex-none items-center justify-center rounded-full bg-accent-soft text-[11px] font-semibold uppercase text-accent">{name.slice(0, 1)}</span>
          {!collapsed && <span className="min-w-0 flex-1 truncate text-[13px] text-ink-2">{name}</span>}
        </button>
      )}
    >
      {(close) => (
        <div>
          <div className="px-2 py-2">
            <div className="truncate text-[13px] font-medium text-ink">{name}</div>
            {me.user?.email && me.user.email !== name && <div className="truncate text-xs text-ink-3">{me.user.email}</div>}
            <div className="mt-1 text-xs text-ink-3">
              {signInWords(me)}
              {me.is_admin ? " · Administrator" : ""}
            </div>
          </div>
          {me.auth_mode !== "disabled" && (
            <>
              <div className="my-1 h-px bg-border" />
              <MenuItem
                icon={<LogOut size={14} />}
                onClick={() => {
                  close();
                  signOut();
                }}
              >
                Sign out
              </MenuItem>
            </>
          )}
        </div>
      )}
    </Popover>
  );
}

/** Legacy page header strip, kept until every page has moved to <Page>. */
export function PageHeader({ title, children, sub }: { title: React.ReactNode; children?: React.ReactNode; sub?: React.ReactNode }) {
  return (
    <header className="flex min-h-14 flex-none items-center justify-between gap-3 border-b border-border bg-surface px-5 py-2.5">
      <div className="min-w-0">
        <h1 className="truncate text-lg font-semibold tracking-tight text-ink">{title}</h1>
        {sub && <div className="truncate text-[13px] text-ink-3">{sub}</div>}
      </div>
      <div className="flex flex-none items-center gap-2">{children}</div>
    </header>
  );
}
