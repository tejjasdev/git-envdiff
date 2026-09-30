# Contributing to git-envdiff

Thanks for considering it. This project stays small on purpose — please
read "Scope" below before proposing a feature, since it explains what will
and won't be accepted and why.

## Setup

Requirements: Go 1.22+ and Git.

```sh
git clone https://github.com/tejjasdev/git-envdiff.git
cd git-envdiff
go build -o git-envdiff .
go test ./...
```

CI runs the tests on the latest Go (Linux, macOS, Windows) and on Go 1.22 as the
minimum supported version.

There are no other dependencies — the entire tool is Go standard library
only (see "Scope" below for why that's a hard rule, not a preference).

## Running the tests

```sh
go test ./...            # everything
go test ./... -race      # with the race detector (what CI runs)
go vet ./...
gofmt -l .                # should print nothing
```

Most of the interesting tests spin up a real temporary Git repository and
run real `git` commands against it (see `internal/hooks/hooks_test.go` and
`internal/install/install_test.go`) rather than mocking Git. If you're
changing hook or install behavior, a test in that style is the way to prove
it — see [docs/HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md) for the behavior
those tests are pinning down.

`internal/install`'s Git-2.54-only tests (`t.Skip` on older Git) will skip
automatically if your local Git predates config-based hooks; that's
expected, not a failure.

## Project layout

```
main.go                 CLI entry point and command dispatch
internal/envfile/       reads .env key NAMES only; appends KEY= lines
internal/hooks/         the four hook events + the manual check
internal/install/       global config hooks / legacy per-repo fallback
internal/gitutil/       thin wrapper around the `git` commands we need
internal/state/         per-repo snooze + local-only-key memory (under .git/)
internal/prompt/        the one interactive y/N/l confirmation
internal/update/        the ONLY package allowed to touch the network
internal/buildinfo/     version string, set via -ldflags at release time
```

## Scope

git-envdiff's whole premise is that it stays tiny. Before opening a feature
PR, check it against this:

- **Go standard library only.** No third-party dependencies, ever. This
  keeps builds reproducible, keeps the supply-chain surface at zero, and
  keeps the codebase small enough that "read the whole thing" is a
  realistic bar for a contributor or a security reviewer.
- **No network access outside `internal/update`.** CI enforces this with a
  grep-based check; a PR that adds an import of `net` or `net/http`
  anywhere else will fail CI on that basis alone, regardless of the reason.
- **No values from `.env`, ever, leave `internal/envfile`'s read path.**
  If a change needs to inspect a `.env` value for any reason, that's very
  likely a sign the change doesn't belong in this project.
- **Not a schema/validation/secrets/cloud/telemetry/AI feature.** These are
  intentionally out of scope — see the README's "What it deliberately does
  not do" and [docs/COMPARISON.md](docs/COMPARISON.md) for tools that do
  solve those problems well. A PR adding one of these will likely be
  declined regardless of implementation quality, not because the idea is
  bad, but because it isn't this project's job.

Bug fixes, portability fixes (especially anything verified on real Windows
— see the README's "not yet verified" list), test coverage, and
documentation improvements are all very welcome without needing to clear
the bar above.

## Commit / PR expectations

- Keep PRs focused — one behavior change per PR is easier to review and to
  revert if something's wrong.
- Add or update a test for any behavior change. See "Running the tests"
  above for the style this project uses.
- `gofmt` your changes (`gofmt -w .`).
- Describe *why*, not just *what*, in the PR description — especially for
  anything touching `internal/hooks` or `internal/install`, where the
  "why" is usually a specific Git behavior (see
  [docs/HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md) for the reasoning already on
  record, so a PR can build on it rather than re-litigate it).

## Reporting a security issue

Please see [SECURITY.md](SECURITY.md) rather than opening a public issue.
