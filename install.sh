#!/usr/bin/env bash
#
# install.sh installs noket and noketd, puts them on PATH, and enables the
# noket agent at login.
#
# macOS:   launchd user agent (~/Library/LaunchAgents/dev.noket.agent.plist)
# Linux:   systemd user unit (~/.config/systemd/user/noket.service)
# Windows: Git Bash / MSYS hands off to install.ps1; native Windows users
#          should run the PowerShell one-liner written by --help.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/noket-dev/noket/main/install.sh | bash
#   ./install.sh --version v0.1.0 --prefix ~/.local/bin --no-autostart
#   ./install.sh --uninstall [--purge]
#
set -euo pipefail

REPO="noket-dev/noket"
REF="${NOKET_INSTALL_REF:-main}"
SERVICE_LABEL="dev.noket.agent"
SERVICE_UNIT="noket.service"
MARKER_BEGIN="# noket"
MARKER_END="# noket end"

VERSION="${NOKET_VERSION:-}"
PREFIX="${NOKET_INSTALL_DIR:-}"
DO_AUTOSTART=1
DO_LOGIN=1
DO_COMPLETION=1
DO_PURGE=0
ACTION="install"

OS=""
ARCH=""
DEST=""
TAG=""
ARCHIVE_VERSION=""
WORKDIR=""
RC_FILE=""
RC_CHANGED=0
PATH_CHANGED=0
PATH_HINT=""
RC_PATH_LINE=""
RC_COMPLETION_LINE=""

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	BOLD=$'\033[1m'
	DIM=$'\033[2m'
	RED=$'\033[31m'
	GREEN=$'\033[32m'
	YELLOW=$'\033[33m'
	RESET=$'\033[0m'
else
	BOLD=""
	DIM=""
	RED=""
	GREEN=""
	YELLOW=""
	RESET=""
fi

cleanup() {
	if [ -n "$WORKDIR" ] && [ -d "$WORKDIR" ]; then
		rm -rf "$WORKDIR"
	fi
}
trap cleanup EXIT

say() { printf '%s\n' "$*"; }
step() { printf '%s==>%s %s\n' "$GREEN" "$RESET" "$*"; }
note() { printf '%s==>%s %s\n' "$DIM" "$RESET" "$*"; }
warn() { printf '%s==> warning:%s %s\n' "$YELLOW" "$RESET" "$*" >&2; }
die() {
	printf '%s==> error:%s %s\n' "$RED" "$RESET" "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Install noket and enable the background agent at login.

Usage:
  install.sh [options]
  install.sh --uninstall [options]

Options:
  --version vX.Y.Z   install a specific release (default: latest)
  --prefix DIR       install directory for the binaries (default: ~/.local/bin)
  --no-autostart     install the binaries but do not enable the agent at login
  --no-login         skip the interactive browser login
  --no-completion    do not install shell completions
  --uninstall        stop and remove the agent, binaries, and shell setup
  --purge            with --uninstall, also remove ~/.noket (credentials, config)
  -h, --help         show this help

Environment:
  NOKET_VERSION       same as --version
  NOKET_INSTALL_DIR   same as --prefix
  NOKET_INSTALL_REF   git ref used to fetch install.ps1 on Windows (default: main)

Windows:
  Run install.sh from Git Bash, or run this in PowerShell:
    irm https://raw.githubusercontent.com/noket-dev/noket/main/install.ps1 | iex
EOF
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--version)
			[ $# -ge 2 ] || die "--version needs a value"
			VERSION="$2"
			shift 2
			;;
		--version=*)
			VERSION="${1#*=}"
			shift
			;;
		--prefix)
			[ $# -ge 2 ] || die "--prefix needs a value"
			PREFIX="$2"
			shift 2
			;;
		--prefix=*)
			PREFIX="${1#*=}"
			shift
			;;
		--no-autostart)
			DO_AUTOSTART=0
			shift
			;;
		--no-login)
			DO_LOGIN=0
			shift
			;;
		--no-completion)
			DO_COMPLETION=0
			shift
			;;
		--uninstall)
			ACTION="uninstall"
			shift
			;;
		--purge)
			DO_PURGE=1
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1 (see --help)"
			;;
		esac
	done
}

detect_platform() {
	local uname_s uname_m
	uname_s=$(uname -s)
	uname_m=$(uname -m)

	case "$uname_s" in
	Darwin) OS=darwin ;;
	Linux) OS=linux ;;
	FreeBSD) OS=freebsd ;;
	MINGW* | MSYS* | CYGWIN*) OS=windows ;;
	*) die "unsupported operating system: $uname_s" ;;
	esac

	case "$uname_m" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	armv7l | armv7*) ARCH=armv7 ;;
	armv6l | armv6*) ARCH=armv6 ;;
	armv5l | armv5*) ARCH=armv5 ;;
	i386 | i486 | i586 | i686 | x86) ARCH=386 ;;
	riscv64) ARCH=riscv64 ;;
	*) die "unsupported architecture: $uname_m" ;;
	esac

	# GoReleaser does not build these combinations.
	if [ "$OS" = darwin ]; then
		case "$ARCH" in
		amd64 | arm64) ;;
		*) die "there is no macOS release for $ARCH" ;;
		esac
	fi
	if [ "$OS" = windows ]; then
		case "$ARCH" in
		amd64 | arm64 | 386) ;;
		*) die "there is no Windows release for $ARCH" ;;
		esac
	fi
}

resolve_prefix() {
	if [ -z "$PREFIX" ]; then
		PREFIX="$HOME/.local/bin"
	fi
	case "$PREFIX" in
	"~") PREFIX="$HOME" ;;
	"~/"*) PREFIX="$HOME/${PREFIX#~/}" ;;
	esac
	case "$PREFIX" in
	/*) ;;
	*) PREFIX="$PWD/$PREFIX" ;;
	esac
	PREFIX="${PREFIX%/}"
	[ -n "$PREFIX" ] || PREFIX="/"
	DEST="$PREFIX"
}

download() {
	local url="$1" out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 --connect-timeout 15 -o "$out" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$out" "$url"
	else
		die "curl or wget is required"
	fi
}

resolve_version() {
	if [ -z "$VERSION" ]; then
		step "Looking up the latest release"
		VERSION=$(curl -fsSL --connect-timeout 15 \
			"https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
			head -n 1) || true
	fi

	# The anonymous API is rate limited; the releases page redirect still
	# names the latest tag.
	if [ -z "$VERSION" ]; then
		local final
		final=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
			"https://github.com/$REPO/releases/latest" 2>/dev/null) || true
		case "$final" in
		*/tag/v*) VERSION="${final##*/}" ;;
		esac
	fi

	[ -n "$VERSION" ] || die "could not determine the latest version; pass --version vX.Y.Z"

	case "$VERSION" in
	v*) ;;
	*) VERSION="v$VERSION" ;;
	esac
	TAG="$VERSION"
	ARCHIVE_VERSION="${VERSION#v}"
}

verify_checksum() {
	local file="$1" list="$2" name="$3" expected actual
	expected=$(awk -v name="$name" '$2 == name { print $1 }' "$list" | head -n 1)
	[ -n "$expected" ] || die "checksums.txt has no entry for $name"

	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$file" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$file" | awk '{print $1}')
	elif command -v sha256 >/dev/null 2>&1; then
		# FreeBSD and other BSDs ship `sha256` rather than `sha256sum`.
		actual=$(sha256 "$file" | awk '{print $NF}')
	elif command -v openssl >/dev/null 2>&1; then
		actual=$(openssl dgst -sha256 "$file" | awk '{print $NF}')
	else
		die "no SHA-256 tool found (need sha256sum, shasum, or openssl)"
	fi

	[ "$expected" = "$actual" ] || die "checksum mismatch for $name"
	note "Checksum verified"
}

install_bin() {
	local src="$1" name dest
	name=$(basename "$1")
	dest="$DEST/$name"
	if ! mkdir -p "$DEST" 2>/dev/null; then
		step "$DEST is not writable; using sudo"
		sudo mkdir -p "$DEST"
	fi
	if [ -w "$DEST" ]; then
		install -m 0755 "$src" "$dest"
	else
		step "$DEST is not writable; using sudo"
		sudo install -m 0755 "$src" "$dest"
	fi
}

config_dir() {
	printf '%s' "${NOKET_CONFIG_DIR:-$HOME/.noket}"
}

has_systemd() {
	command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]
}

stop_existing() {
	if [ -x "$DEST/noket" ]; then
		"$DEST/noket" down >/dev/null 2>&1 || true
	fi

	case "$OS" in
	darwin)
		launchctl bootout "gui/$(id -u)/$SERVICE_LABEL" >/dev/null 2>&1 || true
		launchctl unload "$HOME/Library/LaunchAgents/$SERVICE_LABEL.plist" >/dev/null 2>&1 || true
		;;
	linux)
		if has_systemd; then
			systemctl --user stop "$SERVICE_UNIT" >/dev/null 2>&1 || true
			if [ "$(id -u)" -eq 0 ]; then
				systemctl stop "$SERVICE_UNIT" >/dev/null 2>&1 || true
			fi
		fi
		;;
	esac
}

install_bins() {
	stop_existing

	local asset="noket_${OS}_${ARCH}_${ARCHIVE_VERSION}.tar.gz"
	local base="https://github.com/$REPO/releases/download/$TAG"

	WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/noket-install.XXXXXX")

	step "Downloading noket $TAG for $OS/$ARCH"
	download "$base/$asset" "$WORKDIR/$asset"
	download "$base/checksums.txt" "$WORKDIR/checksums.txt"
	verify_checksum "$WORKDIR/$asset" "$WORKDIR/checksums.txt" "$asset"

	tar -xzf "$WORKDIR/$asset" -C "$WORKDIR"
	[ -f "$WORKDIR/noket" ] || die "archive did not contain the noket binary"
	[ -f "$WORKDIR/noketd" ] || die "archive did not contain the noketd binary"

	step "Installing to $DEST"
	install_bin "$WORKDIR/noket"
	install_bin "$WORKDIR/noketd"
}

fish_quote() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/\\\\'/g")"
}

# Blank lines are held back so the one the block was written with disappears
# too, which keeps reinstalls from piling up empty lines.
remove_path_block() {
	local file="$1" tmp="$1.noket.tmp"
	[ -f "$file" ] || return 0
	awk -v b="$MARKER_BEGIN" -v e="$MARKER_END" '
		$0 == b { skip = 1; held = 0; next }
		$0 == e { skip = 0; next }
		skip { next }
		$0 == "" { held++; next }
		{ for (i = 0; i < held; i++) print ""; held = 0; print }
		END { for (i = 0; i < held; i++) print "" }
	' "$file" >"$tmp" && mv "$tmp" "$file"
}

shell_name() {
	basename "${SHELL:-sh}"
}

rc_file_for_shell() {
	case "$(shell_name)" in
	zsh) printf '%s' "$HOME/.zshrc" ;;
	bash)
		if [ "$OS" = darwin ] && [ -f "$HOME/.bash_profile" ]; then
			printf '%s' "$HOME/.bash_profile"
		else
			printf '%s' "$HOME/.bashrc"
		fi
		;;
	*) printf '%s' "$HOME/.profile" ;;
	esac
}

ensure_path() {
	case ":$PATH:" in
	*":$DEST:"*)
		note "$DEST is already on PATH"
		return 0
		;;
	esac

	if [ "$(shell_name)" = fish ]; then
		local fish_dir="$HOME/.config/fish/conf.d"
		mkdir -p "$fish_dir"
		printf 'fish_add_path %s\n' "$(fish_quote "$DEST")" >"$fish_dir/noket.fish"
		RC_FILE="$fish_dir/noket.fish"
		RC_CHANGED=1
		PATH_HINT="fish_add_path $(fish_quote "$DEST")"
		PATH_CHANGED=1
		note "Added $DEST to PATH in $RC_FILE"
		return 0
	fi

	RC_PATH_LINE="export PATH=\"$DEST:\$PATH\""
	PATH_HINT="$RC_PATH_LINE"
	PATH_CHANGED=1
}

completion_dir() {
	printf '%s' "${XDG_DATA_HOME:-$HOME/.local/share}/noket"
}

# The script is written once at install time rather than generated by the shell
# at startup: it is faster, and macOS ships bash 3.2, which fails to source a
# completion script from a pipe.
install_completion() {
	[ "$DO_COMPLETION" = 1 ] || return 0
	[ -x "$DEST/noket" ] || return 0

	local sh
	sh=$(shell_name)

	if [ "$sh" = fish ]; then
		local dir="$HOME/.config/fish/completions"
		mkdir -p "$dir"
		if "$DEST/noket" completion fish >"$dir/noket.fish" 2>/dev/null; then
			note "Added shell completions in $dir"
		else
			rm -f "$dir/noket.fish"
			warn "could not write fish completions"
		fi
		return 0
	fi

	case "$sh" in
	bash | zsh) ;;
	*) return 0 ;;
	esac

	local dir file
	dir=$(completion_dir)
	file="$dir/completions.$sh"
	mkdir -p "$dir"
	if ! "$DEST/noket" completion "$sh" >"$file" 2>/dev/null; then
		rm -f "$file"
		warn "could not write $sh completions"
		return 0
	fi

	RC_COMPLETION_LINE="[ -r \"$file\" ] && source \"$file\""
	if [ "$sh" = zsh ]; then
		# Stock macOS zsh never runs compinit, and the script needs compdef.
		RC_COMPLETION_LINE="(( \$+functions[compdef] )) || { autoload -Uz compinit && compinit -q; }
$RC_COMPLETION_LINE"
	fi
	RC_CHANGED=1
}

# One marker block holds both lines, so a reinstall replaces them and an
# uninstall removes them together.
write_rc() {
	if [ -z "$RC_PATH_LINE" ] && [ -z "$RC_COMPLETION_LINE" ]; then
		return 0
	fi
	if [ -z "$RC_FILE" ]; then
		RC_FILE=$(rc_file_for_shell)
	fi

	mkdir -p "$(dirname "$RC_FILE")"
	touch "$RC_FILE"
	remove_path_block "$RC_FILE"
	{
		printf '\n%s\n' "$MARKER_BEGIN"
		if [ -n "$RC_PATH_LINE" ]; then
			printf '%s\n' "$RC_PATH_LINE"
		fi
		if [ -n "$RC_COMPLETION_LINE" ]; then
			printf '%s\n' "$RC_COMPLETION_LINE"
		fi
		printf '%s\n' "$MARKER_END"
	} >>"$RC_FILE"
	RC_CHANGED=1

	if [ -n "$RC_PATH_LINE" ]; then
		note "Added $DEST to PATH in $RC_FILE"
	fi
	if [ -n "$RC_COMPLETION_LINE" ]; then
		note "Added shell completions to $RC_FILE"
	fi
}

# A later install directory does not win over an earlier one on PATH, so a
# second copy of noket can keep running after an upgrade.
check_shadowing() {
	local found dir
	found=$(command -v noket 2>/dev/null) || return 0
	dir=$(cd "$(dirname "$found")" 2>/dev/null && pwd) || return 0
	[ "$dir" = "$DEST" ] && return 0
	warn "an older noket runs first: $found"
	warn "remove it, or put $DEST before $dir on PATH"
}

maybe_login() {
	[ "$DO_LOGIN" = 1 ] || return 0

	local creds
	creds="$(config_dir)/credentials.json"
	if [ -f "$creds" ]; then
		note "Already logged in"
		return 0
	fi

	# Under `curl | bash` stdin is the script, so prompts need the terminal.
	if [ -r /dev/tty ]; then
		step "No login found; starting the browser login"
		if ! "$DEST/noket" login </dev/tty; then
			warn "login did not finish; run 'noket login' later"
		fi
	else
		warn "no login found; run 'noket login' before the agent can connect"
	fi
}

xml_escape() {
	printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

systemd_exec() {
	printf '"%s"' "$(printf '%s' "$1" | sed -e 's/%/%%/g' -e 's/"/\\"/g')"
}

macos_service() {
	local uid plist log
	uid=$(id -u)
	plist="$HOME/Library/LaunchAgents/$SERVICE_LABEL.plist"
	log="$(config_dir)/noketd.log"

	mkdir -p "$HOME/Library/LaunchAgents" "$(config_dir)"
	chmod 700 "$(config_dir)" 2>/dev/null || true
	touch "$log"
	chmod 600 "$log" 2>/dev/null || true

	{
		cat <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$SERVICE_LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$(xml_escape "$DEST/noket")</string>
		<string>up</string>
		<string>--foreground</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>$(xml_escape "$log")</string>
	<key>StandardErrorPath</key>
	<string>$(xml_escape "$log")</string>
EOF
		if [ -n "${NOKET_CONFIG_DIR:-}" ]; then
			cat <<EOF
	<key>EnvironmentVariables</key>
	<dict>
		<key>NOKET_CONFIG_DIR</key>
		<string>$(xml_escape "$NOKET_CONFIG_DIR")</string>
	</dict>
EOF
		fi
		cat <<EOF
</dict>
</plist>
EOF
	} >"$plist"

	launchctl bootout "gui/$uid/$SERVICE_LABEL" >/dev/null 2>&1 || true
	if launchctl bootstrap "gui/$uid" "$plist" >/dev/null 2>&1; then
		step "Agent enabled (launchd: $SERVICE_LABEL); it starts at login"
	elif launchctl load -w "$plist" >/dev/null 2>&1; then
		step "Agent enabled (launchd: $SERVICE_LABEL); it starts at login"
	else
		warn "saved $plist, but launchd is unreachable now; it loads at the next login"
	fi
}

linux_user_unit() {
	local unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
	local unit="$unit_dir/$SERVICE_UNIT" env_line=""

	mkdir -p "$unit_dir"
	if [ -n "${NOKET_CONFIG_DIR:-}" ]; then
		env_line="Environment=\"NOKET_CONFIG_DIR=$(printf '%s' "$NOKET_CONFIG_DIR" | sed 's/%/%%/g')\""
	fi

	cat >"$unit" <<EOF
[Unit]
Description=Noket tunnel agent
Documentation=https://docs.noket.dev
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$(systemd_exec "$DEST/noket") up --foreground
Restart=on-failure
RestartSec=30
NoNewPrivileges=true
$env_line

[Install]
WantedBy=default.target
EOF

	systemctl --user daemon-reload >/dev/null 2>&1 || true
	if systemctl --user enable "$SERVICE_UNIT" >/dev/null 2>&1; then
		step "Agent enabled (systemd user unit: $SERVICE_UNIT)"
	else
		warn "could not enable $SERVICE_UNIT now; enable it with: systemctl --user enable $SERVICE_UNIT"
	fi

	if ! systemctl --user restart "$SERVICE_UNIT" >/dev/null 2>&1; then
		warn "could not start the agent now; start it with: systemctl --user start $SERVICE_UNIT"
	fi

	# Without linger a user unit starts at login, not at boot.
	if ! loginctl enable-linger "$(id -un)" >/dev/null 2>&1; then
		warn "could not enable linger; the agent starts at login instead of at boot"
	fi
}

linux_system_unit() {
	local unit="/etc/systemd/system/$SERVICE_UNIT" env_line=""

	if [ -n "${NOKET_CONFIG_DIR:-}" ]; then
		env_line="Environment=\"NOKET_CONFIG_DIR=$(printf '%s' "$NOKET_CONFIG_DIR" | sed 's/%/%%/g')\""
	fi

	cat >"$unit" <<EOF
[Unit]
Description=Noket tunnel agent
Documentation=https://docs.noket.dev
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$(systemd_exec "$DEST/noket") up --foreground
Restart=on-failure
RestartSec=30
NoNewPrivileges=true
$env_line

[Install]
WantedBy=multi-user.target
EOF

	systemctl daemon-reload >/dev/null 2>&1 || true
	if systemctl enable --now "$SERVICE_UNIT" >/dev/null 2>&1; then
		step "Agent enabled (systemd system unit: $SERVICE_UNIT); it starts at boot"
	else
		warn "could not start $SERVICE_UNIT; start it with: systemctl start $SERVICE_UNIT"
	fi
}

configure_autostart() {
	if [ "$DO_AUTOSTART" = 0 ]; then
		note "Autostart disabled (--no-autostart)"
		return 0
	fi

	case "$OS" in
	darwin) macos_service ;;
	linux)
		if has_systemd; then
			if [ "$(id -u)" -eq 0 ]; then
				linux_system_unit
			else
				linux_user_unit
			fi
		else
			warn "systemd not found; autostart skipped. Start the agent with: $DEST/noket up"
		fi
		;;
	freebsd)
		warn "no autostart integration for FreeBSD; start the agent with: $DEST/noket up"
		;;
	esac
}

remove_bin() {
	local file="$DEST/$1"
	[ -e "$file" ] || return 0
	if [ -w "$DEST" ]; then
		rm -f "$file"
	else
		sudo rm -f "$file"
	fi
}

uninstall_unix() {
	stop_existing

	case "$OS" in
	darwin)
		rm -f "$HOME/Library/LaunchAgents/$SERVICE_LABEL.plist"
		;;
	linux)
		if has_systemd; then
			if [ "$(id -u)" -eq 0 ]; then
				systemctl disable --now "$SERVICE_UNIT" >/dev/null 2>&1 || true
				rm -f "/etc/systemd/system/$SERVICE_UNIT"
				systemctl daemon-reload >/dev/null 2>&1 || true
			else
				systemctl --user disable --now "$SERVICE_UNIT" >/dev/null 2>&1 || true
				rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$SERVICE_UNIT"
				systemctl --user daemon-reload >/dev/null 2>&1 || true
			fi
		fi
		;;
	esac

	remove_path_block "$HOME/.zshrc"
	remove_path_block "$HOME/.bashrc"
	remove_path_block "$HOME/.bash_profile"
	remove_path_block "$HOME/.profile"
	rm -f "$HOME/.config/fish/conf.d/noket.fish"
	rm -f "$HOME/.config/fish/completions/noket.fish"
	local cdir
	cdir=$(completion_dir)
	rm -f "$cdir/completions.bash" "$cdir/completions.zsh"
	rmdir "$cdir" 2>/dev/null || true

	remove_bin noket
	remove_bin noketd

	if [ "$DO_PURGE" = 1 ]; then
		local cfg
		cfg="$(config_dir)"
		if [ -n "$cfg" ] && [ "$cfg" != "/" ] && [ "$cfg" != "$HOME" ]; then
			rm -rf "$cfg"
			step "Removed $cfg"
		else
			warn "refusing to remove $cfg; delete it manually if that is what you want"
		fi
	fi

	step "noket was uninstalled from $DEST"
}

handoff_windows() {
	local url="https://raw.githubusercontent.com/$REPO/$REF/install.ps1"

	if command -v powershell.exe >/dev/null 2>&1; then
		step "Windows detected; handing off to install.ps1"
		WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/noket-install.XXXXXX")
		download "$url" "$WORKDIR/install.ps1"

		local script
		if command -v cygpath >/dev/null 2>&1; then
			script=$(cygpath -w "$WORKDIR/install.ps1")
		else
			script="$WORKDIR/install.ps1"
		fi

		set -- -NoProfile -ExecutionPolicy Bypass -File "$script"
		if [ -n "$VERSION" ]; then
			set -- "$@" -Version "$VERSION"
		fi
		if [ -n "$PREFIX" ]; then
			# install.ps1 needs a Windows path, not the MSYS one.
			case "$PREFIX" in
			"~") PREFIX="$HOME" ;;
			"~/"*) PREFIX="$HOME/${PREFIX#~/}" ;;
			esac
			if command -v cygpath >/dev/null 2>&1; then
				PREFIX=$(cygpath -w "$PREFIX")
			fi
			set -- "$@" -InstallDir "$PREFIX"
		fi
		[ "$DO_AUTOSTART" = 0 ] && set -- "$@" -NoAutostart
		[ "$DO_LOGIN" = 0 ] && set -- "$@" -NoLogin
		[ "$DO_COMPLETION" = 0 ] && set -- "$@" -NoCompletion
		[ "$DO_PURGE" = 1 ] && set -- "$@" -Purge
		[ "$ACTION" = uninstall ] && set -- "$@" -Uninstall

		powershell.exe "$@"
		return 0
	fi

	say "Windows installation needs PowerShell. Run this in PowerShell (as your user):"
	say ""
	say "  irm $url | iex"
	return 1
}

finish() {
	check_shadowing

	say ""
	step "${BOLD}noket $TAG${RESET} installed to $DEST"
	say ""

	if [ "$PATH_CHANGED" = 1 ] && [ -n "$PATH_HINT" ]; then
		say "To use noket in this shell now, run one of:"
		say ""
		say "  $PATH_HINT"
		say ""
	fi

	if [ "$RC_CHANGED" = 1 ] && [ -n "$RC_FILE" ]; then
		say "Open a new terminal to pick it up automatically, or run: source $RC_FILE"
		say ""
	fi

	say "  noket login    sign in (if you have not already)"
	say "  noket status   check the tunnel"
	say "  noket tui      manage upstreams"

	if [ "$DO_AUTOSTART" = 1 ]; then
		say ""
		case "$OS" in
		darwin)
			say "The agent starts at login and reconnects automatically."
			say "Logs: $(config_dir)/noketd.log"
			;;
		linux)
			if [ "$(id -u)" -eq 0 ]; then
				say "The agent starts at boot and reconnects automatically."
				say "Logs: journalctl -u $SERVICE_UNIT -f"
			else
				say "The agent starts at login (at boot with linger) and reconnects automatically."
				say "Logs: journalctl --user -u $SERVICE_UNIT -f"
			fi
			;;
		esac
	fi

	if [ "$DO_AUTOSTART" = 1 ] && [ ! -f "$(config_dir)/credentials.json" ]; then
		say ""
		say "The agent waits until you run: noket login"
	fi
}

main() {
	parse_args "$@"

	# The daemon runs with a different working directory than this script, so
	# give a relative override an absolute meaning now.
	case "${NOKET_CONFIG_DIR:-}" in
	"" | /*) ;;
	*) export NOKET_CONFIG_DIR="$PWD/$NOKET_CONFIG_DIR" ;;
	esac

	# A user-level agent must not be installed as root through sudo.
	if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ]; then
		die "do not run this with sudo; install into your home and use --prefix for a system location"
	fi

	detect_platform

	if [ "$OS" = windows ]; then
		handoff_windows
		exit $?
	fi

	resolve_prefix

	if [ "$ACTION" = uninstall ]; then
		uninstall_unix
		exit 0
	fi

	resolve_version
	install_bins
	ensure_path
	install_completion
	write_rc
	maybe_login
	configure_autostart
	finish
}

main "$@"
