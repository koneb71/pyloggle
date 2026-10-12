import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router";
import clsx from "clsx";
import { Check, ChevronsUpDown, Keyboard, LogOut, Menu, Moon, PanelLeftClose, PanelLeftOpen, Sun, SunMoon, X } from "lucide-react";
import type { Me } from "@/api/types";
import { useLogout, useProjectMutations } from "@/api/hooks";
import { Dialog, IconButton, Kbd, MenuItem, Popover, Tooltip } from "@/components/ui";
import { useTheme } from "./theme";
import { useMediaQuery, useSmallScreen } from "@/lib/use-media";
import { ALL_ITEMS, HELP, NAV, shortcutRows, type NavItem } from "./nav";


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

  // Phones: a top bar, and the same navigation in a drawer that slides in from the left.
  const small = useSmallScreen();
  const [menuOpen, setMenuOpen] = useState(false);
  const closeMenu = useCallback(() => setMenuOpen(false), []);
  const { pathname } = useLocation();
  useEffect(() => closeMenu(), [pathname, small, closeMenu]);

  return (
    <div className="flex h-full min-h-0 flex-col md:flex-row">
      <TopBar me={me} menuOpen={menuOpen} onMenu={() => setMenuOpen(true)} />
      <aside className={clsx("hidden h-full flex-none flex-col border-r border-border bg-surface transition-[width] duration-150 md:flex", collapsed ? "w-14" : "w-60")} aria-label="Main navigation">
        <Sidebar
          me={me}
          collapsed={collapsed}
          footer={
            <>
              <UserMenu me={me} collapsed={collapsed} />
              <IconButton label="Keyboard shortcuts (?)" icon={<Keyboard size={15} />} onClick={() => setHelp(true)} />
              <ThemeToggle />
              <IconButton label={collapsed ? "Expand sidebar" : "Collapse sidebar"} onClick={toggle} icon={collapsed ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />} />
            </>
          }
        />
      </aside>
      <main className="flex min-h-0 min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
      {small && <MobileNav open={menuOpen} onClose={closeMenu} me={me} />}
      <ShortcutsDialog open={help} onClose={() => setHelp(false)} me={me} />
    </div>
  );
}

/** The navigation itself, shared by the sidebar on wide screens and the menu on phones. */
function Sidebar({ me, collapsed, footer, headerAction, onNavigate }: { me: Me; collapsed: boolean; footer: ReactNode; headerAction?: ReactNode; onNavigate?: () => void }) {
  return (
    <>
      <div className={clsx("flex h-14 flex-none items-center", collapsed ? "justify-center" : "gap-2.5 px-4")}>
        <Logo />
        {!collapsed && <span className="text-[15px] font-semibold tracking-tight text-ink">Pyloggle</span>}
        {headerAction && <span className="-mr-2 ml-auto">{headerAction}</span>}
      </div>
      {!collapsed && <ProjectSwitcher me={me} onSwitched={onNavigate} />}
      <nav className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-2 py-3">
        {NAV.map((group, gi) => {
          const items = group.items.filter((n) => !n.show || n.show(me));
          if (items.length === 0) return null;
          return (
            <div key={gi} className="flex flex-col gap-0.5">
              {group.title && !collapsed && <div className="px-2.5 pb-1 text-xs font-medium text-ink-3">{group.title}</div>}
              {group.title && collapsed && <div className="mx-3 mb-1 h-px bg-border" aria-hidden />}
              {items.map((n) => (
                <NavEntry key={n.to} item={n} collapsed={collapsed} onNavigate={onNavigate} />
              ))}
            </div>
          );
        })}
        <div className="mt-auto">
          <NavEntry item={HELP} collapsed={collapsed} onNavigate={onNavigate} />
        </div>
      </nav>
      <div className={clsx("flex flex-none items-center gap-0.5 border-t border-border p-2", collapsed && "flex-col")}>{footer}</div>
    </>
  );
}

/** Phones only: the brand, the project, and the button that opens the menu. */
function TopBar({ me, menuOpen, onMenu }: { me: Me; menuOpen: boolean; onMenu: () => void }) {
  return (
    <header className="flex h-12 flex-none items-center gap-2 border-b border-border bg-surface pl-1.5 pr-4 md:hidden">
      <IconButton label="Open menu" size="md" icon={<Menu size={19} />} onClick={onMenu} aria-expanded={menuOpen} aria-controls="mobile-nav" />
      <Logo />
      <span className="flex min-w-0 items-baseline gap-1.5 text-[15px]">
        <span className="flex-none font-semibold tracking-tight text-ink">Pyloggle</span>
        {me.project && (
          <>
            <span className="text-ink-3" aria-hidden>
              /
            </span>
            <span className="truncate text-ink-2" title="Current project">
              {me.project.name}
            </span>
          </>
        )}
      </span>
    </header>
  );
}

/** The menu on phones: the sidebar as a drawer over the page. Escape, the backdrop or a choice closes it. */
function MobileNav({ open, onClose, me }: { open: boolean; onClose: () => void; me: Me }) {
  const panel = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!open) return;
    const back = document.activeElement as HTMLElement | null;
    panel.current?.querySelector<HTMLElement>("[data-autofocus]")?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      back?.focus?.();
    };
  }, [open, onClose]);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-[57] md:hidden">
      <div className="fade-in absolute inset-0 bg-black/40" onPointerDown={onClose} aria-hidden />
      <aside ref={panel} id="mobile-nav" role="dialog" aria-modal aria-label="Main navigation" className="slide-in-left relative flex h-full w-[min(18rem,86vw)] flex-col border-r border-border bg-surface shadow-[var(--shadow)]">
        <Sidebar
          me={me}
          collapsed={false}
          onNavigate={onClose}
          headerAction={<IconButton label="Close menu" size="md" icon={<X size={18} />} onClick={onClose} data-autofocus />}
          footer={
            <>
              <UserMenu me={me} collapsed={false} />
              <ThemeToggle />
            </>
          }
        />
      </aside>
    </div>,
    document.body,
  );
}

function NavEntry({ item: n, collapsed, onNavigate }: { item: NavItem; collapsed: boolean; onNavigate?: () => void }) {
  const link = (
    <NavLink
      to={n.to}
      onClick={onNavigate}
      aria-label={collapsed ? n.label : undefined}
      className={({ isActive }) =>
        clsx(
          "flex h-8 items-center gap-2.5 rounded-md px-2.5 text-[13px] font-medium transition-colors pointer-coarse:h-10 pointer-coarse:text-sm",
          isActive ? "bg-accent-soft text-accent" : "text-ink-2 hover:bg-surface-2 hover:text-ink",
          collapsed && "justify-center px-0",
        )
      }
    >
      <n.icon size={16} className="flex-none" />
      {!collapsed && <span className="truncate">{n.label}</span>}
    </NavLink>
  );
  // In the phone menu every entry already shows its name, and a tap would only pop the tooltip.
  if (onNavigate) return link;
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
  return (
    <Dialog open={open} onClose={onClose} title="Keyboard shortcuts">
      <ShortcutList me={me} />
    </Dialog>
  );
}

/** The keyboard shortcuts as a two-column list, in the dialog and on the Help page. */
export function ShortcutList({ me }: { me: Me }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] items-center gap-x-6 gap-y-2 text-[13px]">
      {shortcutRows(me).map(([keys, what]) => (
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

function ProjectSwitcher({ me, onSwitched }: { me: Me; onSwitched?: () => void }) {
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
                  onSwitched?.();
                }}
              >
                <span className="min-w-0 flex-1 truncate">{p.name}</span>
                <span className="ml-2 truncate font-mono text-[11px] text-ink-3">{p.id}</span>
              </MenuItem>
            ))}
            <div className="my-1 h-px bg-border" />
            <NavLink to="/projects" onClick={() => (close(), onSwitched?.())} className="block rounded px-2 py-1.5 text-[13px] text-ink-2 hover:bg-surface-2 hover:text-ink">
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
