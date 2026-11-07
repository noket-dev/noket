# Tech stack

- Go 1.27 (`go.mod`); the nix flake overrides the toolchain with `go_1_27`.
- CLI: `spf13/cobra`. Logging: `rs/zerolog` (JSON to stderr or `noketd.log`).
- Tunnel transport: WebSocket (`gorilla/websocket`) + `lesismal/arpc` with a protobuf codec (`internal/noketd/codec.go`). arpc method paths are plain strings: `/node/register`, `/node/upstream/{list,probe,apply,update,delete}`.
- `connectrpc.com/connect` is used only by `noket login` (AuthService). `proto/noket/tunnel/v1/node.proto` intentionally has no service declaration — noketd speaks arpc, not connect.
- Protobuf: `buf` v2 module rooted at `proto/`, deps `googleapis` and `protovalidate` pinned in `buf.lock`; plugins `protoc-gen-go`, `protoc-gen-connect-go` (`buf.gen.yaml`, `paths=source_relative`).
- Build/release: `flake.nix` (`buildGoModule` with `vendorHash`, `-X main.version`), `.goreleaser.yaml` (darwin/linux/windows, amd64/arm64, CGO off), `Makefile`.
- Config: TOML via `pelletier/go-toml/v2` (YAML was removed). Credentials, node index and status files are JSON.