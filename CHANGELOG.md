# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-07

### Added
- `noket login` signs in through a browser approval page the CLI polls, so it works on headless and remote machines.
- `noket logout`, `noket status`, `noket up` / `noket down`, `noket upstream add|edit|list|test|remove`, and the `noket tui` console.
- `noketd` registers this machine with Noket and keeps the tunnel connected, reconnecting with backoff.
- Release archives now ship a software bill of materials (SBOM).

### Changed
- `noket login` no longer starts a localhost callback server or performs a PKCE code exchange: the API returns the complete login URL and the CLI only polls it.
- The `[web]` config table is gone, because the CLI no longer needs to know where the web app lives. A `config.toml` that still contains `[web]` is rejected as unknown.

### Fixed
- `noketd` no longer writes a status file after it has shut down, which could leave a stale status behind.

### Security
- Login secrets are polled over the API and never appear in the browser URL; the browser only ever sees the authorization id.

[0.1.0]: https://github.com/noket-dev/noket/releases/tag/v0.1.0
