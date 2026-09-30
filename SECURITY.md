# Security Policy

## What git-envdiff guarantees

- It reads variable **names** from `.env` — never values. See
  [`internal/envfile`](internal/envfile) for where this is enforced in code,
  and the README's [Privacy and security](README.md#privacy-and-security)
  section for the full list of guarantees.
- It makes no network requests except when you explicitly run `git-envdiff
  update`.
- It requires no account, sends no telemetry, and has no server component.

If you find behavior that contradicts any of the above, that's a security
bug — please report it privately (below) rather than as a public issue.

## What the update checksum does and doesn't protect against

`install.sh`, `install.ps1` and `git-envdiff update` verify the download
against the `checksums.txt` published in the same release. That protects
against corrupted or truncated downloads and mismatched files. It does not,
on its own, protect against a compromised release, because the checksums
come from the same place as the binary. If you need stronger guarantees,
build from source (`go install`) and review the code — it is small and has
no dependencies.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for a security vulnerability.

Instead, use GitHub's private vulnerability reporting for this repository:
**Security → Report a vulnerability** on the
[repository's Security tab](https://github.com/tejjasdev/git-envdiff/security).
If that isn't available for some reason, email **hey.tejjas@gmail.com** instead.

Please include:

- What you found and why it's a security concern (not just a bug).
- Steps to reproduce, ideally as a minimal example.
- The git-envdiff version (`git-envdiff --version`) and OS you tested on.

## What to expect

This is a small, unfunded open-source project maintained on a best-effort
basis — there's no guaranteed response time. That said, a real
confidentiality or integrity issue (a secret value leaking, a hook
executing something it shouldn't) will be treated as the highest priority
and a fix released as soon as one exists.

## Supported versions

Only the latest released version is supported. Given the small surface
area, please upgrade (`git-envdiff update`) before reporting an issue to
confirm it still reproduces on the current release.
