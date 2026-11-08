---
title: "noket completion"
description: "Generate the autocompletion script for the specified shell"
sidebar:
  order: 1
---

Generate the autocompletion script for the specified shell

Part of [`noket`](/reference/cli/).

## Usage

```sh
noket completion <command> [flags]
```

Generate the autocompletion script for noket for the specified shell. See each sub-command's help for details on how to use the generated script.

## Commands

| Command | Description |
| --- | --- |
| [`bash`](/reference/cli/completion/bash/) | Generate the autocompletion script for bash |
| [`fish`](/reference/cli/completion/fish/) | Generate the autocompletion script for fish |
| [`powershell`](/reference/cli/completion/powershell/) | Generate the autocompletion script for powershell |
| [`zsh`](/reference/cli/completion/zsh/) | Generate the autocompletion script for zsh |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

