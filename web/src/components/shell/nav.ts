import type { ComponentType } from "react";
import { Activity, AlertOctagon, Bell, ChartNoAxesCombined, FolderKanban, GitBranch, Home, LayoutDashboard, LifeBuoy, ScrollText } from "lucide-react";
import type { Me } from "@/api/types";

export interface NavItem {
  to: string;
  label: string;
  hint: string;
  icon: ComponentType<{ size?: number; className?: string }>;
  key: string;
  show?: (me: Me) => boolean;
}

/** Navigation grouped by what people come to do: look into problems, keep watch, set things up. */
export const NAV: { title?: string; items: NavItem[] }[] = [
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
      { to: "/analytics", label: "Analytics", hint: "Health and use over days and weeks, and any chart you need", icon: ChartNoAxesCombined, key: "y" },
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

/** Help sits at the bottom of the sidebar, apart from the groups. */
export const HELP: NavItem = { to: "/help", label: "Help", hint: "How to use Pyloggle", icon: LifeBuoy, key: "h" };

export const ALL_ITEMS: NavItem[] = [...NAV.flatMap((g) => g.items), HELP];

/** Every keyboard shortcut, as [keys, what it does], for the shortcuts dialog and the Help page. */
export function shortcutRows(me: Me): [string[], string][] {
  return [
    ...ALL_ITEMS.filter((n) => !n.show || n.show(me)).map((n): [string[], string] => [["g", n.key], `Go to ${n.label}`]),
    [["/"], "Search (on Logs and Requests)"],
    [["j", "k"], "Next and previous message (on Logs)"],
    [["Esc"], "Close a panel or dialog"],
    [["?"], "Show these shortcuts"],
  ];
}
