---
title: "noket upstream add"
description: "Add an upstream; edit it when the name already exists"
sidebar:
  order: 1
---

Add an upstream; edit it when the name already exists

Part of [`noket upstream`](/reference/cli/upstream/).

## Usage

```sh
noket upstream add [flags]
```

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--api-key-stdin` | `bool` | — | read one API key line from stdin |
| `--model` | `string array` | — | model id to expose (repeatable) |
| `--name` | `string` | — | Upstream name |
| `--protocol` | `string` | `openai` | openai or anthropic |
| `--url` | `string` | — | upstream base URL (with or without /v1) |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

