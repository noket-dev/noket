---
title: Configuration file
description: Every key in config.toml, with its type, default, and meaning.
sidebar:
  order: 1
---

`noket` and `noketd` read their local configuration from `config.toml` in the [config directory](/reference/files/). The file is the source of truth for the upstream list: `noket upstream add|edit|remove` and upstream changes made in the Noket console both update it. Editing it by hand is supported.

The decoder rejects unknown keys, so a typo fails with the line number instead of being silently ignored. `noketd` reloads the file on its next heartbeat; a parse failure keeps the previous configuration. If the file changes while an edit is open, the command stops instead of overwriting the newer file.

## `[api]`

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `base_url` | `string` | `https://api.noket.dev` | Base URL of the Noket control-plane API. |

## `[tunnel]`

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `url` | `string` | `wss://tunnel.noket.dev/ws` | WebSocket URL of the Noket tunnel endpoint. |
| `name` | `string` | — | Name of this machine in the console. Empty uses the host name. |

## `[noketd]`

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `probe_timeout` | `duration string` | `10s` | Timeout for each upstream probe, including model-list discovery. |
| `upstream_write_timeout` | `duration string` | `10s` | Timeout for writing a request to an upstream API. Accepted for compatibility; the current forwarder does not apply it. |
| `probe_max_bytes` | `integer` | `1048576` | Largest response body the probe reads, in bytes. |

## `[[upstreams]]`

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | `string` | — | Local name for this upstream, unique in the file. |
| `protocol` | `string` | — | API dialect: "openai" or "anthropic". |
| `base_url` | `string` | — | Address of the API. May end in /v1. |
| `api_key` | `string` | — | Key sent as a bearer token or x-api-key header. Stored in config.toml, which is created with mode 0600. |
| `headers` | `string map` | — | Extra request headers, for APIs that need more than a key. |
| `models` | `string array` | — | Models are exposed to the cloud and act as routing keys. A model id may appear in only one upstream. |

## Full example

The file below is [`config.example.toml`](https://github.com/noket-dev/noket/blob/main/config.example.toml) from the repository.

```toml
# Example local configuration for noketd.
#
# Path: ~/.noket/config.toml (Windows: %USERPROFILE%\.noket\config.toml).
# Containers and tests can override the directory with NOKET_CONFIG_DIR.
#
# Usually maintained by `noket upstream add/edit/remove`; editing by hand is optional.
# noketd reloads this file on the next heartbeat; a parse failure keeps the old config.

[api]
base_url = 'https://api.noket.dev'

[tunnel]
url = 'wss://tunnel.noket.dev/ws'
# Node display name; empty uses the hostname. Display only; nodeId is unchanged.
name = 'mac-studio'

[noketd]
probe_timeout = '10s'
upstream_write_timeout = '10s'
probe_max_bytes = 1048576

[[upstreams]]
name = 'ollama'
protocol = 'openai'
base_url = 'http://127.0.0.1:11434'
api_key = '<local-key>'
models = ['qwen3:32b', 'llama3.1:8b']

[[upstreams]]
name = 'vllm'
protocol = 'openai'
base_url = 'http://127.0.0.1:8000'
api_key = ''
models = ['mistral-7b-instruct']
```
