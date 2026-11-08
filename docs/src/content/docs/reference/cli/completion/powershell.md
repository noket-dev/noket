---
title: "noket completion powershell"
description: "Generate the autocompletion script for powershell"
sidebar:
  order: 3
---

Generate the autocompletion script for powershell

Part of [`noket completion`](/reference/cli/completion/).

## Usage

```sh
noket completion powershell [flags]
```

Generate the autocompletion script for powershell. To load completions in your current shell session: noket completion powershell | Out-String | Invoke-Expression To load completions for every new session, add the output of the above command to your powershell profile.

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--no-descriptions` | `bool` | — | disable completion descriptions |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

