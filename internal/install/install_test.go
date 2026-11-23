package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tejjasdev/git-envdiff/internal/gitutil"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newIsolatedRepo creates a fresh repo in a temp dir, chdir's into it, and
// points $HOME at a throwaway directory too, so tests that write *global*
// git config never touch the sandbox's real environment.
func newIsolatedRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return dir
}

// ---- installIntoFile / uninstallRepo: the file-surgery rules ------------

func TestInstallIntoFile_CreatesNewExecutableHookFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pre-commit")

	if err := installIntoFile(path, "pre-commit"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatal("expected the created hook file to be executable")
	}
	content := readFile(t, path)
	if !strings.HasPrefix(content, "#!/bin/sh\n") {
		t.Fatalf("expected a plain POSIX shebang, got:\n%s", content)
	}
	if !strings.Contains(content, beginMarker) || !strings.Contains(content, endMarker) {
		t.Fatalf("expected managed markers, got:\n%s", content)
	}
	if !strings.Contains(content, "git-envdiff hook pre-commit") {
		t.Fatalf("expected the hook to invoke git-envdiff, got:\n%s", content)
	}
}

func TestInstallIntoFile_InsertsRightAfterShebangPreservingExistingContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pre-commit")
	original := "#!/bin/bash\necho \"existing hook ran\"\nexit 0\n"
	if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installIntoFile(path, "pre-commit"); err != nil {
		t.Fatal(err)
	}

	content := readFile(t, path)
	if !strings.HasPrefix(content, "#!/bin/bash\n"+beginMarker) {
		t.Fatalf("expected the managed block to start immediately after the shebang, got:\n%s", content)
	}
	if !strings.Contains(content, `echo "existing hook ran"`) {
		t.Fatalf("existing hook content was lost:\n%s", content)
	}
	if !strings.Contains(content, "exit 0") {
		t.Fatalf("existing hook's own exit was lost:\n%s", content)
	}
	// The managed block must come BEFORE the existing exit 0, so the
	// existing hook's own exit can never prevent git-envdiff from running.
	if strings.Index(content, beginMarker) > strings.Index(content, "exit 0") {
		t.Fatalf("managed block must run before the existing hook's own exit:\n%s", content)
	}
}

func TestInstallIntoFile_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pre-commit")

	if err := installIntoFile(path, "pre-commit"); err != nil {
		t.Fatal(err)
	}
	if err := installIntoFile(path, "pre-commit"); err != nil {
		t.Fatal(err)
	}

	content := readFile(t, path)
	if strings.Count(content, beginMarker) != 1 {
		t.Fatalf("expected exactly one managed block after installing twice, got:\n%s", content)
	}
}

func TestInstallIntoFile_RefusesNonShellHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pre-commit")
	original := "#!/usr/bin/env node\nconsole.log('hi')\n"
	if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installIntoFile(path, "pre-commit"); err == nil {
		t.Fatal("expected an error for a non-shell hook, got nil")
	}

	content := readFile(t, path)
	if content != original {
		t.Fatalf("a non-shell hook must be left completely untouched, got:\n%s", content)
	}
}

func TestInstallIntoFile_RefusesFileWithNoShebang(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pre-commit")
	original := "echo no shebang here\n"
	if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installIntoFile(path, "pre-commit"); err == nil {
		t.Fatal("expected an error for a hook with no shebang, got nil")
	}
	if readFile(t, path) != original {
		t.Fatal("a hook with no shebang must be left untouched")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// ---- Install / Uninstall: end-to-end, against real (sandboxed) Git ------

func TestInstall_RepoOnlyWritesHooksAndUninstallRemovesThem(t *testing.T) {
	dir := newIsolatedRepo(t)

	res, err := Install(true)
	if err != nil {
		t.Fatalf("Install: %v\n%v", err, res.Messages)
	}
	if res.Mode != "repo-hooks" {
		t.Fatalf("expected repo-hooks mode with repoOnly=true, got %q", res.Mode)
	}
	for _, ev := range events {
		path := filepath.Join(dir, ".git", "hooks", ev)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s hook to exist: %v", ev, err)
		}
	}

	uninstallRes, err := Uninstall(true)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(uninstallRes.Messages) == 0 {
		t.Fatal("expected uninstall to report what it removed")
	}
	for _, ev := range events {
		path := filepath.Join(dir, ".git", "hooks", ev)
		if _, err := os.Stat(path); err == nil {
			data := readFile(t, path)
			if strings.Contains(data, beginMarker) {
				t.Fatalf("expected %s hook to no longer contain the managed block", ev)
			}
		}
		// If the file is gone entirely, that's also correct: git-envdiff
		// created it from nothing, so removing it entirely is expected.
	}
}

func TestUninstallRepo_PreservesHookThatHadOtherContent(t *testing.T) {
	dir := newIsolatedRepo(t)
	hookPath := filepath.Join(dir, ".git", "hooks", "pre-commit")
	original := "#!/bin/sh\necho keep-me\n"
	if err := os.WriteFile(hookPath, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(true); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(true); err != nil {
		t.Fatal(err)
	}

	content := readFile(t, hookPath)
	if !strings.Contains(content, "echo keep-me") {
		t.Fatalf("pre-existing hook content must survive uninstall, got:\n%s", content)
	}
	if strings.Contains(content, beginMarker) {
		t.Fatalf("managed block should have been removed, got:\n%s", content)
	}
}

func TestInstall_GlobalConfigHooksRoundtrip(t *testing.T) {
	if !gitutil.SupportsConfigHooks() {
		t.Skip("installed git does not support config-based hooks (< 2.54)")
	}
	newIsolatedRepo(t)

	res, err := Install(false)
	if err != nil {
		t.Fatalf("Install: %v\n%v", err, res.Messages)
	}
	if res.Mode != "global-config" {
		t.Fatalf("expected global-config mode on Git >= 2.54, got %q", res.Mode)
	}
	for _, ev := range events {
		ok, err := gitutil.ConfigSectionExists("--global", "hook.git-envdiff-"+ev)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("expected global hook.git-envdiff-%s section to exist", ev)
		}
	}

	if _, err := Uninstall(false); err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		ok, _ := gitutil.ConfigSectionExists("--global", "hook.git-envdiff-"+ev)
		if ok {
			t.Fatalf("expected global hook.git-envdiff-%s section to be removed", ev)
		}
	}
}

func TestInstall_RepoFlagForcesRepoHooksEvenOnNewGit(t *testing.T) {
	if !gitutil.SupportsConfigHooks() {
		t.Skip("this test specifically exercises the override on Git >= 2.54")
	}
	dir := newIsolatedRepo(t)

	res, err := Install(true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "repo-hooks" {
		t.Fatalf("--repo must force repo-hooks mode even on new Git, got %q", res.Mode)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit")); err != nil {
		t.Fatalf("expected a repo-local pre-commit hook: %v", err)
	}
}

func TestInstall_HuskyStyleHooksPathGetsInstructionsNotFiles(t *testing.T) {
	dir := newIsolatedRepo(t)
	huskyDir := filepath.Join(dir, ".husky")
	if err := os.MkdirAll(huskyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "config", "core.hooksPath", ".husky")

	res, err := Install(true)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, ".husky") {
		t.Fatalf("expected the custom hooksPath to be mentioned, got:\n%s", joined)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit")); err == nil {
		t.Fatal("must not write into .git/hooks when core.hooksPath is set elsewhere")
	}
	entries, _ := os.ReadDir(huskyDir)
	if len(entries) != 0 {
		t.Fatal("must not write into the custom hooksPath directory either — only print instructions")
	}
}

func TestStatus_ReflectsRepoInstallState(t *testing.T) {
	newIsolatedRepo(t)

	before := Status()
	if !strings.Contains(before, "not installed") {
		t.Fatalf("expected 'not installed' before Install, got:\n%s", before)
	}

	if _, err := Install(true); err != nil {
		t.Fatal(err)
	}
	after := Status()
	if !strings.Contains(after, "git-envdiff installed") {
		t.Fatalf("expected status to reflect the repo install, got:\n%s", after)
	}
}

func TestInstall_RepoOnlyReportsErrorWhenAHookCannotBeInstalled(t *testing.T) {
	dir := newIsolatedRepo(t)
	nodeHook := "#!/usr/bin/env node\nconsole.log('hi')\n"
	prePush := filepath.Join(dir, ".git", "hooks", "pre-push")
	if err := os.WriteFile(prePush, []byte(nodeHook), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Install(true)
	if err == nil {
		t.Fatalf("Install(true) must return an error when a hook could not be installed, got nil; messages=%v", res.Messages)
	}
	for _, ev := range []string{"pre-commit", "post-merge", "post-checkout"} {
		if !strings.Contains(readFile(t, filepath.Join(dir, ".git", "hooks", ev)), beginMarker) {
			t.Fatalf("%s must contain the managed block (hooks that could be installed stay installed, no rollback)", ev)
		}
	}
	if got := readFile(t, prePush); got != nodeHook {
		t.Fatalf("the node pre-push hook must be byte-identical, got %q", got)
	}

	// idempotent: once the blocker is removed, re-running must succeed
	if err := os.Remove(prePush); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(true); err != nil {
		t.Fatalf("re-run after removing the blocker must succeed, got %v", err)
	}
}

func TestInstall_PartialFailureStillShowsOldGitNote(t *testing.T) {
	if gitutil.SupportsConfigHooks() {
		t.Skip("only relevant when the installed Git lacks config-based hooks (< 2.54)")
	}
	dir := newIsolatedRepo(t)
	nodeHook := "#!/usr/bin/env node\nconsole.log('hi')\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-push"), []byte(nodeHook), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Install(false)
	if err == nil {
		t.Fatal("expected a non-nil error on partial failure")
	}
	found := false
	for _, m := range res.Messages {
		if strings.Contains(m, "Git < 2.54") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the Git < 2.54 note even on partial failure, messages=%v", res.Messages)
	}
}

func TestInstall_HooksPathCaseStillSucceeds(t *testing.T) {
	if gitutil.SupportsConfigHooks() {
		t.Skip("only relevant when the installed Git lacks config-based hooks (< 2.54)")
	}
	dir := newIsolatedRepo(t)
	runGit(t, dir, "config", "core.hooksPath", ".husky")

	res, err := Install(false)
	if err != nil {
		t.Fatalf("hooksPath case must keep succeeding (nil error), got %v (messages=%v)", err, res.Messages)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "core.hooksPath") {
		t.Fatalf("expected the manual-line messages, got %v", res.Messages)
	}
	if !strings.Contains(joined, "Git < 2.54") {
		t.Fatalf("expected the Git < 2.54 note in the hooksPath case too, got %v", res.Messages)
	}
}

func TestInstallIntoFile_RefusesFishAndPwshHooks(t *testing.T) {
	for _, shebang := range []string{"#!/usr/bin/env fish", "#!/usr/bin/env pwsh", "#!/bin/tcsh"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "pre-commit")
		original := shebang + "\necho custom\n"
		if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := installIntoFile(path, "pre-commit"); err == nil {
			t.Fatalf("%s: expected an error (non-POSIX hook), got nil", shebang)
		}
		if got := readFile(t, path); got != original {
			t.Fatalf("%s: file must be byte-identical, got %q", shebang, got)
		}
	}
}

func TestInstallIntoFile_AcceptsPOSIXShellVariants(t *testing.T) {
	for _, shebang := range []string{"#!/bin/sh", "#!/usr/bin/env bash", "#!/usr/bin/env -S bash -e", "#!/bin/sh\r"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "pre-commit")
		original := shebang + "\necho custom\n"
		if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := installIntoFile(path, "pre-commit"); err != nil {
			t.Fatalf("%q: POSIX shell hook must be accepted, got %v", shebang, err)
		}
		content := readFile(t, path)
		if !strings.Contains(content, beginMarker) {
			t.Fatalf("%q: managed block missing, got:\n%s", shebang, content)
		}
		if !strings.Contains(content, "echo custom") {
			t.Fatalf("%q: existing content lost, got:\n%s", shebang, content)
		}
	}
}

func TestInstall_OutsideRepoShowsNoOldGitNote(t *testing.T) {
	if gitutil.SupportsConfigHooks() {
		t.Skip("only relevant when the installed Git lacks config-based hooks (< 2.54)")
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir() // not a git repository
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	res, err := Install(false)
	if err == nil {
		t.Fatal("Install outside a repository must return an error")
	}
	for _, m := range res.Messages {
		if strings.Contains(m, "Git < 2.54") {
			t.Fatalf("the Git < 2.54 note must not appear when there is nothing to install, messages=%v", res.Messages)
		}
	}
}

func TestIsPOSIXShellShebang(t *testing.T) {
	cr := string(rune(13))
	trueCases := []string{
		"#!/bin/sh", "#!/bin/sh -e", "#! /bin/bash", "#!/usr/bin/env bash",
		"#!/usr/bin/env -S bash -e", "#!/bin/zsh", "#!/bin/sh" + cr,
		"#!/usr/bin/env FOO=1 sh", "#!/bin/dash", "#!/bin/ash", "#!/bin/ksh",
	}
	for _, tc := range trueCases {
		if !isPOSIXShellShebang(tc) {
			t.Errorf("expected true for %q", tc)
		}
	}
	falseCases := []string{
		"#!/usr/bin/env fish", "#!/usr/bin/env pwsh", "#!/bin/tcsh", "#!/bin/csh",
		"#!/usr/bin/env python3", "#!/usr/bin/env node", "#!/usr/bin/perl",
		"#!/usr/bin/env ruby", "#!", "#!/usr/bin/env", "#!/usr/bin/env -S",
		"#!/bin/busybox sh", "#!/bin/shell",
	}
	for _, fc := range falseCases {
		if isPOSIXShellShebang(fc) {
			t.Errorf("expected false for %q", fc)
		}
	}
}
