# Task completion gates

Run before considering a coding task done:
- `make fmt-check` (gofmt clean) or `make fmt`.
- `make lint` (`go vet ./...`).
- `make test` (`go test ./...`); use `make test-race` for concurrency changes.
- Proto touched: `make proto-lint` and `make generate`, then rebuild and test.
- `go.mod`/`go.sum` touched: update `vendorHash` in `flake.nix` and confirm `nix build .#`.
- Full gate: `make check` (fmt-check + lint + test).

There is no CI configuration in the repo; these local gates are the only checks.