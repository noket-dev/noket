# Repository Instructions

Applies repo-wide; a package-level `AGENTS.md` takes precedence.

## Structure

- `cmd/noket`, `cmd/noketd`: CLI and daemon entry points; the version arrives through `-ldflags -X main.version`.
- `internal/cli`, `internal/tui`: Cobra commands and the bubbletea console (table, modals, forms, keys, view).
- `internal/upstream`, `internal/noketd`, `internal/state`: the shared service layer — upstream rules and probing,
  daemon connection and lifecycle, and the config/credentials/status snapshot.
- `internal/config`, `internal/types`: local files, validation, and their types.
- `internal/forward`, `internal/probe`, `internal/netguard`, `internal/rpc`: data plane, guards, and the RPC codec.
- `internal/ui`: the lipgloss palette shared by the CLI and the TUI.
- `proto/`, `gen/`: contracts and generated Go.
- `docs/`: the public user documentation (Cloudflare Nimbus on Astro 7, published to docs.noket.dev);
  see `docs/AGENTS.md`.
- `tools/docgen`: generates the CLI and configuration reference pages in `docs/` from the command tree and
  `internal/types`.

## Rules

- The CLI and the TUI share `internal/upstream`, `internal/noketd`, and `internal/state`; do not duplicate their
  logic in `internal/cli` or `internal/tui`.
- `config.toml` is the only source of upstreams and noketd rewrites it from cloud commands, so racing writes use
  `upstream.SaveFresh` / `RemoveFresh` and handle `ErrStale`.
- TUI side effects run in `tea.Cmd`, never `Update`; async results carry a sequence number and stale ones are
  dropped.
- Change contracts in `proto/` and run `make generate`, which re-enters the flake's dev shell so the
  plugin versions come from `flake.lock`; never hand-edit `gen/`.
- Embed huh (forward messages, inspect `State`, handle `esc`; a completed form renders `""`, so show a spinner),
  replace the table key map, size `MultiSelect` height from the item count, and measure with `lipgloss.Width`,
  never `len`.
- Never print, echo, log, or commit keys or tokens, never pass them in arguments or environment variables, and
  never show more than whether a key is set.
- `docs/src/content/docs/reference/cli/**` and `docs/src/content/docs/reference/config.md` are generated;
  change the command or the structs, run `make docs-generate`, and commit the output. Keep the console pages in
  `docs/` user-facing: no internal service or RPC names, and no real keys in examples.
- Tests use the standard library only, isolate with `t.Setenv(config.DirEnv, t.TempDir())`, and use a fake
  `/v1/models` server — never the real `~/.noket` or upstreams.

## Commands

- Dev shell: `nix develop` (or `direnv allow`).
- Build: `make build` (`nix build .#noket` → `result/bin/{noket,noketd}`) or `make build-local` for `bin/`.
- Check: `make check` (gofumpt check, `golangci-lint`, `go test`); separately `make fmt`, `make lint`,
  `make test`, `make test-race`.
- One test: `go test ./internal/tui -run TestName`.
- Proto: `make proto-lint` and `make generate`.
- Build without touching `result`: `nix build .#noket --no-link --print-out-paths`.
- Local daemon: `go run ./cmd/noket up --foreground` (temp `NOKET_CONFIG_DIR` unless running for real).
- Docs: `make docs-generate`, `make docs-check`, `make docs-lint`, `make docs-typecheck`,
  `make docs-dev`, `make docs-build`; the site lives in `docs/`.

## Validation

- After `internal/`, `cmd/`, or `proto/` changes: `make check`; proto changes also need `make proto-lint` and
  `make generate`.
- After CLI changes: keep `internal/cli` tests green — they pin the status and error wording.
- After `internal/noketd` changes: the package tests drive registration and stop with real child processes; keep
  error strings byte-identical to the CLI's.
- After `internal/tui` changes: cover the key routing, modal state, and layout you touched; check the header,
  popups, and help line at a narrow and a wide terminal.
- After `go.mod`/`go.sum` changes: stage them (the Nix source is `cleanSource`, tracked files only), set
  `vendorHash = pkgs.lib.fakeHash;` in `flake.nix`, run `nix build .#noket`, and paste the `got:` hash back.
  Dependabot pull requests get this from `.github/workflows/vendor-hash.yml`.
- After CLI command or flag changes, and after `internal/types` field changes: `make docs-generate` and commit
  the regenerated pages; `make check` reports stale ones.
- After `docs/` changes: `make docs-check`, `go test ./tools/docgen`, `make docs-lint`,
  `make docs-typecheck`, and `make docs-build`.
- Never claim verification without the format check, vet, and tests; say what you skipped.

## Git

- Conventional Commits, imperative and lower case, e.g. `feat(tui): add a details popup`; one concern per commit.
- Preserve unrelated worktree changes; do not rewrite history or discard changes unless requested.
- Commit only when requested.
