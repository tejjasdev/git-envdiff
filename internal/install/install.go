// Package install manages git-envdiff's Git hook registration.
//
// It has two strategies:
//
//   - Global config-based hooks (Git >= 2.54): one command registers
//     git-envdiff for every repository on the machine, and coexists
//     cleanly with each repository's own hooks and with core.hooksPath
//     (e.g. Husky) — verified against real Git in this project's test
//     suite and documented in docs/HOW_IT_WORKS.md.
//   - A legacy per-repository fallback: a clearly marked block appended
//     into the repository's own .git/hooks/<event> files, immediately
//     after the shebang line so an existing hook's own exec/exit calls
//     are never skipped.
//
// Neither strategy ever touches a line of hook content it did not add
// itself, and Uninstall removes exactly what Install added.
package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tejjasdev/git-envdiff/internal/gitutil"
	"github.com/tejjasdev/git-envdiff/internal/state"
)

var events = []string{"pre-commit", "pre-push", "post-merge", "post-checkout"}

const (
	beginMarker = "# >>> git-envdiff (managed) >>>"
	endMarker   = "# <<< git-envdiff <<<"
)

// guardedCommand is the shell fragment run for every hook event, in both
// installation strategies. It is deliberately defensive: if the
// git-envdiff binary is ever removed — a package manager uninstall, or a
// PATH that a GUI client doesn't share with the shell — `command -v`
// fails and the whole fragment exits 0 (doing nothing) rather than
// breaking every commit and push on the machine. If the binary IS present
// and it exits non-zero, that exit code still propagates normally.
func guardedCommand(event string) string {
	return fmt.Sprintf("command -v git-envdiff >/dev/null 2>&1 || exit 0; git-envdiff hook %s", event)
}

// Result summarizes what an Install or Uninstall call did, for printing.
type Result struct {
	Mode     string // "global-config" or "repo-hooks"
	Messages []string
}

// Install registers git-envdiff's hooks.
//
// If repoOnly is false and the installed Git supports config-based hooks
// (>= 2.54), it installs once, globally, for every repository on the
// machine. Otherwise — including whenever repoOnly is true — it installs
// into the current repository's own .git/hooks only.
func Install(repoOnly bool) (Result, error) {
	if !repoOnly && gitutil.SupportsConfigHooks() {
		return installGlobal()
	}
	res, err := installRepo()
	if err == nil && !repoOnly {
		res.Messages = append(res.Messages,
			"Note: Git < 2.54 detected, so this only installed into the current repository.",
			"Run `git-envdiff install` again in each repository, or upgrade Git to 2.54+ for a one-time global install.",
		)
	}
	return res, err
}

func installGlobal() (Result, error) {
	res := Result{Mode: "global-config"}
	for _, ev := range events {
		if err := gitutil.ConfigSetGlobal(fmt.Sprintf("hook.git-envdiff-%s.event", ev), ev); err != nil {
			return res, fmt.Errorf("configuring %s hook: %w", ev, err)
		}
		if err := gitutil.ConfigSetGlobal(fmt.Sprintf("hook.git-envdiff-%s.command", ev), guardedCommand(ev)); err != nil {
			return res, fmt.Errorf("configuring %s hook: %w", ev, err)
		}
	}
	res.Messages = []string{
		"Installed globally via Git's config-based hooks (Git >= 2.54).",
		"This now works in every repository on this machine — nothing else to do per-project.",
	}
	return res, nil
}

// Uninstall removes exactly what Install added: the global config hooks
// (unless repoOnly), and the managed block from the current repository's
// hook files, if present. Safe to call even if nothing was installed.
func Uninstall(repoOnly bool) (Result, error) {
	var messages []string

	if !repoOnly {
		removedAny := false
		for _, ev := range events {
			section := fmt.Sprintf("hook.git-envdiff-%s", ev)
			if existed, _ := gitutil.ConfigSectionExists("--global", section); existed {
				removedAny = true
			}
			_ = gitutil.ConfigUnsetGlobalSection(section)
		}
		if removedAny {
			messages = append(messages, "Removed global config-based hooks.")
		}
	}

	repoMsgs, err := uninstallRepo()
	messages = append(messages, repoMsgs...)

	if len(messages) == 0 {
		messages = []string{"Nothing to remove (git-envdiff was not installed here)."}
	}
	return Result{Messages: messages}, err
}

// installRepo installs the managed block into the current repository's
// own hook files, or — if core.hooksPath is configured (e.g. by Husky) —
// prints the line the user needs to add themselves, since git-envdiff
// won't guess at the structure of a hooks setup it doesn't understand.
func installRepo() (Result, error) {
	res := Result{Mode: "repo-hooks"}

	gitDir, err := gitutil.GitDir()
	if err != nil {
		return res, errors.New("not inside a git repository")
	}

	if hooksPath, ok := gitutil.HooksPathConfigured(); ok {
		res.Messages = []string{
			fmt.Sprintf("This repository uses a custom core.hooksPath (%s), so git-envdiff did not modify it automatically.", hooksPath),
			"Add a line like this to each hook file there (one per event: pre-commit, pre-push, post-merge, post-checkout):",
			"  " + guardedCommand("<event>") + ` "$@"`,
		}
		return res, nil
	}

	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return res, err
	}

	var problems []string
	for _, ev := range events {
		if err := installIntoFile(filepath.Join(hooksDir, ev), ev); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", ev, err))
		}
	}
	res.Messages = append(res.Messages, "Installed into this repository's .git/hooks (existing hooks were preserved).")
	res.Messages = append(res.Messages, problems...)
	return res, nil
}

// block returns the managed snippet inserted into a legacy hook file.
func block(event string) string {
	return beginMarker + "\n" +
		"if command -v git-envdiff >/dev/null 2>&1; then\n" +
		"  git-envdiff hook " + event + ` "$@" || exit $?` + "\n" +
		"fi\n" +
		endMarker + "\n"
}

// installIntoFile adds the managed block to path, right after its shebang
// line. If the file doesn't exist yet, it is created with a plain POSIX
// shebang. If it exists but isn't a recognizable shell script, nothing is
// written — installIntoFile returns an error describing the manual step
// instead, rather than guessing wrong about an unfamiliar hook.
func installIntoFile(path, event string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		content := "#!/bin/sh\n" + block(event)
		return os.WriteFile(path, []byte(content), 0o755)
	}
	if err != nil {
		return err
	}

	content := string(data)
	if strings.Contains(content, beginMarker) {
		return nil // already installed
	}

	lines := strings.SplitAfter(content, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "#!") {
		return fmt.Errorf("existing hook has no recognizable #! shebang; add manually:\n    %s", guardedCommand(event))
	}
	shebang := strings.ToLower(lines[0])
	if !strings.Contains(shebang, "sh") {
		return fmt.Errorf("existing hook is not a shell script (%s); add manually:\n    %s",
			strings.TrimSpace(lines[0]), guardedCommand(event))
	}

	newContent := lines[0] + block(event) + strings.Join(lines[1:], "")

	mode := os.FileMode(0o755)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode()
	}
	return os.WriteFile(path, []byte(newContent), mode)
}

// uninstallRepo strips the managed block from each hook file that has one.
// If nothing but the shebang line is left afterwards — meaning
// git-envdiff created the file from scratch — the file is removed
// entirely; otherwise whatever else was already there is left untouched.
func uninstallRepo() ([]string, error) {
	gitDir, err := gitutil.GitDir()
	if err != nil {
		return nil, nil // not inside a repo; nothing repo-local to do
	}
	hooksDir := filepath.Join(gitDir, "hooks")

	var messages []string
	for _, ev := range events {
		path := filepath.Join(hooksDir, ev)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		start := strings.Index(content, beginMarker)
		if start == -1 {
			continue
		}
		end := strings.Index(content, endMarker)
		if end == -1 {
			continue
		}
		end += len(endMarker)
		if end < len(content) && content[end] == '\n' {
			end++
		}
		remainder := content[:start] + content[end:]

		if isOnlyShebang(remainder) {
			_ = os.Remove(path)
		} else {
			mode := os.FileMode(0o755)
			if info, statErr := os.Stat(path); statErr == nil {
				mode = info.Mode()
			}
			_ = os.WriteFile(path, []byte(remainder), mode)
		}
		messages = append(messages, fmt.Sprintf("Removed git-envdiff from .git/hooks/%s", ev))
	}
	return messages, nil
}

func isOnlyShebang(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#!") {
			continue
		}
		return false
	}
	return true
}

// Status returns a short, human-readable report of what's currently
// installed, for `git-envdiff status`.
func Status() string {
	var b strings.Builder

	if _, err := exec.LookPath("git-envdiff"); err != nil {
		b.WriteString("WARNING: git-envdiff is not on PATH in this shell, so installed hooks will silently do nothing.\n")
	}

	if ok, _ := gitutil.ConfigSectionExists("--global", "hook.git-envdiff-pre-commit"); ok {
		b.WriteString("Global config-based hooks: installed\n")
	} else {
		b.WriteString("Global config-based hooks: not installed\n")
	}

	gitDir, err := gitutil.GitDir()
	if err != nil {
		b.WriteString("Not inside a Git repository.\n")
		return b.String()
	}

	found := false
	for _, ev := range events {
		data, err := os.ReadFile(filepath.Join(gitDir, "hooks", ev))
		if err == nil && strings.Contains(string(data), beginMarker) {
			found = true
		}
	}
	if found {
		b.WriteString("This repository's own .git/hooks: git-envdiff installed\n")
	} else {
		b.WriteString("This repository's own .git/hooks: git-envdiff not installed\n")
	}

	if hooksPath, ok := gitutil.HooksPathConfigured(); ok {
		fmt.Fprintf(&b, "Note: core.hooksPath is set to %q here (e.g. Husky) — see `git-envdiff install` output for the manual step.\n", hooksPath)
	}

	if state.IsDisabled(gitDir) {
		b.WriteString("git-envdiff is currently DISABLED in this repository (run `git-envdiff on` to re-enable).\n")
	}

	if local, err := state.LocalKeys(gitDir); err == nil && len(local) > 0 {
		names := make([]string, 0, len(local))
		for k := range local {
			names = append(names, k)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "Local-only variables (never proposed for .env.example): %s\n", strings.Join(names, ", "))
	}

	return b.String()
}
