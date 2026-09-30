#!/bin/sh
# git-envdiff installer for Linux and macOS.
#
#   curl -fsSL https://github.com/tejjasdev/git-envdiff/releases/latest/download/install.sh | sh
#
# What this script does, in order:
#   1. Detects your OS and CPU architecture.
#   2. Finds the latest release tag (without calling the GitHub API, so it
#      never hits an anonymous rate limit).
#   3. Downloads that release's archive AND its checksums.txt.
#   4. Verifies the archive's SHA-256 checksum before touching anything else.
#   5. Extracts the git-envdiff binary into $INSTALL_DIR (default: ~/.local/bin).
#   6. Writes a small receipt so `git-envdiff update` can update itself later.
#
# Written in plain POSIX sh on purpose: `curl ... | sh` ignores the shebang
# and uses whatever /bin/sh is (dash on Debian/Ubuntu), so bash-only
# features like `pipefail` would break the documented install command.
#
# It never runs with sudo and never installs outside $INSTALL_DIR. Read
# this script before piping it into a shell if you'd like to check that
# yourself: every download comes only from
# github.com/tejjasdev/git-envdiff — nowhere else.
set -eu

REPO="tejjasdev/git-envdiff"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${GIT_ENVDIFF_VERSION:-latest}"
# Overridable for mirrors and for testing this script offline (file:// works).
DOWNLOAD_BASE="${GIT_ENVDIFF_DOWNLOAD_BASE:-https://github.com/${REPO}/releases/download}"

log() { printf '%s\n' "$*"; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "required command '$1' not found"
}

detect_platform() {
	os="$(uname -s)"
	case "$os" in
	Linux) os="linux" ;;
	Darwin) os="darwin" ;;
	*) die "unsupported OS: $os (Windows users: use install.ps1 instead)" ;;
	esac

	arch="$(uname -m)"
	case "$arch" in
	x86_64 | amd64) arch="amd64" ;;
	arm64 | aarch64) arch="arm64" ;;
	*) die "unsupported architecture: $arch" ;;
	esac
}

# resolve_tag prints the latest release's tag (e.g. "v1.2.3") by following
# GitHub's redirect from /releases/latest, which avoids the anonymous
# GitHub API rate limit that hitting api.github.com directly runs into.
resolve_tag() {
	if [ "$VERSION" != "latest" ]; then
		printf '%s\n' "$VERSION"
		return
	fi
	url="https://github.com/${REPO}/releases/latest"
	final="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$url")"
	tag="${final##*/}"
	[ -n "$tag" ] || die "could not determine the latest release tag"
	printf '%s\n' "$tag"
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "need either sha256sum or shasum to verify the download"
	fi
}

main() {
	need curl
	need tar
	need mkdir
	need mktemp

	detect_platform
	tag="$(resolve_tag)"
	version="${tag#v}"
	asset="git-envdiff_${version}_${os}_${arch}.tar.gz"

	base_url="${DOWNLOAD_BASE}/${tag}"
	tmp_dir="$(mktemp -d)"
	trap 'rm -rf "$tmp_dir"' EXIT

	log "Downloading git-envdiff ${tag} for ${os}/${arch}..."
	curl -fsSL -o "$tmp_dir/$asset" "$base_url/$asset" ||
		die "download failed: $base_url/$asset"
	curl -fsSL -o "$tmp_dir/checksums.txt" "$base_url/checksums.txt" ||
		die "download failed: $base_url/checksums.txt"

	log "Verifying checksum..."
	expected="$(awk -v f="$asset" '$2 == f || $2 == "*" f {print $1}' "$tmp_dir/checksums.txt")"
	[ -n "$expected" ] || die "no checksum entry found for $asset in checksums.txt"
	got="$(sha256_of "$tmp_dir/$asset")"
	[ "$expected" = "$got" ] || die "checksum mismatch for $asset (expected $expected, got $got)"

	log "Extracting..."
	tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
	binary_path="$(find "$tmp_dir" -type f -name 'git-envdiff' | head -n1)"
	[ -n "$binary_path" ] || die "git-envdiff binary not found inside $asset"

	mkdir -p "$INSTALL_DIR"
	dest="$INSTALL_DIR/git-envdiff"
	install -m 0755 "$binary_path" "$dest" 2>/dev/null || {
		cp "$binary_path" "$dest"
		chmod 0755 "$dest"
	}

	receipt_dir="$HOME/.git-envdiff"
	mkdir -p "$receipt_dir"
	installed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)"
	cat >"$receipt_dir/receipt.json" <<EOF
{
  "channel": "script",
  "binary_path": "$dest",
  "installed_at": "$installed_at"
}
EOF

	log ""
	log "git-envdiff ${tag} installed to $dest"

	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		log ""
		log "$INSTALL_DIR is not on your PATH. Add this to your shell profile:"
		log "  export PATH=\"$INSTALL_DIR:\$PATH\""
		;;
	esac

	log ""
	log "Next step — run this once per machine:"
	log "  git-envdiff install"
}

main "$@"
