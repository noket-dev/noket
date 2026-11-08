---
title: "noket up"
description: "Start noketd, connect to the tunnel, and register the node"
sidebar:
  order: 7
---

Start noketd, connect to the tunnel, and register the node

Part of [`noket`](/reference/cli/).

## Usage

```sh
noket up [flags]
```

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--foreground` | `bool` | — | run in the foreground, write no pid file, leave supervision to systemd / Docker, and wait for `noket login` instead of exiting |
| `--name` | `string` | — | override the node display name (does not create a second node) |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

