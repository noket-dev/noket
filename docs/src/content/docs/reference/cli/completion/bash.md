---
title: "noket completion bash"
description: "Generate the autocompletion script for bash"
sidebar:
  order: 1
---

Generate the autocompletion script for bash

Part of [`noket completion`](/reference/cli/completion/).

## Usage

```sh
noket completion bash
```

Generate the autocompletion script for the bash shell. This script depends on the 'bash-completion' package. If it is not installed already, you can install it via your OS's package manager. To load completions in your current shell session: source <(noket completion bash) To load completions for every new session, execute once: #### Linux: noket completion bash > /etc/bash_completion.d/noket #### macOS: noket completion bash > $(brew --prefix)/etc/bash_completion.d/noket You will need to start a new shell for this setup to take effect.

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--no-descriptions` | `bool` | — | disable completion descriptions |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

