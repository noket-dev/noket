---
title: "noket upstream"
description: "Manage local upstream LLM APIs"
sidebar:
  order: 8
---

Manage local upstream LLM APIs

Part of [`noket`](/reference/cli/).

## Usage

```sh
noket upstream <command> [flags]
```

## Commands

| Command | Description |
| --- | --- |
| [`add`](/reference/cli/upstream/add/) | Add an upstream; edit it when the name already exists |
| [`edit`](/reference/cli/upstream/edit/) | Edit an upstream |
| [`list`](/reference/cli/upstream/list/) | List upstreams and models |
| [`remove`](/reference/cli/upstream/remove/) | Remove an upstream |
| [`test`](/reference/cli/upstream/test/) | Request the upstream /v1/models endpoint to check the URL and key |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

