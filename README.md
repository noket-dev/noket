# noket

`noket` connects LLM APIs running on your own machines to [Noket](https://noket.dev). Models served by Ollama, vLLM,
LM Studio, or any OpenAI- or Anthropic-compatible API can be used from your Noket account while inference stays on
your hardware.

The repository ships two binaries: `noket`, the command-line client, and `noketd`, the daemon that keeps the connector running.

## Install

macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/noket-dev/noket/main/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/noket-dev/noket/main/install.ps1 | iex
```

The installers download the release for this platform, verify its SHA-256 checksum, install `noket` and `noketd`,
add them to `PATH`, install shell completions for the current shell, and enable the connector at login: launchd on
macOS, a systemd user unit on Linux, and a Scheduled Task on Windows. Run `noket login` when the installer
cannot open a browser; the connector waits for it.

Use `--no-autostart` (`-NoAutostart`) to skip autostart, `--no-completion` (`-NoCompletion`) to skip shell
completions, `--prefix DIR` (`-InstallDir DIR`) to choose the install directory, and `--uninstall [--purge]`
(`-Uninstall -Purge`) to remove everything.

## Documentation

Installation, configuration, and usage are documented at **https://docs.noket.dev**.

## Releases

Binaries for macOS, Linux, and Windows are published on the
[releases page](https://github.com/noket-dev/noket/releases).

## License

Licensed under the [MIT License](LICENSE).
