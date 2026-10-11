import { useEffect } from "react";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useMe, useProjectMutations } from "@/api/hooks";
import { ApiRequestError, setUnauthorizedHandler } from "@/api/client";
import { AppShell } from "@/components/shell/app-shell";
import { Spinner } from "@/components/ui";
import { LoginPage } from "@/features/auth/login-page";
import { IssuesPage } from "@/features/issues/issues-page";
import { IssueDetailPage } from "@/features/issues/issue-detail-page";
import { LogsPage } from "@/features/logs/logs-page";
import { DashboardsPage } from "@/features/dashboards/dashboards-page";
import { AlertsPage } from "@/features/alerts/alerts-page";
import { ProjectsPage } from "@/features/projects/projects-page";
import { TracesPage } from "@/features/traces/traces-page";
import { TracePage } from "@/features/traces/trace-page";
import { SystemPage } from "@/features/system/system-page";

export function App() {
  const me = useMe();
  const nav = useNavigate();
  const loc = useLocation();
  const qc = useQueryClient();

  useEffect(() => {
    setUnauthorizedHandler(() => {
      qc.removeQueries({ queryKey: ["me"] });
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
      <div className="flex h-full items-center justify-center p-6 text-sm text-critical">
        Could not reach the API: {me.error instanceof Error ? me.error.message : String(me.error)}
      </div>
    );
  }
  if (unauthorized || !me.data) {
    return (
      <Routes>
        <Route path="*" element={<LoginPage me={undefined} />} />
      </Routes>
    );
  }

  return (
    <Routes>
      <Route path="/login" element={<Navigate to="/issues" replace />} />
      <Route element={<AppShell me={me.data} />}>
        <Route index element={<Navigate to="/issues" replace />} />
        <Route path="/issues" element={<NeedsProject me={me.data}><IssuesPage /></NeedsProject>} />
        <Route path="/issues/:id" element={<NeedsProject me={me.data}><IssueDetailPage /></NeedsProject>} />
        <Route path="/logs" element={<NeedsProject me={me.data}><LogsPage /></NeedsProject>} />
        <Route path="/traces" element={<NeedsProject me={me.data}><TracesPage /></NeedsProject>} />
        <Route path="/traces/:id" element={<NeedsProject me={me.data}><TracePage /></NeedsProject>} />
        <Route path="/dashboards" element={<NeedsProject me={me.data}><DashboardsPage /></NeedsProject>} />
        <Route path="/alerts" element={<NeedsProject me={me.data}><AlertsPage me={me.data} /></NeedsProject>} />
        <Route path="/projects" element={<ProjectsPage me={me.data} />} />
        <Route path="/system" element={<SystemPage me={me.data} />} />
        <Route path="*" element={<Navigate to="/issues" replace />} />
      </Route>
    </Routes>
  );
}

/** Pages that need an active project: auto-select the first one, or send the user to Projects. */
function NeedsProject({ me, children }: { me: NonNullable<ReturnType<typeof useMe>["data"]>; children: React.ReactElement }) {
  const { select } = useProjectMutations();
  const first = me.projects[0];
  useEffect(() => {
    if (!me.project && first && !select.isPending) select.mutate(first.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me.project?.id, first?.id]);
  if (me.project) return children;
  if (!first) return <Navigate to="/projects?first=1" replace />;
  return (
    <div className="flex h-full items-center justify-center">
      <Spinner />
    </div>
  );
}
