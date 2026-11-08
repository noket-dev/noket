---
title: "noket completion zsh"
description: "Generate the autocompletion script for zsh"
sidebar:
  order: 4
---

Generate the autocompletion script for zsh

Part of [`noket completion`](/reference/cli/completion/).

## Usage

```sh
noket completion zsh [flags]
```

Generate the autocompletion script for the zsh shell. If shell completion is not already enabled in your environment you will need to enable it. You can execute the following once: echo "autoload -U compinit; compinit" >> ~/.zshrc To load completions in your current shell session: source <(noket completion zsh) To load completions for every new session, execute once: #### Linux: noket completion zsh > "${fpath[1]}/_noket" #### macOS: noket completion zsh > $(brew --prefix)/share/zsh/site-functions/_noket You will need to start a new shell for this setup to take effect.

## Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `--no-descriptions` | `bool` | — | disable completion descriptions |

## Inherited flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-v`, `--verbose` | `bool` | — | enable debug logging |

