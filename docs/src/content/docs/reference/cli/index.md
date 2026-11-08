---
title: "noket"
description: "Connect local LLM APIs to the Noket tunnel"
---

Connect local LLM APIs to the Noket tunnel

## Usage

```sh
noket <command> [flags]
```

## Commands

| Command | Description |
| --- | --- |
| [`completion`](/reference/cli/completion/) | Generate the autocompletion script for the specified shell |
| [`down`](/reference/cli/down/) | Stop the noketd started by noket up |
| [`login`](/reference/cli/login/) | Log in through the browser and save local credentials |
| [`logout`](/reference/cli/logout/) | Remove the local login state |
| [`status`](/reference/cli/status/) | Show the logged-in user, node, and connection status |
| [`tui`](/reference/cli/tui/) | Manage upstreams and noketd in a full-screen terminal UI |
| [`up`](/reference/cli/up/) | Start noketd, connect to the tunnel, and register the node |
| [`upstream`](/reference/cli/upstream/) | Manage local upstream LLM APIs |

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--version` | `bool` | — | version for noket |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

