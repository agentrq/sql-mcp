#!/bin/sh
# Installs the sqlmcp CLI on macOS and Linux:
#
#   curl -fsSL https://raw.githubusercontent.com/tmphost/sql-mcp/main/install.sh | sh
#
# Environment:
#   SQLMCP_VERSION       release tag to install, such as v1.2.3 (default: latest)
#   SQLMCP_INSTALL_DIR   where to put the binary (default: /usr/local/bin when
#                        writable, else $HOME/.local/bin)
#   SQLMCP_DOWNLOAD_URL  base URL holding the release assets, for mirrors and
#                        testing (default: the GitHub release)
set -eu

REPO="tmphost/sql-mcp"

log() { printf '%s\n' "$*" >&2; }
fail() { log "error: $*"; exit 1; }

detect_os() {
	case "$(uname -s)" in
		Linux) echo linux ;;
		Darwin) echo darwin ;;
		*) fail "unsupported OS $(uname -s); on Windows use install.ps1" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) echo amd64 ;;
		arm64 | aarch64) echo arm64 ;;
		*) fail "unsupported architecture $(uname -m)" ;;
	esac
}

download() { # url dest
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		fail "curl or wget is required"
	fi
}

sha256() { # file
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "sha256sum or shasum is required to check the download"
	fi
}

install_dir() {
	if [ -n "${SQLMCP_INSTALL_DIR:-}" ]; then
		echo "$SQLMCP_INSTALL_DIR"
	elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		echo /usr/local/bin
	else
		echo "$HOME/.local/bin"
	fi
}

main() {
	os=$(detect_os)
	arch=$(detect_arch)
	version=${SQLMCP_VERSION:-latest}
	asset="sqlmcp_${os}_${arch}.tar.gz"
	if [ -n "${SQLMCP_DOWNLOAD_URL:-}" ]; then
		base=${SQLMCP_DOWNLOAD_URL%/}
	elif [ "$version" = latest ]; then
		base="https://github.com/$REPO/releases/latest/download"
	else
		base="https://github.com/$REPO/releases/download/$version"
	fi

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	log "Downloading $asset ($version)..."
	download "$base/$asset" "$tmp/$asset" || fail "could not download $base/$asset"
	download "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download $base/checksums.txt"

	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || fail "$asset is not listed in checksums.txt"
	got=$(sha256 "$tmp/$asset")
	[ "$want" = "$got" ] || fail "checksum mismatch for $asset: expected $want, got $got"

	tar -xzf "$tmp/$asset" -C "$tmp" sqlmcp
	dir=$(install_dir)
	mkdir -p "$dir"
	# Copy to a temporary name and rename, so a running sqlmcp is never
	# replaced by half a file.
	cp "$tmp/sqlmcp" "$dir/.sqlmcp.new"
	chmod 755 "$dir/.sqlmcp.new"
	mv "$dir/.sqlmcp.new" "$dir/sqlmcp"
	log "Installed sqlmcp to $dir/sqlmcp"

	case ":$PATH:" in
		*":$dir:"*) ;;
		*) log "Add $dir to your PATH, e.g.: export PATH=\"$dir:\$PATH\"" ;;
	esac
	log 'Run "sqlmcp login --server https://your-sql-mcp-server" to get started.'
}

main "$@"
