# Commands

- Nix build (also `make build`): `nix build .#` → `result/bin/{noket,noketd}`.
- Fast local build: `make build-local` → `bin/`.
- Foreground daemon: `go run ./cmd/noket up --foreground`.
- Tests: `make test` (`go test ./...`); concurrency: `make test-race`.
- Format: `make fmt`; lint: `make lint` (`go vet ./...`).
- Proto: `make proto-lint` (`buf lint`); `make generate` deletes and regenerates `gen/`.
- Release: `goreleaser release --clean`.

Nix: after ANY change to `go.mod`/`go.sum`, update `vendorHash` in `flake.nix` — set it to `pkgs.lib.fakeHash`, run `nix build .#`, copy the `got:` hash from the mismatch error. `buildGoModule` generates `vendor/` itself; the repo has no vendor directory.

Darwin: `stat -f "%Sm %N" <file>` (GNU form does not work); `pgrep -fl noketd` to find a running daemon; daemon log at `~/.noket/noketd.log`.