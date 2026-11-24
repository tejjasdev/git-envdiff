# How git-envdiff works

This document is the "why" behind git-envdiff's design. The short version
lives in the [README](../README.md); this is the detail.

## Why four hooks, and not a "post-pull" hook

Git has no single hook that fires "after a pull". `git pull` is fetch plus
either a merge or a rebase, and each of those triggers different hooks
depending on exactly what happens. Assuming a "post-pull" hook exists is a
common mistake — instead, git-envdiff listens on the two hooks that
together cover every case, verified against real Git rather than assumed:

| Operation | Hook that fires |
|---|---|
| `git pull` (fast-forward) | `post-merge` |
| `git pull` (true merge) | `post-merge` |
| `git pull --rebase`, no local commits | `post-merge` |
| `git pull --rebase`, with local commits | `post-checkout` (flag `1`), then `post-rewrite` |
| Branch switch (`git checkout`/`switch`) | `post-checkout` (flag `1`) |
| Single-file checkout (`git checkout -- path`) | `post-checkout` (flag `0`) — **ignored** |

git-envdiff hooks `post-merge` and `post-checkout` (acting only when the
third argument is `1`, meaning a real `HEAD` move, not a file checkout).
That pair covers every path above without needing `post-rewrite` at all.

For the other direction — a new variable in `.env` that hasn't reached
`.env.example` yet — git-envdiff hooks `pre-commit` (the primary check) and
`pre-push` (a backstop, described below).

## Why `pre-commit` fixes are staged, but `pre-push` fixes block the push

When `pre-commit` accepts a fix, it appends the missing key to
`.env.example` and runs `git add` on it — Git hasn't built the commit yet,
so the new line becomes part of the commit that's about to be made. This is
the common case and it's how most differences should get fixed.

`pre-push` exists only as a backstop for a commit made with `--no-verify`,
an older install, or a repository where the hook was never set up. By the
time `pre-push` runs, Git has already built the commits that are about to
be pushed. Editing the working tree at that point cannot retroactively
change what's inside those commits — so if `pre-push` wrote the fix and let
the push continue, a teammate pulling that push would still get an
out-of-date `.env.example`, silently. Rather than create that illusion of
safety, an accepted fix at `pre-push` time **stops the push** (exit code
`1`) and asks the user to commit the fix and push again. Declining never
blocks anything — see the [state machine](#state-per-repository) below for
why it doesn't nag on every subsequent push either.

This was tested directly: writing to `.env.example` inside a `pre-push`
hook and letting the push proceed does not include that change in what
reaches the remote, confirming the file-versus-commit distinction above
rather than leaving it as a theoretical concern.

## Install strategy

### Git ≥ 2.54: global config-based hooks

Git 2.54 added hooks that can be defined in config rather than as files —
including in `~/.gitconfig`. `git-envdiff install` writes:

```gitconfig
[hook "git-envdiff-pre-commit"]
    event = pre-commit
    command = command -v git-envdiff >/dev/null 2>&1 || exit 0; git-envdiff hook pre-commit
```

(and the same for `pre-push`, `post-merge`, `post-checkout`). This is what
makes a single, one-time install work in every repository on the machine.
It was verified to:

- run in repositories that have no hooks of their own,
- run *alongside* a repository's own `.git/hooks/*` scripts (both fire —
  neither is skipped),
- run alongside a repository-local `core.hooksPath` (e.g. Husky) — both
  fire there too,
- correctly receive the hook's normal arguments and, for `pre-push`, its
  stdin.

### The guard, and why it exists

Look closely at the command above: `command -v git-envdiff >/dev/null 2>&1
|| exit 0; git-envdiff hook pre-commit`. That first clause matters more
than it looks.

Without it, if the git-envdiff binary is ever removed — a package manager
uninstall, or simply a PATH a GUI Git client doesn't share with the shell —
**every `git commit` and `git push`, in every repository on that machine,
starts failing**, because Git tries to run a command that no longer exists.
This was reproduced directly before the guard was added, and it's the
reason the guard is unconditional in both install strategies: with it, a
missing binary makes the hook silently do nothing (exit `0`) instead.

### Git < 2.54: per-repository fallback

`git-envdiff install --repo` (or a plain `install` on older Git) appends a
clearly marked block into the current repository's own
`.git/hooks/<event>` files, immediately after the shebang line:

```sh
#!/bin/sh
# >>> git-envdiff (managed) >>>
if command -v git-envdiff >/dev/null 2>&1; then
  git-envdiff hook pre-commit "$@" || exit $?
fi
# <<< git-envdiff <<<
... (anything that was already in this file stays right after this)
```

Placing the block immediately after the shebang, rather than at the end of
the file, matters: if an existing hook calls `exit` partway through (a
common pattern), a block appended at the end would simply never run.
`uninstall --repo` removes exactly this block; if nothing but the shebang
is left afterward (meaning git-envdiff created the file from nothing), the
file is deleted entirely.

If `core.hooksPath` is already set — Husky and similar tools do this — the
fallback does not guess at that directory's structure. It prints the one
line to add by hand instead, rather than writing into a hook layout it
doesn't understand.

The block is inserted only into hooks whose first line names a POSIX shell
— `sh`, `bash`, `dash`, `ash`, `zsh` or `ksh`, directly or through `env`
(including `env -S` and `VAR=value` forms). A hook in any other language
(fish, PowerShell, csh, Python, Node, …) or with no shebang is left
untouched; `install` prints the line to add by hand, still installs the
other hooks, and exits `1`.

### Why not `core.hooksPath` or `init.templateDir` globally

A global `core.hooksPath` silently stops Git from reading a repository's
own `.git/hooks` at all, and a repository-local Husky-style
`core.hooksPath` setting overrides it — so a global `core.hooksPath` would
quietly disable itself in exactly the repositories most likely to already
have hooks. `init.templateDir` only affects repositories created *after*
it's set, which fails the "works in every repository, immediately" goal.
Neither is used.

## File rules

- **Reading `.env`:** only variable *names* are ever read. A value is never
  captured, logged, or retained — see
  [`internal/envfile`](../internal/envfile)'s doc comments for exactly
  where that guarantee is enforced in code.
- **Writing `.env.example`:** only a bare `KEY=` line is ever appended.
  There is no function in the codebase capable of writing a value there.
- **Writing `.env`:** values copied in come only from `.env.example`
  itself — a file already committed to version control — never from
  anywhere else. A value is copied exactly as written in `.env.example`,
  including quotes and any trailing comment; an `export` keyword is not
  copied. A multi-line quoted value in the template is copied as a blank
  value (`NAME=`).
- **Every write is append-only and atomic**: a temp file in the same
  directory, then a rename. A crash mid-write can't leave a half-written
  file behind.
- **Symlinks:** if `.env` or `.env.example` is a symlink, the append goes to
  the file it points to and the link stays a link. The temporary file is
  created next to the target, so that directory must be writable.

## What counts as a variable

- **Recognized:** optional leading whitespace, optional `export`, a name of
  letters, digits and underscores that does not start with a digit, and
  optional spaces around `=`. Names are case-sensitive.
- **Ignored:** comment lines, names with dots or hyphens, `NAME: value`
  lines, and names starting with a digit.
- A name that appears more than once counts once (the first occurrence).
  A leading BOM and CRLF line endings are handled.
- A single line longer than 10 MiB cannot be read. Hooks print a warning
  and exit `0`; plain `git-envdiff` reports an error naming the file and
  exits `1` — it never says "in sync" when it could not read a file.
- A value that opens a quote and does not close it on the same line is
  multi-line: lines inside it are skipped until the closing quote, so PEM
  keys or base64 blocks are not mistaken for variables. Inside double
  quotes a backslash escapes the next character (an escaped quote does not
  close the value); single quotes have no escapes.
- A quote that is never closed before the end of the file is treated as an
  ordinary single-line value, so variables after it are still found.
  Accepted limit: an even number of stray quotes pair up and the text
  between them is skipped.

## When it prompts, and when it only prints

A prompt is shown only when stderr is a real terminal *and* a controlling
terminal can be opened. That second check on stderr is deliberate: an
editor started from a shell inherits a terminal it isn't showing anyone, and
prompting there would hang the commit on a question nobody can see. In that
case (and in CI, and in most GUI Git clients) git-envdiff prints a single
warning line instead and never blocks.

## Commit flavours

Git tells the pre-commit hook which index it is building through
`GIT_INDEX_FILE`. Verified against real Git: a plain `git commit` uses
`.git/index` and `git commit -a` uses `.git/index.lock` — an accepted fix is
staged into the commit in both. `git commit <path>` uses a temporary
`next-index-<pid>.lock`; there git-envdiff still writes the fix but only
warns, because staging into a temporary index would leave the real index and
the commit disagreeing.

Before appending, git-envdiff checks whether `.env.example` already has
unstaged changes. If it does, the name is appended but **not staged**, and a
warning says to review the file and run `git add`; otherwise the fix is
staged as before. This one extra `git` call happens only after you answer
`y`. `git commit -a` stages tracked changes anyway, so there the whole file
is committed.

If `.env.example` is a symlink to a file inside the repository, that target
is what gets staged and the link is unchanged; if it points outside the
repository, nothing is staged and a warning says so.

## State per repository

Three small files live under `.git/` (never in a tracked file, so they're
local to your clone): `.git/git-envdiff-snooze`, `.git/git-envdiff-local`,
and `.git/git-envdiff-disabled` (created by `git-envdiff off`, removed by
`on`):

- **A snooze list** — a fingerprint of "this exact set of missing keys, in
  this direction" for any diff you've declined, so the same unchanged diff
  doesn't ask again on every subsequent commit or pull. It stops applying
  automatically as soon as the set of missing keys changes — and it is
  forgotten entirely once that difference disappears, so if the same
  difference comes back later you are asked again. There are two kinds:
  an explicit "No" at a prompt snoozes the whole direction (pre-commit and
  pre-push share `push`, post-merge and post-checkout share `pull`), so one
  declined diff is not asked about again by either hook of that direction.
  A hook that ran without a terminal (GUI Git clients, scripts) snoozes
  only itself (`push/pre-commit`, `pull/post-checkout`, …), so a different
  hook of the same direction run later from a real terminal — e.g. `git push`
  after a commit made in a GUI — still asks.
- **A local-only list** — variable names you've explicitly marked (the `l`
  answer) as never worth proposing for `.env.example`. `.env` legitimately
  holds machine-specific values that don't belong in a shared template,
  and without this, every commit would keep nagging about them. Delete a
  line (or the whole file) to be asked about that variable again;
  `git-envdiff status` lists them. An `l` answer covers every variable
  named in that prompt.

## Exit codes

Hooks exit `0` even on an internal error or panic, except `pre-push` after
you accept a fix (it stops the push). Plain `git-envdiff` exits `1` when a
file cannot be read. `install` exits `1` if any hook could not be installed.

## What's been verified, and what hasn't

Verified against real Git (2.43 and 2.54) and the compiled binary:

- the hook matrix above, the two install strategies, and the missing-binary guard;
- accepted fixes landing in plain, `-a`, and pathspec commits as described, driven through a real pseudo-terminal (not a mock), including the `y` / `N` / `l` answers and the `pre-push` block;
- a no-difference hook run spawning no `git` subprocess at all (there is a test for this) and taking roughly 2 ms end to end, most of which is process start-up;
- `install.sh` running under `dash` (what `| sh` means on Debian/Ubuntu), including rejecting a tampered download;
- `install.ps1` executing under PowerShell 7, including resolving the latest tag from GitHub's redirect and rejecting a tampered download;
- `install.sh` and `install.ps1` refusing to continue when no release is published (a clear error, nothing downloaded);
- a CI job building Git 2.54.0 from source and running the config-hook
  install tests, which skip on older Git elsewhere.

Not yet verified on real hardware:

- Git for Windows and a real Windows machine (everything cross-compiles; the installer ran under PowerShell 7 on Linux, not on Windows itself);
- behaviour inside specific GUI Git clients beyond the general "no terminal" fallback;
- `.env` layouts other than a single root-level `.env` / `.env.example` pair (monorepos, `.env.local`, …) — out of scope for v0.1 by design.

If you hit something on this list, or something not on it, please open an
issue.
