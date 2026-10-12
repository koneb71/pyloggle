import { useState } from "react";
import { CopyButton, Section, Tabs } from "@/components/ui";

interface Props {
  serverUrl: string;
  projectId: string;
  /** The key to show in the snippets; a placeholder when it isn't known. */
  apiKey?: string;
  /** Shown first and open when the project has no data yet. */
  prominent?: boolean;
}

const TABS = [
  { id: "python", label: "Python" },
  { id: "django", label: "Django" },
  { id: "fastapi", label: "FastAPI" },
  { id: "otel", label: "OpenTelemetry" },
];

/** "Connect your app": copyable setup for each way of sending data, filled in for this server and project. */
export function ConnectGuide({ serverUrl, projectId, apiKey, prominent }: Props) {
  const [tab, setTab] = useState("python");
  const key = apiKey || "<your access key>";
  const env = `PYLOGGLE_INGEST_URL=${serverUrl}\nPYLOGGLE_API_KEY=${key}\nPYLOGGLE_PROJECT_ID=${projectId}`;
  const steps: Record<string, { intro: string; blocks: { title: string; code: string }[] }> = {
    python: {
      intro: "For scripts, workers and any Python app. Logging calls and uncaught errors are sent automatically.",
      blocks: [
        { title: "1. Install", code: "pip install pyloggle" },
        { title: "2. Set these in the app's environment", code: env },
        { title: "3. Start it once, as early as possible", code: 'import pyloggle\n\npyloggle.init(service="my-app", env="production")' },
      ],
    },
    django: {
      intro: "Adds one middleware. Every request, error, slow query and log line is recorded with the route it came from.",
      blocks: [
        { title: "1. Install", code: 'pip install "pyloggle[django]"' },
        { title: "2. Set these in the app's environment", code: env },
        { title: "3. In settings.py", code: 'MIDDLEWARE = [\n    "pyloggle.django.PyloggleMiddleware",\n    # ... your other middleware\n]\n\nPYLOGGLE = {"service": "my-site", "env": "production"}' },
      ],
    },
    fastapi: {
      intro: "Records requests, errors and log lines, and FastAPI's built-in step-by-step timings.",
      blocks: [
        { title: "1. Install", code: 'pip install "pyloggle[fastapi]"' },
        { title: "2. Set these in the app's environment", code: env },
        {
          title: "3. Where you create the app",
          code: 'import pyloggle\nfrom fastapi import FastAPI\nfrom pyloggle.fastapi import PyloggleFastAPIMiddleware, telemetry\n\nclient = pyloggle.init(service="my-api", env="production")\napp = FastAPI(telemetry=telemetry())  # FastAPI 0.143+; leave out telemetry= on older versions\napp.add_middleware(PyloggleFastAPIMiddleware, client=client)',
        },
      ],
    },
    otel: {
      intro: "Any language with OpenTelemetry: point its exporter at this server. Logs, traces and metrics all work.",
      blocks: [
        {
          title: "Set these in the app's environment",
          code: `OTEL_EXPORTER_OTLP_ENDPOINT=${serverUrl}\nOTEL_EXPORTER_OTLP_HEADERS=x-api-key=${key}\nOTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf\nOTEL_SERVICE_NAME=my-app\nOTEL_RESOURCE_ATTRIBUTES=deployment.environment=production`,
        },
      ],
    },
  };
  const s = steps[tab];
  return (
    <Section
      title="Connect your app"
      description={prominent ? "This project hasn't received anything yet. Pick how your app is built and follow the steps." : "How to send data to this project, with this server's address filled in."}
      className={prominent ? "border-accent/40" : undefined}
      flush
    >
      <div className="px-4 pt-1">
        <Tabs items={TABS} value={tab} onChange={setTab} />
      </div>
      <div className="space-y-4 p-4">
        <p className="text-[13px] text-ink-2">{s.intro}</p>
        {s.blocks.map((b) => (
          <div key={b.title}>
            <div className="mb-1.5 flex items-center justify-between">
              <span className="text-[13px] font-medium text-ink">{b.title}</span>
              <CopyButton text={b.code} label="Copy" showLabel />
            </div>
            <pre className="overflow-x-auto rounded-md border border-border bg-surface-2 px-3 py-2.5 font-mono text-[12px] leading-5 text-ink">{b.code}</pre>
          </div>
        ))}
        {!apiKey && <p className="text-xs text-ink-3">Replace &lt;your access key&gt; with a key that can send data. Create one under the project's access keys; it is shown only once.</p>}
      </div>
    </Section>
  );
}
