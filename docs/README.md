# Noket documentation

The user documentation for Noket, published at <https://docs.noket.dev>. It is built with
[Cloudflare Nimbus](https://github.com/cloudflare/nimbus), an Astro-based documentation framework, and
lives in this repository so that a CLI or configuration change can update its reference pages in the
same pull request.

## Layout

```
docs/
├── src/content/docs/
│   ├── index.mdx               product overview
│   ├── quickstarts/            use a shared model, share yours, connect a local model
│   ├── console/                the web console at noket.dev
│   ├── api/                    hand-written HTTP API reference
│   ├── connector/              install, login, upstreams, tunnel, local servers
│   ├── concepts/               models, sharing, upstreams, tunnel, probing, security
│   ├── reference/              config, files, environment, platforms
│   │   └── cli/                generated — do not edit
│   └── troubleshooting.mdx
├── src/components/             Nimbus UI components from the scaffold
├── src/layouts/, src/pages/    page shell, routes, generated OG/llms.txt endpoints
├── src/styles/                 Tailwind and prose styles
├── public/                     static assets, including CNAME
├── astro.config.ts             site metadata and sidebar
├── nimbus.json                 Nimbus scaffold metadata
└── package.json
```

`reference/cli/` and `reference/config.md` are generated from the Cobra command tree and the structs in
`internal/types`. Run `make docs-generate` from the repository root and commit the output; never edit
those pages by hand. `make check` fails when they are stale.

## Commands

From the repository root:

```bash
make docs-install     # npm ci
make docs-generate    # regenerate the reference pages
make docs-check       # verify the generated pages are current
make docs-lint        # nimbus-docs lint: frontmatter and internal links
make docs-typecheck   # astro check
make docs-dev         # dev server at http://localhost:4321
make docs-build       # production build into docs/dist
```

Or from `docs/`: `npm run dev`, `npm run build`, `npm run lint:docs`, `npm run typecheck`, and
`npx nimbus-docs check` (scaffold structure, migrations, and types). Node 22.23.4 or newer is
required.

## Writing

- Pages are MDX. Nimbus registers `Aside`, `Card`, `CardGrid`, `Steps`, `Tabs`, and the other
  components globally, so they need no import.
- Sidebar groups come from `astro.config.ts`; a directory's `index.mdx` names its group, and
  `sidebar.order` in frontmatter sets each page's position. Unknown frontmatter keys are rejected.
- Link with root-relative paths (`/connector/local-models/ollama/`); `make docs-lint` fails on a broken
  internal link. The site is pre-launch and keeps no redirects, so when a page moves, update the links to
  it rather than adding a `redirects` entry to `astro.config.ts`.
- The pages under `api/` are hand-written, not generated. When a route, header, or error in the
  inference API changes, update them in the same pull request.
- The generated reference pages are plain `.md`, which Nimbus lint skips. Keep them generated
  rather than converting them: the CLI help text contains characters that MDX would parse as JSX.
- Keep the console pages user-facing. Describe what a person sees and does, not internal service names
  or RPC method names.
- Never put a real key or token in an example, and do not pass keys in arguments or environment
  variables; pipe them from a secret manager instead.
- TOML snippets in fenced blocks are parsed by `tools/docgen`'s tests, so a snippet cannot drift from
  the configuration schema.

## Deployment

`.github/workflows/docs.yml` checks the generated pages, lints and typechecks the site on every pull
request that touches the docs or the generator, and deploys `main` to GitHub Pages. `public/CNAME`
pins the custom domain. The Pages source must be set to GitHub Actions in the repository settings.
