---
title: "noket completion fish"
description: "Generate the autocompletion script for fish"
sidebar:
  order: 2
---

Generate the autocompletion script for fish

Part of [`noket completion`](/reference/cli/completion/).

## Usage

```sh
noket completion fish [flags]
```

Generate the autocompletion script for the fish shell. To load completions in your current shell session: noket completion fish | source To load completions for every new session, execute once: noket completion fish > ~/.config/fish/completions/noket.fish You will need to start a new shell for this setup to take effect.

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--no-descriptions` | `bool` | — | disable completion descriptions |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

