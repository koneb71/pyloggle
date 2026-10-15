/**
 * Pages of the documentation site the app links to, by what they explain. Paths are relative to
 * the docs root (PYLOGGLE_DOCS_URL, sent as features.docs_url) and match files in docs/; a test
 * checks that each one exists, so the app cannot ship a dead link.
 */
export const DOCS = {
  home: "",
  quickStart: "introduction/quick-start",
  search: "app/search",
  analytics: "app/analytics",
  python: "send-data/python",
  sdkConfiguration: "send-data/configuration",
  frameworks: "send-data/frameworks",
  opentelemetry: "send-data/opentelemetry",
  deployment: "self-hosting/deployment",
  signIn: "self-hosting/sign-in",
  alerting: "self-hosting/alerting",
  security: "self-hosting/security",
  retention: "self-hosting/retention-and-footprint",
  configuration: "reference/configuration",
  httpApi: "reference/http-api",
  webhookPayload: "reference/webhook-payload",
} as const;

export type DocKey = keyof typeof DOCS;

/** The full address of a docs page, or null when the server hides the docs links. */
export function docsUrl(base: string | undefined, key: DocKey, anchor?: string): string | null {
  const root = (base ?? "").trim();
  if (!root) return null;
  const page = DOCS[key];
  return root.replace(/\/?$/, "/") + page + (anchor ? `#${anchor}` : "");
}
