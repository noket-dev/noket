import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";
import nimbus, {
  defineConfig as defineNimbusConfig,
} from "@cloudflare/nimbus-docs";
import { tableScroll } from "@cloudflare/nimbus-docs/markdown";

const nimbusConfig = defineNimbusConfig({
  // Canonical origin (no trailing slash): canonical URLs, absolute OG image
  // URLs, robots.txt, sitemap, and the links in /llms.txt.
  site: "https://docs.noket.dev",
  // Same title as the web app's <title>, so the two tabs read alike. Nimbus
  // drops the suffix when a page's title already equals it (the homepage).
  title: "Noket - AI, Yours to Share.",
  description: "Share your LLM with everyone. From your machine, to anywhere.",
  locale: "en",
  github: "https://github.com/noket-dev/noket",
  socialImageAlt: "Noket documentation preview",
  editPattern: "https://github.com/noket-dev/noket/edit/main/docs/{path}",
  // Each top-level folder's index page names its group. A folder without one
  // is labeled from its directory name, and `sidebar.order` in frontmatter
  // sets each page's position.
  sidebar: {
    items: [
      "index",
      { label: "Quickstarts", autogenerate: { directory: "quickstarts" } },
      { label: "Console", autogenerate: { directory: "console" } },
      { label: "API", autogenerate: { directory: "api" } },
      { label: "Connector", autogenerate: { directory: "connector" } },
      { label: "Concepts", autogenerate: { directory: "concepts" } },
      {
        label: "Reference",
        items: [
          "reference/config",
          "reference/environment",
          "reference/files",
          "reference/platforms",
          { label: "CLI", autogenerate: { directory: "reference/cli" } },
        ],
      },
      { label: "Troubleshooting", link: "/troubleshooting/" },
    ],
  },
});

export default defineConfig({
  // nimbus:adapter
  output: "static",
  // Tailwind v4 via its Vite plugin (the integration Astro recommends for
  // Tailwind v4 — replaces the PostCSS plugin, which doesn't build under
  // Astro 7's Vite 8 bundler).
  vite: {
    plugins: [tailwindcss()],
  },
  // Hover-prefetch link targets so full-page navigations feel instant without
  // a client-side router.
  prefetch: {
    prefetchAll: true,
    defaultStrategy: "hover",
  },
  integrations: [
    nimbus(nimbusConfig, {
      // Authoring rules are opt-in by design — your repo, your taste. The
      // two below are the load-bearing pair: frontmatter has to validate
      // against the content schema for the page to render properly, and
      // broken internal links are 404s for your readers. Add the others
      // (heading hierarchy, code-block language, style, etc.) when you're
      // ready to enforce them — see `nimbus-docs lint --help`.
      rules: {
        "nimbus/frontmatter-shape": "error",
        "nimbus/internal-link": "error",
      },
      markdown: {
        // Wrap wide tables so they scroll instead of overflowing the page
        // (styled by `.nb-table-scroll` in src/styles/prose.css).
        hastPlugins: [tableScroll()],
        // `## Install {#install}` gives a heading a fixed id. A heading that
        // ends in other `{…}` text, such as `{account_id}` in a .md file, is
        // read as attributes too; remove this to keep it as text.
        features: { headingAttributes: true },
      },
    }),
  ],
});
