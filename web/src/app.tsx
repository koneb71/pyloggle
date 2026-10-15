import { useEffect } from "react";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useMe, useProjectMutations } from "@/api/hooks";
import { ApiRequestError, setUnauthorizedHandler } from "@/api/client";
import { AppShell } from "@/components/shell/app-shell";
import { Button, ErrorState, Spinner } from "@/components/ui";
import { LoginPage } from "@/features/auth/login-page";
import { IssuesPage } from "@/features/issues/issues-page";
import { OverviewPage } from "@/features/overview/overview-page";
import { IssueDetailPage } from "@/features/issues/issue-detail-page";
import { LogsPage } from "@/features/logs/logs-page";
import { DashboardsPage } from "@/features/dashboards/dashboards-page";
import { AnalyticsPage } from "@/features/analytics/analytics-page";
import { AlertsPage } from "@/features/alerts/alerts-page";
import { ProjectsPage } from "@/features/projects/projects-page";
import { TracesPage } from "@/features/traces/traces-page";
import { TracePage } from "@/features/traces/trace-page";
import { SystemPage } from "@/features/system/system-page";
import { HelpPage } from "@/features/help/help-page";

export function App() {
  const me = useMe();
  const nav = useNavigate();
  const loc = useLocation();
  const qc = useQueryClient();

  useEffect(() => {
    // A 401 while signed in means the session ended: drop it so the sign-in screen shows.
    // Without a session there is nothing to drop; resetting again would refetch /me forever.
    setUnauthorizedHandler(() => {
      if (qc.getQueryData(["me"]) === undefined) return;
      qc.resetQueries({ queryKey: ["me"] });
      if (!loc.pathname.startsWith("/login")) nav(`/login?next=${encodeURIComponent(loc.pathname + loc.search)}`, { replace: true });
    });
  }, [nav, loc.pathname, loc.search, qc]);

  if (me.isPending) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    );
  }
  const unauthorized = me.isError && me.error instanceof ApiRequestError && me.error.status === 401;
  if (me.isError && !unauthorized) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="w-full max-w-md rounded-lg border border-border bg-surface">
          <ErrorState error={me.error} title={databaseDown(me.error) ? "Pyloggle can't reach its database" : "Can't reach the Pyloggle server"} retry={() => me.refetch()} />
        </div>
      </div>
    );
  }
  if (unauthorized || !me.data) {
    return (
      <Routes>
        <Route path="*" element={<LoginPage me={undefined} loginUrl={unauthorized ? (me.error as ApiRequestError).body.login_url : undefined} />} />
      </Routes>
    );
  }

  return (
    <Routes>
      <Route path="/login" element={<Navigate to="/overview" replace />} />
      <Route element={<AppShell me={me.data} />}>
        <Route index element={<Navigate to="/overview" replace />} />
        <Route path="/overview" element={<NeedsProject me={me.data}><OverviewPage me={me.data} /></NeedsProject>} />
        <Route path="/issues" element={<NeedsProject me={me.data}><IssuesPage /></NeedsProject>} />
        <Route path="/issues/:id" element={<NeedsProject me={me.data}><IssueDetailPage /></NeedsProject>} />
        <Route path="/logs" element={<NeedsProject me={me.data}><LogsPage /></NeedsProject>} />
        <Route path="/traces" element={<NeedsProject me={me.data}><TracesPage /></NeedsProject>} />
        <Route path="/traces/:id" element={<NeedsProject me={me.data}><TracePage /></NeedsProject>} />
        <Route path="/dashboards" element={<NeedsProject me={me.data}><DashboardsPage /></NeedsProject>} />
        <Route path="/analytics" element={<NeedsProject me={me.data}><AnalyticsPage /></NeedsProject>} />
        <Route path="/alerts" element={<NeedsProject me={me.data}><AlertsPage me={me.data} /></NeedsProject>} />
        <Route path="/projects" element={<ProjectsPage me={me.data} />} />
        <Route path="/system" element={<SystemPage me={me.data} />} />
        <Route path="/help" element={<HelpPage me={me.data} />} />
        <Route path="*" element={<Navigate to="/overview" replace />} />
      </Route>
    </Routes>
  );
}

/** Pages that need an active project: pick the first one automatically, or send the user to Projects. */
function NeedsProject({ me, children }: { me: NonNullable<ReturnType<typeof useMe>["data"]>; children: React.ReactElement }) {
  const { select } = useProjectMutations();
  const first = me.projects[0];
  useEffect(() => {
    if (!me.project && first && select.isIdle) select.mutate(first.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me.project?.id, first?.id]);
  if (me.project) return children;
  if (!first) return <Navigate to="/projects?first=1" replace />;
  if (select.isError) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="w-full max-w-md rounded-lg border border-border bg-surface">
          <ErrorState error={select.error} title={`Couldn't open the project "${first.name}"`} retry={() => select.mutate(first.id)} />
          <div className="border-t border-border px-4 py-3 text-center">
            <Button variant="ghost" onClick={() => (window.location.href = "/ui/projects")}>
              Go to Projects
            </Button>
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="flex h-full items-center justify-center">
      <Spinner />
    </div>
  );
}

/** The server answered, but its database (ClickHouse) did not. */
function databaseDown(e: unknown): boolean {
  return e instanceof ApiRequestError && e.status === 503 && e.body?.error === "unavailable";
}
