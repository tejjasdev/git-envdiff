# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
intends to follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

- pre-commit no longer stages unrelated unstaged edits to `.env.example` when folding in an accepted fix (it warns and leaves the file unstaged), and now stages the *target* of a symlinked `.env.example` inside the repo (a link outside the repo is appended but not staged).
- An unclosed quote in `.env` (e.g. `PASSWORD="abc`) no longer makes the scanner treat the rest of the file as a multi-line value and silently skip later variables; the opener is recovered as an ordinary single-line value.
- Quote tracking in the `.env` scanner is now escape-aware for double quotes: an escaped quote (`\"`) inside a value no longer makes the parser treat the rest of the file as a multi-line value and silently skip later variables.
- Manual `git-envdiff` now exits 1 with a stderr error naming the file when
  `.env` / `.env.example` cannot be read (e.g. a line over the 10 MB scanner
  limit) instead of wrongly reporting "in sync". Hook mode is unchanged:
  still warns and exits 0.
- A hook running without a terminal (GUI Git clients, scripts) now snoozes
  only itself (`push/pre-commit`, `pull/post-checkout`, …) instead of the
  whole direction, so a later `git push` / `git pull` from a real terminal
  still prompts. An explicit "No" still snoozes the whole direction, and
  old snooze files keep working unchanged.

## [0.1.0] - 2026-09-30

### Added

- Initial release: pre-commit / pre-push / post-merge / post-checkout hooks
  detecting drift between `.env` and `.env.example` in both directions.
- Global, one-time install via Git 2.54+ config-based hooks, with a
  per-repository fallback for older Git.
- `install`, `uninstall`, `status`, `on`/`off`, `update`, and the default
  manual check.
- Self-update for `install.sh` / `install.ps1` installs; upgrade-command
  guidance for every other install channel.
