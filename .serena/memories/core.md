# noket

User-side client that connects local LLM APIs to the Noket tunnel. Two binaries:
- `cmd/noket`: cobra CLI — login, up/down, status, upstream add/edit/list/test/remove.
- `cmd/noketd`: long-running daemon — WebSocket to the gateway, registers the node, forwards requests, serves management RPCs.

`noket up` starts noketd detached and waits for the first registration; `noket up --foreground` leaves supervision to an external process manager.

## Source map
- `internal/cli/`: command tree, prompt helpers, process start/stop per platform.
- `internal/noketd/`: connection lifecycle (`daemon.go`), forwarding (`forward/`), management handlers (`handlers.go`, `upstream.go`), arpc protobuf codec (`codec.go`), connection wrapper (`wsconn.go`).
- `internal/config/`: TOML config, atomic writes, live snapshot store, credentials/node/status files.
- `internal/types/`: config and local-state structs.
- `internal/probe`, `internal/forward`, `internal/netguard`, `internal/rpc`: data plane and shared utilities.
- `proto/` + `gen/`: protocol definitions and generated Go; never edit `gen/` by hand.

## Invariants
- The local `config.toml` is the ONLY source of the upstream list; the cloud keeps no copy.
- One node id (UUID v7) per account in `node.json`; the server binds a node id to the account that first registered it, so a rotated id is issued when ownership conflicts.
- All config mutations inside noketd go through `config.Store.Mutate` (serialized, validated, atomic rename). Reloads trigger on mtime/size change and keep the previous snapshot on parse or validation errors.
- `captured_at` in `GetNodeUpstreamsResponse` is when the config was last loaded successfully, not when the response was produced.
- API keys live only in `~/.noket/config.toml` (0600). They are never logged or reported to the cloud; responses expose `has_api_key` only. Probe/create/update carry a key transiently in the request body.
- Files under `~/.noket` are 0600 and the directory 0700; `NOKET_CONFIG_DIR` overrides the directory (tests rely on this).
- Upstream `base_url` may end in `/v1`; `netguard.JoinPath` de-duplicates the version segment when joining an ingress path. There is no address filtering: any host/IP (loopback, private, public, link-local, metadata) is used as given; dialing uses the resolved IP directly, ignores proxy env vars, and refuses cross-host redirects.

See `mem:tech_stack`, `mem:suggested_commands`, `mem:conventions`, `mem:task_completion`.