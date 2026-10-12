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
    ],
  },
  {
    text: "Using the app",
    items: [{ text: "Search syntax", link: "/app/search" }],
  },
  {
    text: "Send data",
    items: [{ text: "OpenTelemetry", link: "/send-data/opentelemetry" }],
  },
  {
    text: "Run it yourself",
    items: [
      { text: "Retention and footprint", link: "/self-hosting/retention-and-footprint" },
      {
        text: "Upgrading",
        collapsed: true,
        items: [
          { text: "To 0.3", link: "/self-hosting/upgrading-0.3" },
          { text: "To 0.2", link: "/self-hosting/upgrading-0.2" },
        ],
      },
    ],
  },
  {
    text: "Reference",
    items: [{ text: "Wire format", link: "/reference/wire-format" }],
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
      { text: "Send data", link: "/send-data/opentelemetry", activeMatch: "^/send-data/" },
      { text: "Run it yourself", link: "/self-hosting/retention-and-footprint", activeMatch: "^/self-hosting/" },
      { text: "Reference", link: "/reference/wire-format", activeMatch: "^/reference/" },
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
