# git-envdiff

**Keep `.env` and `.env.example` in sync, automatically, through Git.**

[![CI](https://github.com/tejjasdev/git-envdiff/actions/workflows/ci.yml/badge.svg)](https://github.com/tejjasdev/git-envdiff/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Latest release](https://img.shields.io/github/v/release/tejjasdev/git-envdiff)](https://github.com/tejjasdev/git-envdiff/releases/latest)

> **Status:** early (v0.1). The core is small and tested, but it hasn't seen
> heavy real-world use yet. Issues and feedback are very welcome.

## At a glance

- **What it is:** a small command-line tool (single Go binary, no runtime, standard library only) that installs Git hooks to keep `.env.example` and `.env` from drifting apart.
- **What it does:** before you commit or push, offers to add new variable *names* to `.env.example`; after you pull or switch branches, offers to add new variables to your `.env`.
- **What it never does:** read or copy `.env` values, use the network (except an explicit `git-envdiff update`), collect telemetry, or overwrite your existing Git hooks.
- **Works with:** any language or framework (it only looks at two files), macOS, Linux, and Windows (Windows is newer and less battle-tested).
- **Install:** `curl -fsSL https://github.com/tejjasdev/git-envdiff/releases/latest/download/install.sh | sh`, then `git-envdiff install`.
- **License:** MIT.

## The problem

You add a new environment variable to your local `.env` file. It works, you
commit, you push. Three days later a teammate pulls your change, runs the
app, and it crashes — because `.env.example` never got the new variable, and
their `.env` doesn't have it either. Nobody notices until it breaks
something.

`.env.example` drifts out of sync with `.env` constantly, because updating it
by hand is a step everyone forgets under normal working conditions. It's not
a hard problem — it just needs something watching for it.

## What git-envdiff does

Install it once. From then on, it works silently in every Git repository on
your machine:

- **Before you commit or push** — if `.env` has a variable that
  `.env.example` doesn't, it asks whether to add it (name only, never the
  value) and folds the fix into your commit.
- **After you pull or switch branches** — if `.env.example` now has a
  variable your `.env` is missing, it asks whether to add it (with the
  template's own — already public — default value).
- **When there's nothing to say, it says nothing.** No output, no delay,
  no daily nag.

```
$ git commit -m "add Stripe integration"
git-envdiff: .env defines STRIPE_KEY, not in .env.example.
Add to .env.example (name only, no value)? [y/N/l]: y
git-envdiff: added STRIPE_KEY to .env.example and included it in this commit.
```

```
$ git pull
git-envdiff: .env.example has variable(s) missing from your .env: REDIS_URL
Add to .env (values copied from .env.example, usually blank)? [y/N]: y
git-envdiff: added REDIS_URL to .env. Fill in real values for any that are blank.
```

It works the same way in a Python repo, a Node repo, a Go repo, or anything
else — it only looks at `.env` and `.env.example`, never at your language or
framework.

## What it deliberately does not do

git-envdiff does one job. On purpose, it is **not**:

- a secrets manager, vault, or encryption tool
- a cloud service, dashboard, or account of any kind
- a `.env` loader or a replacement for `dotenv`
- a schema/validation framework
- a CI platform or IDE plugin
- collecting telemetry of any kind

If you're looking for a richer environment-configuration system with schema
validation and type checking, tools like [dotenvx](https://dotenvx.com) or
[varlock](https://varlock.dev) solve a bigger problem than this one. See
[docs/COMPARISON.md](docs/COMPARISON.md) for how git-envdiff relates to those
and to other tools in this space.

## Install

**macOS / Linux:**

```sh
curl -fsSL https://github.com/tejjasdev/git-envdiff/releases/latest/download/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://github.com/tejjasdev/git-envdiff/releases/latest/download/install.ps1 | iex
```

Prefer to read the script first, or install by hand? See
[install.sh](install.sh) / [install.ps1](install.ps1), or grab a binary
directly from the [Releases page](https://github.com/tejjasdev/git-envdiff/releases/latest)
and verify it against that release's `checksums.txt`.

**Go toolchain:**

```sh
go install github.com/tejjasdev/git-envdiff@latest
```

Windows is newer and less battle-tested than macOS/Linux: the installer and
prompts have been exercised under PowerShell 7 and cross-compiled, but not
yet used on a large number of real Windows machines. Reports are welcome.

Then, once per machine:

```sh
git-envdiff install
```

On Git 2.54+ this registers git-envdiff globally — every repository on your
machine, from that point on, without touching that repository again. On
older Git it installs into the current repository only (`git-envdiff
install` again in each one, or upgrade Git). Either way, your existing hooks
— Husky included — are never overwritten.

## Commands

| Command | What it does |
|---|---|
| `git-envdiff` | Check both directions right now and offer to fix |
| `git-envdiff install [--repo]` | Install hooks (global by default on Git ≥ 2.54; `--repo` forces this repository only) |
| `git-envdiff uninstall [--repo]` | Remove exactly what `install` added |
| `git-envdiff status` | Show what's currently installed |
| `git-envdiff on` / `off` | Enable / disable git-envdiff in the current repository |
| `git-envdiff update` | Update git-envdiff itself |
| `git-envdiff --version` | Print the version |

Temporary bypass for one command: `GIT_ENVDIFF_SKIP=1 git commit ...` (this
also works with Git's own `--no-verify`, which skips every hook).

## How it works

Git has no single "just ran a pull" hook, so git-envdiff listens on four:
`pre-commit` and `pre-push` (checking `.env` against `.env.example`), and
`post-merge` and `post-checkout` (checking the other direction, after a pull
or branch switch). Each one is a no-op in under a few milliseconds when
there's nothing to report.

The full reasoning — why these four hooks and not others, how the two
install strategies work, and what was verified against real Git rather than
assumed — is in **[docs/HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md)**.

## Privacy and security

This is the part that matters most for a tool that reads your `.env` file
and runs automatically during Git commands:

- **git-envdiff reads variable *names* only.** It never reads, logs,
  prints, or stores a value from your `.env` file. This is enforced by the
  file-handling code itself (see [`internal/envfile`](internal/envfile)) —
  there is no code path that copies a `.env` value anywhere.
- **It only ever appends a bare `KEY=` line to `.env.example`.** Never a
  value, never a reorder, never a rewrite of existing lines.
- **No network access, ever, except `git-envdiff update`** — and even that
  only runs when you explicitly type it. No telemetry, no phone-home, no
  "checking for updates" on startup.
- **No account, no cloud, no dashboard.** Nothing about your project leaves
  your machine.
- **If the git-envdiff binary is ever missing** (uninstalled, or a PATH a
  GUI Git client doesn't share with your shell), every hook silently does
  nothing rather than breaking your commits — verified in
  [`internal/install`](internal/install)'s test suite.
- **A crash inside git-envdiff never fails your Git command.** Every hook
  exits `0` except in one specific case: accepting a fix at `pre-push` time
  intentionally stops the push, because the fix can't retroactively join
  commits that are already being pushed — see
  [docs/HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md) for why.

If you find a case where any of this isn't true, please open an issue —
this list is a set of guarantees, not just intentions.

## How this compares to other tools

| | Two-way drift check | Automatic via Git hooks | One install for every repo | Runtime needed |
|---|---|---|---|---|
| **git-envdiff** | ✅ | ✅ | ✅ (Git ≥ 2.54) | none |
| [dot_example](https://github.com/smcabrera/dot_example) | ✅ | ✅ (per project) | ❌ | Ruby |
| [clean-dotenv](https://pypi.org/project/clean-dotenv/) | one way (regenerates `.env.example`) | via pre-commit framework | ❌ | Python |
| [dotenvx](https://dotenvx.com) | one way (`ext genexample`, on demand) | opt-in precommit guard only | n/a | none / Node |
| [varlock](https://varlock.dev) | replaces the pair with a schema | leak scanning | n/a | none / Node 22+ |
| [dotenv-linter](https://github.com/dotenv-linter/dotenv-linter) | compare on demand | ❌ | n/a | none |

git-envdiff isn't trying to out-feature these tools — several of them do
things it deliberately never will. The gap it fills is narrow: keep the
plain `.env` / `.env.example` convention, get automatic checks in both
directions through Git itself, and install it once. Details, and where each
of the others is the better choice, are in
[docs/COMPARISON.md](docs/COMPARISON.md) (checked against each project's own
documentation in September 2026).

New to the `.env` / `.env.example` pattern, or want the manual approach?
See the [guide](docs/GUIDE.md).

## FAQ

**Will this ever write a secret value into a file I commit?**
No — see [Privacy and security](#privacy-and-security) above. `AppendBlankKeys`,
the only function that writes to `.env.example`, has no parameter for a
value at all.

**I already use Husky / lefthook / another hook manager. Will this break it?**
No. git-envdiff detects `core.hooksPath` and prints the one line to add to
your existing hook files instead of writing its own — it never overwrites a
hook manager's setup. See [docs/HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md).

**Does this work with [my language/framework]?**
Yes — it only reads `.env` and `.env.example`; it has no idea what language
your project is in, on purpose.

**What if I don't want to be asked about a particular variable?**
Answer `l` (local-only) at the prompt, or run `GIT_ENVDIFF_SKIP=1` for a
single command. Both are documented above.

**Does this replace `dotenv`, `dotenvx`, or a `.env` loader?**
No — git-envdiff never loads environment variables into a process. It only
compares two files. Keep using whatever loader your project already uses.

## Uninstalling

```sh
git-envdiff uninstall     # removes whatever `install` added
rm ~/.local/bin/git-envdiff   # or wherever you installed the binary
rm -rf ~/.git-envdiff
```

## Contributing

Bug reports, ideas, and pull requests are welcome — see
[CONTRIBUTING.md](CONTRIBUTING.md). The whole codebase is Go standard
library only, organized as small, independently-testable packages under
[`internal/`](internal); `go test ./...` is the whole test suite.

## License

[MIT](LICENSE)
