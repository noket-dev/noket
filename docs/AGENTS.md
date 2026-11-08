# Documentation site instructions

This directory is the [Cloudflare Nimbus](https://github.com/cloudflare/nimbus) site for
<https://docs.noket.dev>, built on Astro 7. The repository root `AGENTS.md` applies here too.

## Generated pages

`src/content/docs/reference/cli/**` and `src/content/docs/reference/config.md` are produced by
`tools/docgen` from the Cobra command tree and the structs in `internal/types`. Do not edit them by
hand: change the source, run `make docs-generate`, and commit the result. `make check` runs
`go run ./tools/docgen -check` and fails on stale output.

They stay `.md`: Nimbus lint reads only `.mdx`, and the generated help text contains `<command>` and
`${…}` in prose, which MDX would parse as JSX. Hand-written pages are `.mdx` so that lint sees them.

## Development

Start the dev server in the background:

```
astro dev --background
```

Manage it with `astro dev stop`, `astro dev status`, and `astro dev logs`.

Run `npm run build` once in a fresh checkout before the checks below: lint and `nimbus-docs check`
read `.nimbus/lint.json` and `.nimbus/routes.json`, which only a dev run or a build writes.

Before committing, run:

- `npm run lint:docs` — Nimbus authoring rules; `nimbus/frontmatter-shape` and
  `nimbus/internal-link` are errors, so invalid frontmatter and broken internal links fail. Pass
  `--fix` to apply the safe fixes.
- `npm run typecheck` — `astro check`.
- `npx nimbus-docs check` — scaffold structure, migrations, and types.
- `npm run build` — renders every page into `dist/`.

## Content rules

- Pages are `.mdx`. Nimbus components are global — no imports: `Aside`, `Card`, `CardGrid`,
  `PackageManagers`, `Step`, `Steps`, `TabItem`, `Tabs`, `Render`.
- Frontmatter requires `title` and rejects unknown keys. Use `description`, `sidebar.order`, and the
  other keys from the Nimbus content schema.
- Sidebar groups are defined in `astro.config.ts`. A directory's `index.mdx` names its group; pages
  sort by `sidebar.order`.
- Links are root-relative paths to content slugs, e.g. `/connector/install/`. The site is pre-launch, so it
  keeps no redirects: when a page moves, update the links to it instead of adding a `redirects` entry to
  `astro.config.ts`.
- Keep pages user-facing: describe what a person sees and does. No internal service or RPC names.
- Never put a real key or token in an example. Show a placeholder (`YOUR_API_KEY`) or pipe a key from
  a secret manager; never pass one as an argument or environment variable.
- Fenced `toml` blocks are parsed against the real configuration schema by `go test ./tools/docgen`,
  so a snippet must be valid `config.toml`.

## References

- Nimbus repository and CLI help: <https://github.com/cloudflare/nimbus>, `nimbus-docs --help`
- Astro: <https://docs.astro.build>
- Site structure is recorded in `nimbus.json`; keep it in sync with the scaffold when upgrading.
