// Package gitutil wraps the handful of `git` invocations git-envdiff needs:
// locating the repository root and .git directory, reading a little
// config, staging a file, and detecting the installed Git version. It
// never reads or writes repository *content* — only plumbing.
package gitutil

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotAGitRepo is returned by RepoRoot/GitDir when run outside of a Git
// working tree.
var ErrNotAGitRepo = errors.New("not inside a git repository")

func run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// RepoRoot returns the absolute path to the working tree root of the
// current repository.
func RepoRoot() (string, error) {
	out, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", ErrNotAGitRepo
	}
	return out, nil
}

// GitDir returns the absolute path to the current repository's .git
// directory (which may live outside the work tree — worktrees, submodules).
func GitDir() (string, error) {
	out, err := run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", ErrNotAGitRepo
	}
	return out, nil
}

// HooksPathConfigured reports whether core.hooksPath is set for the
// current repository (as, for example, Husky sets it), and its value if so.
func HooksPathConfigured() (string, bool) {
	out, err := run("config", "--get", "core.hooksPath")
	if err != nil || out == "" {
		return "", false
	}
	return out, true
}

// versionFieldRE pulls the leading "MAJOR.MINOR" out of a Git version
// token, tolerating trailing platform suffixes like ".windows.1".
var versionFieldRE = regexp.MustCompile(`^(\d+)\.(\d+)`)

// Version returns the installed Git's (major, minor) version numbers, as
// reported by `git --version`.
func Version() (major, minor int, err error) {
	out, err := run("--version")
	if err != nil {
		return 0, 0, err
	}
	for _, field := range strings.Fields(out) {
		m := versionFieldRE.FindStringSubmatch(field)
		if m == nil {
			continue
		}
		maj, err1 := strconv.Atoi(m[1])
		min, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil {
			return maj, min, nil
		}
	}
	return 0, 0, fmt.Errorf("could not parse git version from %q", out)
}

// SupportsConfigHooks reports whether the installed Git is new enough to
// support config-defined hooks (the `[hook "name"]` config section),
// introduced in Git 2.54.
func SupportsConfigHooks() bool {
	maj, min, err := Version()
	if err != nil {
		return false
	}
	return maj > 2 || (maj == 2 && min >= 54)
}

// ConfigSetGlobal sets a global git config value.
func ConfigSetGlobal(key, value string) error {
	_, err := run("config", "--global", key, value)
	return err
}

// ConfigSectionExists reports whether any key exists under the given
// config section (e.g. "hook.git-envdiff-pre-commit") in the given scope
// ("--global" or "--local").
func ConfigSectionExists(scope, section string) (bool, error) {
	out, err := run("config", scope, "--get-regexp", "^"+regexp.QuoteMeta(section)+`\.`)
	if err != nil {
		// git exits non-zero (with empty output) when nothing matches;
		// that is the expected "not configured" case, not a real error.
		return false, nil
	}
	return strings.TrimSpace(out) != "", nil
}

// ConfigUnsetGlobalSection removes an entire global config section (e.g.
// "hook.git-envdiff-pre-commit"). It is not an error if the section does
// not exist.
func ConfigUnsetGlobalSection(section string) error {
	_, err := run("config", "--global", "--remove-section", section)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such section") {
		return nil
	}
	return err
}

// Add stages a path in the current repository. Used only to fold an
// accepted .env.example fix into the commit already being made.
func Add(path string) error {
	_, err := run("add", "--", path)
	return err
}
