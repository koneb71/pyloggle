import { readFileSync } from "node:fs";
import { defineConfig, type DefaultTheme } from "vitepress";

// The site is served from https://koneb71.github.io/pyloggle/. DOCS_BASE=/ builds it for the
// root of another host (a mirror, or a preview).
const base = process.env.DOCS_BASE ?? "/pyloggle/";
const repo = "https://github.com/koneb71/pyloggle";
// The docs carry the release's version (scripts/check-versions.sh keeps it in step), shown as "1.0".
const { version } = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
const minor = version.split(".").slice(0, 2).join(".");

const sidebar: DefaultTheme.SidebarItem[] = [
  {
    text: "Introduction",
    items: [
      { text: "What is Pyloggle?", link: "/introduction/what-is-pyloggle" },
      { text: "Quick start", link: "/introduction/quick-start" },
      { text: "Concepts", link: "/introduction/concepts" },
    ],
  },
  {
    text: "Using the app",
    items: [
      { text: "Overview", link: "/app/overview" },
      { text: "Issues", link: "/app/issues" },
      { text: "Logs", link: "/app/logs" },
      { text: "Requests", link: "/app/requests" },
      { text: "Dashboards", link: "/app/dashboards" },
      { text: "Analytics", link: "/app/analytics" },
      { text: "Alerts", link: "/app/alerts" },
      { text: "Projects", link: "/app/projects" },
      { text: "System", link: "/app/system" },
      { text: "Help and shortcuts", link: "/app/help" },
      { text: "Search syntax", link: "/app/search" },
    ],
  },
  {
    text: "Send data",
    items: [
      { text: "Python package", link: "/send-data/python" },
      { text: "Frameworks", link: "/send-data/frameworks" },
      { text: "Configuration", link: "/send-data/configuration" },
      { text: "OpenTelemetry", link: "/send-data/opentelemetry" },
      { text: "Examples", link: "/send-data/examples" },
    ],
  },
  {
    text: "Run it yourself",
    items: [
      { text: "Deployment", link: "/self-hosting/deployment" },
      { text: "Sign-in", link: "/self-hosting/sign-in" },
      { text: "Alerting", link: "/self-hosting/alerting" },
      { text: "Security", link: "/self-hosting/security" },
      { text: "Retention and footprint", link: "/self-hosting/retention-and-footprint" },
      {
        text: "Upgrading",
        collapsed: true,
        items: [
          { text: "To 1.0", link: "/self-hosting/upgrading" },
          { text: "To 0.3", link: "/self-hosting/upgrading-0.3" },
          { text: "To 0.2", link: "/self-hosting/upgrading-0.2" },
        ],
      },
    ],
  },
  {
    text: "Reference",
    items: [
      { text: "Server configuration", link: "/reference/configuration" },
      { text: "HTTP API", link: "/reference/http-api" },
      { text: "Webhook payload", link: "/reference/webhook-payload" },
      { text: "Wire format", link: "/reference/wire-format" },
    ],
  },
];

export default defineConfig({
  title: "Pyloggle",
  description: "Self-hosted logs, error tracking, request timelines and alerts for Python apps.",
  lang: "en-US",
  base,
  cleanUrls: true,
  srcExclude: ["README.md"],
  // Pages show commands such as `open http://localhost:8080/ui/`; those are not dead links.
  ignoreDeadLinks: "localhostLinks",
  sitemap: { hostname: `https://koneb71.github.io${base}` },
  head: [
    ["link", { rel: "icon", type: "image/svg+xml", href: `${base}logo.svg` }],
    ["meta", { name: "theme-color", content: "#2262c0" }],
  ],
  markdown: {
    theme: { light: "github-light", dark: "github-dark" },
  },
  themeConfig: {
    logo: "/logo.svg",
    nav: [
      { text: "Guide", link: "/introduction/what-is-pyloggle", activeMatch: "^/(introduction|app)/" },
      { text: "Send data", link: "/send-data/python", activeMatch: "^/send-data/" },
      { text: "Run it yourself", link: "/self-hosting/deployment", activeMatch: "^/self-hosting/" },
      { text: "Reference", link: "/reference/configuration", activeMatch: "^/reference/" },
      {
        text: minor,
        items: [
          { text: "What's new", link: `${repo}/blob/main/CHANGELOG.md` },
          { text: "Releases", link: `${repo}/releases` },
          { text: "Contributing", link: `${repo}/blob/main/CONTRIBUTING.md` },
        ],
      },
    ],
    sidebar,
    outline: { level: [2, 3], label: "On this page" },
    socialLinks: [{ icon: "github", link: repo }],
    editLink: {
      pattern: `${repo}/edit/main/docs/:path`,
      text: "Suggest a change to this page",
    },
    search: { provider: "local" },
    footer: {
      message: `Released under the <a href="${repo}/blob/main/LICENSE">Apache License 2.0</a>.`,
      copyright: "Copyright © 2026 koneb71",
    },
    docFooter: { prev: "Previous", next: "Next" },
    externalLinkIcon: true,
  },
});
