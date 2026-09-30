# Keeping `.env.example` in sync with `.env`

A practical guide to a small problem that bites almost every team: you add
an environment variable to your local `.env`, forget the shared
`.env.example`, and a teammate's app breaks after their next `git pull`.
This page covers what `.env.example` is for, how to keep it accurate by
hand, and how to automate it — with or without git-envdiff.

## Should `.env.example` be committed to Git?

Yes. `.env` holds your real, machine-specific values and must stay out of
version control (add it to `.gitignore`). `.env.example` is the opposite: a
committed template listing every variable the project expects, so a new
contributor can copy it to `.env` and fill in their own values.

## What belongs in `.env.example`?

- **Every variable name** the app reads, including optional ones.
- **Safe defaults or blank values** — `PORT=3000`, `DEBUG=false`, `API_KEY=`.
- **Comments** explaining anything non-obvious, or where to get a key.
- **Never real secrets.** Anything in this file is public to everyone who
  can read the repository, and it stays in Git history forever.

## Why does `.env.example` drift out of date?

Because nothing enforces it. Adding a variable to `.env` makes your own app
work immediately, so there is no failing test or error to remind you to
update the template. The cost lands on someone else, later: a teammate pulls
your change, and the app crashes with a missing-variable error.

## The manual way

Cheap and worth doing even if you automate later:

- Add a line to your pull request template: *"Did you add a new environment
  variable? Is it in `.env.example`?"*
- Check for drift on demand with a one-liner (bash). This prints names that
  are in `.env` but missing from `.env.example`:

  ```bash
  names() { sed -n 's/^[[:space:]]*\(export[[:space:]]\{1,\}\)\{0,1\}\([A-Za-z_][A-Za-z0-9_]*\)=.*/\2/p' "$1" | sort -u; }
  comm -23 <(names .env) <(names .env.example)     # in .env, missing from .env.example
  comm -13 <(names .env) <(names .env.example)     # in .env.example, missing from .env
  ```

  It only compares variable names, so it never prints a secret. It does not
  understand multi-line quoted values, which is one reason a purpose-built
  tool is safer than a script.

## The automatic way

The most reliable fix is to make Git do the reminding, at the moments the
problem actually happens:

- **When you commit or push** — notice a variable in `.env` that isn't in
  `.env.example`, and offer to add it (name only).
- **When you pull or switch branches** — notice a variable in
  `.env.example` that isn't in your `.env`, and offer to add it.

Tools that do part or all of this include
[git-envdiff](../README.md) (both directions, one install for every repo,
no runtime),
[dot_example](https://github.com/smcabrera/dot_example) (Ruby),
[clean-dotenv](https://pypi.org/project/clean-dotenv/) (Python,
pre-commit framework), and [dotenvx](https://dotenvx.com)'s
`ext genexample`. If you'd rather replace the `.env.example` convention with
a schema entirely, look at [varlock](https://varlock.dev). See
[COMPARISON.md](COMPARISON.md) for an honest side-by-side.

With git-envdiff:

```sh
curl -fsSL https://github.com/tejjasdev/git-envdiff/releases/latest/download/install.sh | sh
git-envdiff install      # once per machine
```

## Common questions

### Why does my app crash after `git pull` with a missing environment variable?

Someone added a variable to the code (and to their own `.env`) but not to
`.env.example`, so you never learned you needed it. Compare the two files
(see above) or check the pull request that changed the code for new
`process.env.*` / `os.getenv(...)` / `os.Getenv(...)` usages.

### How do I find out that a teammate added a new environment variable?

Watch for changes to `.env.example` in code review, and — better — make
adding to it automatic so the change actually appears in the diff. A hook
that adds the name at commit time means the new variable shows up in the same
pull request as the code that needs it.

### Can I check `.env` against `.env.example` in CI?

CI usually doesn't have your real `.env`, so there is nothing to compare —
this is why drift is best caught locally, at commit and pull time. What CI
*can* do is fail fast on missing variables at application start-up, which is
a different (and complementary) safeguard.

### Is it safe to auto-generate `.env.example` from `.env`?

Only if the tool copies variable **names** and never values. A generator
that copies values, even by accident, publishes your secrets. git-envdiff is
built so this is structurally impossible: the only function that writes to
`.env.example` takes names and writes `KEY=` — it has no way to receive a
value.
