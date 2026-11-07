# Conventions

- Errors: wrap with context, `fmt.Errorf("verb %s: %w", path, err)`; sentinel errors live in the owning package (`config.ErrNoCredentials`, `config.ErrNotRunning`, …).
- Config write path is always `ApplyDefaults` → `Normalize` → `Validate` → marshal → atomic rename with 0600 (`config.atomicWrite`).
- TOML decoding is strict (`DisallowUnknownFields`); report unknown keys with line context via `toml.StrictMissingError.String()`.
- Platform-specific code uses `_unix.go` / `_windows.go` suffixes; keep process handling split rather than runtime-switching.
- Tests: `t.TempDir()` + `t.Setenv(config.DirEnv, dir)` for config; integration tests run a fake gateway over arpc (`internal/noketd/integration_test.go`) without real network access.
- Comments state invariants and reasons, not restatements; exported identifiers carry doc comments.
- Proto: snake_case fields, doc comments describing wire semantics; noketd keeps only the message set its arpc handlers exchange. `gen/` is regenerated, never edited.
- `internal/rpc` defines the status codes and the `Status`/`Reply` envelope carried in `UpstreamReply`; handlers always answer through `writeReply`.