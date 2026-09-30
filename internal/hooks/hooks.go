// Package hooks implements the four Git hook events git-envdiff cares
// about (pre-commit, pre-push, post-merge, post-checkout) plus the manual
// `git-envdiff` check, and the logic shared between them.
//
// See docs/HOW_IT_WORKS.md in the repository root for *why* these specific
// events were chosen and how each one behaves, verified against real Git.
//
// Performance note: every hook starts with prepare(), which decides
// whether there is anything to do using only a couple of stat() calls —
// no `git` subprocess is spawned unless the repository layout is unusual
// (a linked worktree or submodule). That is what keeps the "nothing to
// report" path down to a fraction of a millisecond of real work.
package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tejjasdev/git-envdiff/internal/envfile"
	"github.com/tejjasdev/git-envdiff/internal/gitutil"
	"github.com/tejjasdev/git-envdiff/internal/prompt"
	"github.com/tejjasdev/git-envdiff/internal/state"
)

const (
	dirPush = "push" // .env -> .env.example: a new local var not yet templated
	dirPull = "pull" // .env.example -> .env: the template has vars you're missing

	exampleFile = ".env.example"
	envFileName = ".env"

	maxListed = 10
)

// confirmFn is a package-level seam so tests can substitute a scripted
// answer instead of talking to a real terminal.
var confirmFn = prompt.Confirm

// Paths bundles the locations a single hook run needs.
type Paths struct {
	RepoRoot string
	GitDir   string
}

func (p Paths) env() string     { return filepath.Join(p.RepoRoot, envFileName) }
func (p Paths) example() string { return filepath.Join(p.RepoRoot, exampleFile) }

// locateRoot finds the working-tree root. Git runs hooks from the top of
// the working tree, so the current directory is normally the answer and
// costs nothing; only if it doesn't look like a root do we ask Git.
func locateRoot() (string, bool) {
	if cwd, err := os.Getwd(); err == nil {
		if _, err := os.Lstat(filepath.Join(cwd, ".git")); err == nil || os.Getenv("GIT_DIR") != "" {
			return cwd, true
		}
	}
	root, err := gitutil.RepoRoot()
	return root, err == nil
}

// gitDirFor finds the .git directory for root, spawning `git` only for
// layouts where .git isn't a plain directory (linked worktrees, submodules).
func gitDirFor(root string) (string, bool) {
	if d := os.Getenv("GIT_DIR"); d != "" {
		if abs, err := filepath.Abs(d); err == nil {
			return abs, true
		}
	}
	dotgit := filepath.Join(root, ".git")
	if fi, err := os.Stat(dotgit); err == nil && fi.IsDir() {
		return dotgit, true
	}
	d, err := gitutil.GitDir()
	return d, err == nil
}

// prepare is the shared fast path for every automatic hook. It returns
// ok=false — meaning "do nothing, print nothing" — unless git-envdiff is
// not skipped/disabled AND both .env and .env.example exist.
func prepare() (Paths, bool) {
	if os.Getenv("GIT_ENVDIFF_SKIP") == "1" {
		return Paths{}, false
	}
	root, ok := locateRoot()
	if !ok {
		return Paths{}, false
	}
	p := Paths{RepoRoot: root}
	if !envfile.Exists(p.example()) || !envfile.Exists(p.env()) {
		return Paths{}, false
	}
	gitDir, ok := gitDirFor(root)
	if !ok || state.IsDisabled(gitDir) {
		return Paths{}, false
	}
	p.GitDir = gitDir
	return p, true
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "git-envdiff: "+format+"\n", args...)
}

// silentFail reports an internal error without ever failing the Git
// operation that triggered it: git-envdiff must never be the reason a
// commit, push, or pull fails because of its own bug.
func silentFail(err error) { warnf("internal error (ignored): %v", err) }

// ---- pre-commit ---------------------------------------------------------

// RunPreCommit checks whether .env defines variables .env.example does
// not. If the current commit is a normal (non-partial) commit, an
// accepted fix is staged directly into it. Never blocks a commit — this
// hook's exit code is always 0.
func RunPreCommit() int {
	p, ok := prepare()
	if !ok {
		return 0
	}
	missing, snoozed := pending(p, dirPush)
	if len(missing) == 0 || snoozed {
		return 0
	}
	fp := state.Fingerprint(dirPush, missing)

	switch ask(missing, dirPush, true) {
	case prompt.AnswerYes:
		if err := envfile.AppendBlankKeys(p.example(), missing); err != nil {
			silentFail(err)
			return 0
		}
		switch {
		case isPartialCommit():
			warnf("added %s to .env.example, but this is a partial commit (git commit <path>), so it was NOT staged. Run `git add %s` and commit it.",
				joinKeys(missing), exampleFile)
		default:
			if err := gitutil.Add(p.example()); err != nil {
				warnf("added %s to .env.example, but could not stage it automatically (%v). Run `git add %s`.",
					joinKeys(missing), err, exampleFile)
			} else {
				warnf("added %s to .env.example and included it in this commit.", joinKeys(missing))
			}
		}
	case prompt.AnswerLocal:
		markLocal(p, missing)
	case prompt.AnswerUnavailable:
		warnf(".env has variable(s) not in .env.example: %s (run `git-envdiff` in a terminal to review)", joinKeys(missing))
		_ = state.Snooze(p.GitDir, dirPush, fp)
	default: // AnswerNo
		_ = state.Snooze(p.GitDir, dirPush, fp)
	}
	return 0
}

// isPartialCommit reports whether Git is committing through a temporary
// index — the case for `git commit <pathspec>`. There Git has already
// decided what the commit contains, so a file we change now would leave
// the real index and the commit disagreeing; we warn instead of staging.
//
// Verified against real Git: a plain commit uses ".git/index", and
// `git commit -a` uses ".git/index.lock" (both are safe to stage into),
// while a pathspec commit uses ".git/next-index-<pid>.lock".
func isPartialCommit() bool {
	idx := os.Getenv("GIT_INDEX_FILE")
	base := idx[strings.LastIndexAny(idx, `/\`)+1:] // Git for Windows may hand us either separator
	return strings.HasPrefix(base, "next-index")
}

// ---- pre-push (backstop only) -------------------------------------------

// RunPrePush checks the same direction as pre-commit, as a backstop for a
// commit made with --no-verify, an older install, or a repository where
// the pre-commit hook was never set up. It never modifies a file without
// asking, and a change accepted here always blocks the push (exit 1),
// because a fix made now cannot retroactively join the commits already
// being pushed — it must be committed first.
func RunPrePush() int {
	p, ok := prepare()
	if !ok {
		return 0
	}
	missing, snoozed := pending(p, dirPush)
	if len(missing) == 0 || snoozed {
		return 0
	}
	fp := state.Fingerprint(dirPush, missing)

	switch ask(missing, dirPush, true) {
	case prompt.AnswerYes:
		if err := envfile.AppendBlankKeys(p.example(), missing); err != nil {
			silentFail(err)
			return 0
		}
		warnf("added %s to .env.example. This is a NEW uncommitted change, so the push has been stopped — commit it and push again.",
			joinKeys(missing))
		return 1
	case prompt.AnswerLocal:
		markLocal(p, missing)
	case prompt.AnswerUnavailable:
		warnf("heads-up: .env has variable(s) not in .env.example: %s (run `git-envdiff` in a terminal to review)", joinKeys(missing))
		_ = state.Snooze(p.GitDir, dirPush, fp)
	default:
		_ = state.Snooze(p.GitDir, dirPush, fp)
	}
	return 0
}

// ---- post-merge / post-checkout (pull side) ------------------------------

// RunPostMerge handles the hook Git runs after a merge, including the
// fast-forward, true-merge, and no-local-commits-rebase forms of `git pull`.
func RunPostMerge(_ []string) int {
	// The single argument is "1" for a squash merge, "0" otherwise — irrelevant here.
	return runPullSide()
}

// RunPostCheckout handles the hook Git runs after checkout, including
// branch switches and the checkout step of `git pull --rebase` when there
// are local commits to replay. args are [prevHead, newHead, branchFlag].
// branchFlag "0" means a *file* checkout (e.g. `git checkout -- path`),
// which is deliberately ignored — only a HEAD/branch move is relevant.
func RunPostCheckout(args []string) int {
	if len(args) < 3 || args[2] != "1" {
		return 0
	}
	if args[0] == args[1] {
		return 0 // HEAD didn't move
	}
	return runPullSide()
}

func runPullSide() int {
	p, ok := prepare()
	if !ok {
		return 0
	}
	missing, snoozed := pending(p, dirPull)
	if len(missing) == 0 || snoozed {
		return 0
	}
	fp := state.Fingerprint(dirPull, missing)

	switch ask(missing, dirPull, false) {
	case prompt.AnswerYes:
		values, err := envfile.ParseKeyValues(p.example(), missing)
		if err != nil {
			silentFail(err)
			return 0
		}
		if err := envfile.AppendKeyValues(p.env(), missing, values); err != nil {
			silentFail(err)
			return 0
		}
		warnf("added %s to .env. Fill in real values for any that are blank.", joinKeys(missing))
	case prompt.AnswerUnavailable:
		warnf(".env.example has variable(s) missing from your .env: %s (run `git-envdiff` in a terminal to sync)", joinKeys(missing))
		_ = state.Snooze(p.GitDir, dirPull, fp)
	default:
		_ = state.Snooze(p.GitDir, dirPull, fp)
	}
	return 0
}

// ---- manual `git-envdiff` (no subcommand) --------------------------------

// RunManual implements plain `git-envdiff`: an explicit check of both
// directions. Unlike the hooks it ignores existing snoozes (you asked
// directly, so you see the current picture). Without a terminal it does
// not prompt; it prints what it found instead.
func RunManual() int {
	root, ok := locateRoot()
	if !ok {
		fmt.Fprintln(os.Stderr, "git-envdiff: not inside a git repository")
		return 1
	}
	gitDir, ok := gitDirFor(root)
	if !ok {
		fmt.Fprintln(os.Stderr, "git-envdiff: could not locate the .git directory")
		return 1
	}
	p := Paths{RepoRoot: root, GitDir: gitDir}

	hasEnv, hasExample := envfile.Exists(p.env()), envfile.Exists(p.example())

	if hasExample && !hasEnv {
		switch confirmFn("git-envdiff: .env.example exists but .env does not. Create .env from .env.example?", false) {
		case prompt.AnswerYes:
			if err := envfile.CreateFromTemplate(p.env(), p.example()); err != nil {
				fmt.Fprintf(os.Stderr, "git-envdiff: could not create .env: %v\n", err)
				return 1
			}
			fmt.Println("git-envdiff: created .env from .env.example. Fill in real values for any that are blank.")
		case prompt.AnswerUnavailable:
			fmt.Println("git-envdiff: .env.example exists but .env does not. Run `git-envdiff` in a terminal to create it.")
		}
		return 0
	}
	if !hasEnv || !hasExample {
		fmt.Println("git-envdiff: no .env / .env.example pair found in this repository. Nothing to check.")
		return 0
	}

	found := false

	if missing, err := missingKeys(p, dirPush); err == nil && len(missing) > 0 {
		found = true
		switch ask(missing, dirPush, true) {
		case prompt.AnswerYes:
			if err := envfile.AppendBlankKeys(p.example(), missing); err != nil {
				silentFail(err)
			} else {
				fmt.Printf("git-envdiff: added %s to .env.example.\n", joinKeys(missing))
			}
		case prompt.AnswerLocal:
			markLocal(p, missing)
		case prompt.AnswerUnavailable:
			fmt.Printf("git-envdiff: .env has variable(s) not in .env.example: %s\n", joinKeys(missing))
		default:
			_ = state.Snooze(p.GitDir, dirPush, state.Fingerprint(dirPush, missing))
		}
	}

	if missing, err := missingKeys(p, dirPull); err == nil && len(missing) > 0 {
		found = true
		switch ask(missing, dirPull, false) {
		case prompt.AnswerYes:
			values, _ := envfile.ParseKeyValues(p.example(), missing)
			if err := envfile.AppendKeyValues(p.env(), missing, values); err != nil {
				silentFail(err)
			} else {
				fmt.Printf("git-envdiff: added %s to .env.\n", joinKeys(missing))
			}
		case prompt.AnswerUnavailable:
			fmt.Printf("git-envdiff: .env.example has variable(s) missing from your .env: %s\n", joinKeys(missing))
		default:
			_ = state.Snooze(p.GitDir, dirPull, state.Fingerprint(dirPull, missing))
		}
	}

	if !found {
		fmt.Println("git-envdiff: no differences found. .env and .env.example are in sync.")
	}
	return 0
}

// ---- shared helpers -------------------------------------------------------

// pending computes the diff for one direction and whether it is snoozed.
// When there is no diff it also forgets that direction's old snoozes, so
// a snooze only lives as long as the exact diff it was made for.
func pending(p Paths, direction string) (missing []string, snoozed bool) {
	missing, err := missingKeys(p, direction)
	if err != nil {
		silentFail(err)
		return nil, false
	}
	if len(missing) == 0 {
		_ = state.ClearSnoozed(p.GitDir, direction)
		return nil, false
	}
	return missing, state.IsSnoozed(p.GitDir, direction, state.Fingerprint(direction, missing))
}

// missingKeys computes the diff for one direction. On the push direction
// only, keys the user has marked local-only (via the "l" answer) are
// excluded, since those are intentionally never meant to reach
// .env.example.
func missingKeys(p Paths, direction string) ([]string, error) {
	fromPath, toPath := p.example(), p.env()
	if direction == dirPush {
		fromPath, toPath = p.env(), p.example()
	}

	fromKeys, err := envfile.ParseKeys(fromPath)
	if err != nil {
		return nil, err
	}
	toKeys, err := envfile.ParseKeys(toPath)
	if err != nil {
		return nil, err
	}
	missing := envfile.Diff(fromKeys, toKeys)

	if direction != dirPush || len(missing) == 0 {
		return missing, nil
	}
	local, err := state.LocalKeys(p.GitDir)
	if err != nil {
		return missing, nil // best-effort; a state-file read error shouldn't block the check
	}
	filtered := missing[:0:0]
	for _, k := range missing {
		if !local[k] {
			filtered = append(filtered, k)
		}
	}
	return filtered, nil
}

func markLocal(p Paths, keys []string) {
	for _, k := range keys {
		_ = state.MarkLocal(p.GitDir, k)
	}
}

func joinKeys(keys []string) string {
	if len(keys) <= maxListed {
		return strings.Join(keys, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(keys[:maxListed], ", "), len(keys)-maxListed)
}

func ask(keys []string, direction string, allowLocal bool) prompt.Answer {
	var msg string
	if direction == dirPush {
		msg = fmt.Sprintf("git-envdiff: .env defines %s, not in .env.example.\nAdd to .env.example (name only, no value)?", joinKeys(keys))
	} else {
		msg = fmt.Sprintf("git-envdiff: .env.example defines %s, missing from your .env.\nAdd to .env (values copied from .env.example, usually blank)?", joinKeys(keys))
	}
	return confirmFn(msg, allowLocal)
}
