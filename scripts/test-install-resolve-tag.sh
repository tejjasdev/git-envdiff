#!/bin/sh
# Regression tests for install.sh resolve_tag() — issue #5 (GE-01).
#
# No network access: a fake `curl` is placed first in PATH. It logs every
# call to $FAKE_CURL_LOG, answers the `-w '%{url_effective}'` probe with
# $FAKE_LATEST_URL, and fails any real download (still logged, so tests can
# assert that no download was attempted).
#
# Run from anywhere:  sh scripts/test-install-resolve-tag.sh
# Needs an executable TMPDIR (some containers mount /tmp noexec):
#   TMPDIR=/path/with/exec sh scripts/test-install-resolve-tag.sh
set -u

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
install_sh="$repo_root/install.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/ge01.XXXXXX")
trap 'rm -rf "$work"' EXIT

failures=0
pass() { printf 'PASS  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; failures=$((failures + 1)); }
show() { sed 's/^/    /' "$1"; }

# run_case <dir> <fake-latest-url> [VAR=val ...]
run_case() {
	d=$1; fake_url=$2; shift 2
	mkdir -p "$d/bin" "$d/home"
	cat >"$d/bin/curl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$FAKE_CURL_LOG"
for arg in "$@"; do
	if [ "$arg" = '%{url_effective}' ]; then
		printf '%s\n' "$FAKE_LATEST_URL"
		exit 0
	fi
done
exit 1
EOF
	chmod +x "$d/bin/curl"
	(
		env PATH="$d/bin:$PATH" FAKE_CURL_LOG="$d/curl.log" FAKE_LATEST_URL="$fake_url" \
			INSTALL_DIR="$d/inst" HOME="$d/home" "$@" \
			sh "$install_sh" >"$d/out" 2>"$d/err"
		echo $? >"$d/code"
	)
}

# A: no published release — /releases/latest resolves to /releases.
run_case "$work/a" "https://github.com/tejjasdev/git-envdiff/releases"
if [ "$(cat "$work/a/code")" -ne 0 ] \
	&& grep -q "no published release found (resolved tag 'releases')" "$work/a/err" \
	&& ! grep -q 'releases/download' "$work/a/curl.log"; then
	pass "A: 'releases' rejected with clear error, no download attempted (exit $(cat "$work/a/code"))"
else
	fail "A: expected clear rejection of 'releases' + no download (exit $(cat "$work/a/code"))"
	show "$work/a/out"; show "$work/a/err"; show "$work/a/curl.log"
fi

# B: valid release — redirect lands on /releases/tag/v0.1.0.
run_case "$work/b" "https://github.com/tejjasdev/git-envdiff/releases/tag/v0.1.0"
if grep -q "Downloading git-envdiff v0.1.0 for" "$work/b/out" \
	&& grep -q 'releases/download/v0.1.0/' "$work/b/curl.log"; then
	pass "B: valid tag v0.1.0 resolved and used for the download URL"
else
	fail "B: expected v0.1.0 to resolve and be used (exit $(cat "$work/b/code"))"
	show "$work/b/out"; show "$work/b/err"; show "$work/b/curl.log"
fi

# C: explicit GIT_ENVDIFF_VERSION — no latest lookup at all.
run_case "$work/c" "https://github.com/tejjasdev/git-envdiff/releases" GIT_ENVDIFF_VERSION=v0.9.9
if ! grep -q 'url_effective' "$work/c/curl.log" \
	&& grep -q "Downloading git-envdiff v0.9.9 for" "$work/c/out" \
	&& grep -q 'releases/download/v0.9.9/' "$work/c/curl.log"; then
	pass "C: explicit GIT_ENVDIFF_VERSION=v0.9.9 used as-is, no latest-release lookup"
else
	fail "C: expected v0.9.9 without a latest lookup (exit $(cat "$work/c/code"))"
	show "$work/c/out"; show "$work/c/err"; show "$work/c/curl.log"
fi

# D: prerelease tag must be accepted.
run_case "$work/d" "https://github.com/tejjasdev/git-envdiff/releases/tag/v0.1.0-rc1"
if grep -q "Downloading git-envdiff v0.1.0-rc1 for" "$work/d/out"; then
	pass "D: prerelease tag v0.1.0-rc1 accepted"
else
	fail "D: expected v0.1.0-rc1 to be accepted (exit $(cat "$work/d/code"))"
	show "$work/d/out"; show "$work/d/err"; show "$work/d/curl.log"
fi

# E: non-v tag (release workflow only creates v*) must be rejected.
run_case "$work/e" "https://github.com/tejjasdev/git-envdiff/releases/tag/1.2.3"
if [ "$(cat "$work/e/code")" -ne 0 ] \
	&& grep -q "resolved tag '1.2.3'" "$work/e/err" \
	&& ! grep -q 'releases/download' "$work/e/curl.log"; then
	pass "E: non-v tag 1.2.3 rejected, no download attempted"
else
	fail "E: expected 1.2.3 to be rejected with no download (exit $(cat "$work/e/code"))"
	show "$work/e/out"; show "$work/e/err"; show "$work/e/curl.log"
fi

if [ "$failures" -eq 0 ]; then
	printf 'ALL %s SCENARIOS PASS\n' "5"
	exit 0
fi
printf '%d of 5 scenarios FAILED\n' "$failures"
exit 1
