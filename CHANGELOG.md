# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
intends to follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

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
