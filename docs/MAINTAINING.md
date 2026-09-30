# Maintaining git-envdiff

Notes for whoever maintains the repository: how to cut a release, how the
automated updates work, and the one-time GitHub settings that keep the
project (and the people installing it) safe. GitHub renames menu items from
time to time; the names below are close enough to find each setting.

## Cutting a release

1. Move the `Unreleased` notes in [CHANGELOG.md](../CHANGELOG.md) under the new
   version heading and commit.
2. Make sure CI on `main` is green.
3. Tag and push:

   ```sh
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

4. The **Release** workflow builds the archives and `checksums.txt` with
   GoReleaser and attaches them, along with `install.sh` and `install.ps1`,
   to a new GitHub Release.
5. On a clean machine or container, run the documented install command and
   `git-envdiff --version`. Only then announce the release.

Never move or delete a published tag; publish a new patch version instead.
`install.sh` and `git-envdiff update` trust whatever `latest` points at.

## Automated dependency updates

Dependabot checks the GitHub Actions used in `.github/workflows` once a
month and opens **one** grouped pull request (`ci: bump the actions group
...`). Every action is pinned to a full commit SHA with the version in a
trailing comment, so a moved or hijacked tag can't change what CI or the
release job runs.

- Read the release notes in the PR, wait for CI, then merge.
- `@dependabot rebase` refreshes a PR; `@dependabot ignore this major
  version` stops proposals for one upgrade you don't want.
- The Go module has no third-party dependencies, so there is nothing else
  to update. The `vulncheck` workflow runs weekly to catch newly published
  advisories against the Go standard library.

## One-time repository settings

**General**
- Automatically delete head branches: on (keeps Dependabot branches from piling up).
- Issues: on. Wiki: off. Discussions: off until there is traffic.
- Fill in the description and topics (see the README's launch notes).

**Code security** (all free for public repositories)
- Private vulnerability reporting: on (SECURITY.md points people to it).
- Dependency graph, Dependabot alerts, Dependabot security updates: on.
- Secret scanning and push protection: on.
- Code scanning: "Default setup" with CodeQL for Go.

**Actions → General**
- Workflow permissions: *Read repository contents* (the release job asks for
  write access explicitly in its own `permissions:` block).
- Do not allow Actions to create or approve pull requests.
- Fork pull request workflows: require approval for all outside collaborators.
- Require actions to be pinned to a full-length commit SHA: on (the
  workflows already comply).

**Rules → Rulesets**
- `main`: restrict deletions, block force pushes, require the status checks
  `test (ubuntu-latest)`, `test (macos-latest)`, `min-go (1.22)`,
  `network-boundary` and `shellcheck` (they appear in the picker after
  they have run once). Requiring pull requests is optional for a
  single-maintainer project; keep admin bypass so you can't lock yourself out.
- Tags matching `v*`: restrict deletions and block updates, so a published
  release can't be rewritten.

**Your account**
- Two-factor authentication on (a passkey or security key is best).
- Prefer an SSH key or a credential helper over long-lived tokens. If you use
  a personal access token, make it fine-grained, scoped to this repository,
  with a short expiry, and revoke it when you are done.
