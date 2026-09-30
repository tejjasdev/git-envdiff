# How git-envdiff compares to other tools

A fair comparison, not a sales pitch. Where another tool does something
git-envdiff deliberately doesn't, this page says so plainly.

Everything below was checked against each project's own README or
documentation in **September 2026**. Projects change; if something here is
out of date or wrong, please [open an issue](https://github.com/tejjasdev/git-envdiff/issues) —
getting this page right matters more than flattering git-envdiff.

## The short version

Several good tools touch the `.env` / `.env.example` problem. They fall into
three groups, and git-envdiff sits in the first:

1. **Keep the pair in sync using Git hooks** — git-envdiff, [dot_example](https://github.com/smcabrera/dot_example), [clean-dotenv](https://pypi.org/project/clean-dotenv/).
2. **Generate or lint the files on demand** — [dotenvx](https://dotenvx.com) (`ext genexample`), [dotenv-linter](https://github.com/dotenv-linter/dotenv-linter).
3. **Replace the pair with something bigger** — [varlock](https://varlock.dev) (a schema as the single source of truth) and, in a different direction, dotenvx's encryption.

## At a glance

| Tool | What it does about `.env` ↔ `.env.example` | Automatic via Git hooks | Runtime you need |
|---|---|---|---|
| **git-envdiff** | Detects drift in *both* directions and offers the fix (names only, never values) | Yes: `pre-commit`, `pre-push`, `post-merge`, `post-checkout`; one global install on Git ≥ 2.54 | None (single static binary) |
| **dot_example** | Pre-commit hook adds `.env` keys to `.env.example`; post-checkout hook checks for new variables when you check out a branch | Yes, set up per project with `dot_example setup` | Ruby (installed as a gem) |
| **clean-dotenv** | Regenerates `.env.example` from `.env` with values stripped | Through the [pre-commit](https://pre-commit.com) framework | Python (pip) |
| **dotenvx** | `ext genexample` generates `.env.example` from `.env`; `ext precommit` guards against committing `.env` files; core feature is encryption | Only the opt-in `ext precommit --install` guard | None (standalone binary) or Node (npm) |
| **varlock** | Replaces the pair with a `.env.schema` that its docs describe as a single source of truth, with validation and types | Leak scanning via `varlock scan` and Git hooks | None (standalone binary) or Node 22+ (npm) |
| **dotenv-linter** | Lints env files and can compare them | No — on demand (you can wire it into a hook yourself) | None (Rust binary) |

## What each is best at

- **dot_example** is the closest prior art to git-envdiff's core idea and
  deserves credit for it: same two-directional thinking, same hook-based
  approach. The differences are practical — it is a Ruby gem configured per
  project (its `setup` also edits `bin/setup`), and its README describes a
  `post-checkout` hook for branch checkouts. A plain `git pull` on the
  branch you're already on fires `post-merge`, not `post-checkout`
  (see [HOW_IT_WORKS.md](HOW_IT_WORKS.md)), so covering pulls needs both.
- **clean-dotenv** is a good fit if you already use the pre-commit
  framework and just want `.env.example` regenerated from `.env`
  automatically. It regenerates the file rather than checking the other
  direction, and needs Python.
- **dotenvx** is a strong choice if you want encrypted `.env` files
  committed safely and multi-environment support. `genexample` covers
  generating `.env.example` on demand; that is a different job from a
  continuous two-way drift check.
- **varlock** is the right answer if you are willing to move to a schema:
  types, validation, and secret-loading plugins, with `.env.schema`
  replacing `.env.example`. If your team wants that, you don't need
  git-envdiff. If your team wants to keep the plain `.env.example` pattern
  that every framework and hosting guide already documents, that's the gap
  git-envdiff targets.
- **dotenv-linter** is excellent at what it does — linting and comparing
  files on demand — and, like git-envdiff, is a single binary with no
  runtime.

## Where git-envdiff fits

If the honest answer to "why install another tool for this?" is "my project
already works", that's a reasonable position. For a lot of small teams a
five-line `post-merge` script plus an occasional manual compare is enough.

git-envdiff exists for the case just past that: more than one contributor,
drift that has already bitten someone once, and a preference for something
that installs once, works in every repository regardless of language, never
overwrites the hooks you already have, and stays silent when nothing is
wrong.

It is **not** the right tool if you want schema validation, typed
variables, encrypted secrets, or a replacement for the `.env.example`
convention — see varlock and dotenvx above.
