---
title: "noket upstream edit"
description: "Edit an upstream"
sidebar:
  order: 2
---

Edit an upstream

Part of [`noket upstream`](/reference/cli/upstream/).

## Usage

```sh
noket upstream edit <name> [flags]
```

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--api-key-stdin` | `bool` | — | read one API key line from stdin |
| `--clear-api-key` | `bool` | — | clear the saved API key |
| `--model` | `string array` | — | complete model list (repeatable) |
| `--new-name` | `string` | — | rename the upstream |
| `--protocol` | `string` | — | openai or anthropic |
| `--url` | `string` | — | upstream base URL (with or without /v1) |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

