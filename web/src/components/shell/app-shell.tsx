import { useEffect, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router";
import clsx from "clsx";
import { Activity, AlertOctagon, Bell, ChevronsUpDown, FolderKanban, GitBranch, LayoutDashboard, LogOut, Moon, PanelLeftClose, PanelLeftOpen, ScrollText, Sun, SunMoon, Check } from "lucide-react";
import type { Me } from "@/api/types";
import { useLogout, useProjectMutations } from "@/api/hooks";
import { Button, Kbd, MenuItem, Popover } from "@/components/ui";
import { useTheme } from "./theme";

const NAV = [
  { to: "/issues", label: "Issues", icon: AlertOctagon, key: "i" },
  { to: "/logs", label: "Logs", icon: ScrollText, key: "l" },
  { to: "/traces", label: "Traces", icon: GitBranch, key: "t" },
  { to: "/dashboards", label: "Dashboards", icon: LayoutDashboard, key: "d" },
  { to: "/alerts", label: "Alerts", icon: Bell, key: "a" },
  { to: "/projects", label: "Projects", icon: FolderKanban, key: "p" },
  { to: "/system", label: "System", icon: Activity, key: "s" },
];

export function AppShell({ me }: { me: Me }) {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem("pyloggle.sidebar") === "collapsed";
    } catch {
      return false;
    }
  });
  const nav = useNavigate();
  useEffect(() => {
    try {
      localStorage.setItem("pyloggle.sidebar", collapsed ? "collapsed" : "open");
    } catch {
      /* ignore */
    }
  }, [collapsed]);

  // g then <key> navigation, like GitHub.
  useEffect(() => {
    let pendingG = 0;
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
      if (e.key === "g") {
        pendingG = Date.now();
        return;
      }
      if (pendingG && Date.now() - pendingG < 1200) {
        const item = NAV.find((n) => n.key === e.key);
        if (item) {
          e.preventDefault();
          nav(item.to);
        }
        pendingG = 0;
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [nav]);

  return (
    <div className="flex h-full min-h-0">
      <aside className={clsx("flex h-full flex-none flex-col border-r border-border bg-surface transition-[width]", collapsed ? "w-12" : "w-52")}>
        <div className={clsx("flex h-12 items-center border-b border-border", collapsed ? "justify-center" : "gap-2 px-3")}>
          <Logo />
          {!collapsed && <span className="text-[14px] font-semibold tracking-tight text-ink">pyloggle</span>}
        </div>
        {!collapsed && <ProjectSwitcher me={me} />}
        <nav className="flex flex-1 flex-col gap-0.5 p-1.5">
          {NAV.filter((n) => (n.to !== "/alerts" || me.features.alerting || me.is_admin) && (n.to !== "/traces" || me.features.traces !== false) && (n.to !== "/system" || me.is_admin)).map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              title={collapsed ? n.label : `${n.label} (g ${n.key})`}
              className={({ isActive }) =>
                clsx(
                  "flex items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] font-medium transition-colors",
                  isActive ? "bg-accent-soft text-accent" : "text-ink-2 hover:bg-surface-2 hover:text-ink",
                  collapsed && "justify-center px-0",
                )
              }
            >
              <n.icon size={16} className="flex-none" />
              {!collapsed && <span className="flex-1">{n.label}</span>}
              {!collapsed && <Kbd>g {n.key}</Kbd>}
            </NavLink>
          ))}
        </nav>
        <div className={clsx("flex items-center gap-1 border-t border-border p-1.5", collapsed ? "flex-col" : "")}>
          <ThemeToggle />
          <UserMenu me={me} collapsed={collapsed} />
          <Button variant="ghost" className="px-1.5" title={collapsed ? "Expand sidebar" : "Collapse sidebar"} aria-label="Toggle sidebar" onClick={() => setCollapsed(!collapsed)} icon={collapsed ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />} />
        </div>
      </aside>
      <main className="flex min-h-0 min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  );
}

function Logo() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden>
      <rect x="2" y="2" width="20" height="20" rx="5" fill="var(--accent)" />
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
    <div className="px-1.5 pt-1.5">
      <Popover
        width={240}
        trigger={({ toggle, open }) => (
          <button type="button" onClick={toggle} className={clsx("flex w-full items-center gap-2 rounded-md border border-border px-2 py-1.5 text-left hover:bg-surface-2", open && "bg-surface-2")}>
            <span className="flex h-5 w-5 flex-none items-center justify-center rounded bg-surface-3 text-[11px] font-semibold uppercase text-ink-2">{(current?.name ?? "?").slice(0, 1)}</span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[12px] font-medium text-ink">{current?.name ?? "No project"}</span>
              <span className="block truncate font-mono text-[10px] text-ink-3">{current?.id ?? "select a project"}</span>
            </span>
            <ChevronsUpDown size={13} className="text-ink-3" />
          </button>
        )}
      >
        {(close) => (
          <div className="max-h-80 overflow-y-auto">
            <div className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">Projects</div>
            {me.projects.length === 0 && <div className="px-2 py-1 text-xs text-ink-3">No projects yet</div>}
            {me.projects.map((p) => (
              <MenuItem
                key={p.id}
                icon={p.id === current?.id ? <Check size={13} className="text-accent" /> : <span className="inline-block w-[13px]" />}
                onClick={() => {
                  close();
                  if (p.id !== current?.id) select.mutate(p.id);
                }}
              >
                <span className="min-w-0 flex-1 truncate">{p.name}</span>
                <span className="ml-2 truncate font-mono text-[10px] text-ink-3">{p.id}</span>
              </MenuItem>
            ))}
            <div className="my-1 h-px bg-border" />
            <NavLink to="/projects" onClick={close} className="block rounded px-2 py-1.5 text-xs text-ink-2 hover:bg-surface-2">
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
  return <Button variant="ghost" className="px-1.5" title={`Theme: ${theme} (click for ${next})`} aria-label="Toggle theme" onClick={() => setTheme(next)} icon={<Icon size={15} />} />;
}

function UserMenu({ me, collapsed }: { me: Me; collapsed: boolean }) {
  const logout = useLogout();
  const nav = useNavigate();
  const name = me.user?.name || me.user?.email || (me.auth_mode === "disabled" ? "local" : "user");
  if (collapsed) {
    return (
      <Button
        variant="ghost"
        className="px-1.5"
        title={`Sign out (${name})`}
        aria-label="Sign out"
        icon={<LogOut size={15} />}
        onClick={() => logout.mutate(undefined, { onSuccess: () => (window.location.href = "/ui/login") })}
      />
    );
  }
  return (
    <Popover
      width={220}
      trigger={({ toggle }) => (
        <button type="button" onClick={toggle} className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 py-1 text-left hover:bg-surface-2">
          <span className="flex h-5 w-5 flex-none items-center justify-center rounded-full bg-accent-soft text-[10px] font-semibold uppercase text-accent">{name.slice(0, 1)}</span>
          <span className="min-w-0 flex-1 truncate text-xs text-ink-2">{name}</span>
        </button>
      )}
    >
      {(close) => (
        <div>
          <div className="px-2 py-1.5 text-xs">
            <div className="font-medium text-ink">{name}</div>
            <div className="text-ink-3">
              {me.auth_mode === "disabled" ? "auth disabled" : me.auth_mode}
              {me.is_admin ? " · admin" : ""}
            </div>
          </div>
          <div className="my-1 h-px bg-border" />
          <MenuItem
            icon={<LogOut size={13} />}
            onClick={() => {
              close();
              logout.mutate(undefined, { onSuccess: () => nav("/login", { replace: true }) });
            }}
          >
            Sign out
          </MenuItem>
        </div>
      )}
    </Popover>
  );
}

/** Page header strip: title on the left, controls on the right. */
export function PageHeader({ title, children, sub }: { title: React.ReactNode; children?: React.ReactNode; sub?: React.ReactNode }) {
  return (
    <header className="flex h-12 flex-none items-center justify-between gap-3 border-b border-border bg-surface px-4">
      <div className="min-w-0">
        <h1 className="truncate text-[15px] font-semibold tracking-tight text-ink">{title}</h1>
        {sub && <div className="truncate text-xs text-ink-3">{sub}</div>}
      </div>
      <div className="flex flex-none items-center gap-2">{children}</div>
    </header>
  );
}
