package hooks

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tejjasdev/git-envdiff/internal/prompt"
	"github.com/tejjasdev/git-envdiff/internal/state"
)

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates a fresh, minimally-configured git repository in a temp
// directory and chdir's the test process into it (restored via t.Cleanup).
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")

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

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// withAnswer temporarily replaces the confirmation function with one that
// always returns answer, restoring the real one on cleanup.
func withAnswer(t *testing.T, answer prompt.Answer) {
	t.Helper()
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer { return answer }
	t.Cleanup(func() { confirmFn = orig })
}

func TestRunPreCommit_AcceptedFixIsStagedIntoPlainCommit(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "EXISTING=1\nNEW_API_KEY=super-secret-value\n")
	writeFile(t, dir, ".env.example", "EXISTING=\n")
	git(t, dir, "add", ".env.example")

	withAnswer(t, prompt.AnswerYes)

	if code := RunPreCommit(); code != 0 {
		t.Fatalf("RunPreCommit returned %d, want 0 (must never block a commit)", code)
	}

	// Simulate the rest of a normal `git commit`: the staged .env.example
	// (which the hook should have `git add`-ed) is what gets committed.
	git(t, dir, "commit", "-q", "-m", "add example")

	content := git(t, dir, "show", "HEAD:.env.example")
	if !strings.Contains(content, "NEW_API_KEY=") {
		t.Fatalf(".env.example missing NEW_API_KEY=, got:\n%s", content)
	}
	if strings.Contains(content, "super-secret-value") {
		t.Fatalf("secret value leaked into committed .env.example:\n%s", content)
	}
}

func TestRunPreCommit_PartialCommitWarnsButDoesNotAutoStage(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")
	writeFile(t, dir, "app.py", "print(1)")

	withAnswer(t, prompt.AnswerYes)

	// A partial commit (git commit <pathspec>) makes Git use a temporary
	// index file; simulate that environment variable directly. t.Setenv
	// restores the previous state (including "unset") automatically,
	// which plain os.Setenv + defer does not do correctly when the
	// variable started out unset.
	t.Setenv("GIT_INDEX_FILE", filepath.Join(dir, ".git", "next-index-1234.lock"))

	if code := RunPreCommit(); code != 0 {
		t.Fatalf("RunPreCommit returned %d, want 0", code)
	}

	content := readFile(t, dir, ".env.example")
	if !strings.Contains(content, "NEW_KEY=") {
		t.Fatalf("expected NEW_KEY= to still be written to the working tree, got:\n%s", content)
	}

	status := git(t, dir, "status", "--porcelain", "--", ".env.example")
	if strings.HasPrefix(status, "A") || strings.HasPrefix(status, "M ") {
		// "M " (staged) would indicate we incorrectly staged it; an
		// untracked/modified-unstaged status is expected instead.
		t.Fatalf("did not expect .env.example to be staged during a partial commit, git status: %q", status)
	}
}

func TestRunPreCommit_DeclineSnoozesSameDiff(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")

	asked := 0
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer {
		asked++
		return prompt.AnswerNo
	}
	defer func() { confirmFn = orig }()

	RunPreCommit()
	RunPreCommit() // same diff again — should be silent, not re-prompt

	if asked != 1 {
		t.Fatalf("expected exactly 1 prompt for an unchanged diff, got %d", asked)
	}

	content := readFile(t, dir, ".env.example")
	if strings.Contains(content, "NEW_KEY") {
		t.Fatalf("declined key should not have been written: %q", content)
	}
}

func TestRunPreCommit_LocalAnswerExcludesKeyGoingForward(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "LOCAL_ONLY=x\n")
	writeFile(t, dir, ".env.example", "")

	withAnswer(t, prompt.AnswerLocal)
	RunPreCommit()

	content := readFile(t, dir, ".env.example")
	if content != "" {
		t.Fatalf(".env.example should be untouched after a local-only answer, got: %q", content)
	}

	// Now even if the user were asked again and said yes, the key should
	// no longer be considered "missing" at all.
	withAnswer(t, prompt.AnswerYes)
	RunPreCommit()

	content = readFile(t, dir, ".env.example")
	if strings.Contains(content, "LOCAL_ONLY") {
		t.Fatalf("a key marked local-only should never be proposed again, got: %q", content)
	}
}

func TestRunPreCommit_NoDiffIsCompletelySilent(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "A=1\n")
	writeFile(t, dir, ".env.example", "A=\n")

	called := false
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer {
		called = true
		return prompt.AnswerNo
	}
	defer func() { confirmFn = orig }()

	if code := RunPreCommit(); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
	if called {
		t.Fatal("expected no prompt when .env and .env.example already agree")
	}
}

func TestRunPreCommit_MissingFilesIsANoOp(t *testing.T) {
	newRepo(t) // no .env / .env.example at all
	if code := RunPreCommit(); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
}

func TestRunPrePush_AcceptedFixBlocksPushAndWritesNoValue(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=another-secret\n")
	writeFile(t, dir, ".env.example", "")

	withAnswer(t, prompt.AnswerYes)

	code := RunPrePush()
	if code != 1 {
		t.Fatalf("expected pre-push to return 1 (block) when a fix was just made, got %d", code)
	}

	content := readFile(t, dir, ".env.example")
	if !strings.Contains(content, "NEW_KEY=\n") {
		t.Fatalf("expected NEW_KEY= to be appended, got: %q", content)
	}
	if strings.Contains(content, "another-secret") {
		t.Fatalf("secret value leaked into .env.example: %q", content)
	}
}

func TestRunPrePush_DeclineDoesNotBlock(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")

	withAnswer(t, prompt.AnswerNo)

	if code := RunPrePush(); code != 0 {
		t.Fatalf("declining should never block a push, got %d", code)
	}
}

func TestRunPrePush_NoDiffNeverPrompts(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "A=1\n")
	writeFile(t, dir, ".env.example", "A=\n")

	called := false
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer { called = true; return prompt.AnswerNo }
	defer func() { confirmFn = orig }()

	if code := RunPrePush(); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
	if called {
		t.Fatal("expected no prompt when there is no diff")
	}
}

func TestRunPostMerge_AddsMissingKeysToEnvWithTemplateValues(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "EXISTING=1\n")
	writeFile(t, dir, ".env.example", "EXISTING=\nSTRIPE_KEY=\nREDIS_URL=redis://localhost:6379\n")

	withAnswer(t, prompt.AnswerYes)

	if code := RunPostMerge([]string{"0"}); code != 0 {
		t.Fatalf("got %d, want 0 (pull side never blocks)", code)
	}

	content := readFile(t, dir, ".env")
	if !strings.Contains(content, "STRIPE_KEY=\n") {
		t.Fatalf("expected blank STRIPE_KEY= copied in, got:\n%s", content)
	}
	if !strings.Contains(content, "REDIS_URL=redis://localhost:6379\n") {
		t.Fatalf("expected template's own REDIS_URL value copied in, got:\n%s", content)
	}
}

func TestRunPostCheckout_IgnoresFileCheckouts(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "")
	writeFile(t, dir, ".env.example", "NEW=default\n")

	called := false
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer { called = true; return prompt.AnswerYes }
	defer func() { confirmFn = orig }()

	// flag "0" is a plain file checkout, e.g. `git checkout -- path`.
	RunPostCheckout([]string{"aaa", "aaa", "0"})
	if called {
		t.Fatal("a file checkout (flag 0) must not trigger a check")
	}

	// flag "1" is a real HEAD move (branch switch, or the rebase replay step).
	RunPostCheckout([]string{"aaa", "bbb", "1"})
	if !called {
		t.Fatal("a branch switch (flag 1) should trigger a check")
	}
}

func TestRunManual_CreatesEnvFromTemplateWhenMissing(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env.example", "A=\nB=default\n")

	withAnswer(t, prompt.AnswerYes)

	if code := RunManual(); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
	content := readFile(t, dir, ".env")
	if content != "A=\nB=default\n" {
		t.Fatalf("got %q", content)
	}
}

func TestOnOff_DisablesAndReenablesChecking(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW=x\n")
	writeFile(t, dir, ".env.example", "")

	gitDir := filepath.Join(dir, ".git")
	if err := state.SetDisabled(gitDir, true); err != nil {
		t.Fatal(err)
	}

	called := false
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer { called = true; return prompt.AnswerYes }
	defer func() { confirmFn = orig }()

	RunPreCommit()
	if called {
		t.Fatal("expected no prompt while disabled")
	}

	if err := state.SetDisabled(gitDir, false); err != nil {
		t.Fatal(err)
	}
	RunPreCommit()
	if !called {
		t.Fatal("expected a prompt again after re-enabling")
	}
}

func TestEnvSkip_BypassesTheHookEntirely(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW=x\n")
	writeFile(t, dir, ".env.example", "")

	t.Setenv("GIT_ENVDIFF_SKIP", "1")

	called := false
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer { called = true; return prompt.AnswerYes }
	defer func() { confirmFn = orig }()

	RunPreCommit()
	if called {
		t.Fatal("GIT_ENVDIFF_SKIP=1 should bypass the check entirely")
	}
}

func TestIsPartialCommit(t *testing.T) {
	cases := map[string]bool{
		"":                                  false, // not set
		".git/index":                        false, // plain `git commit`
		"/repo/.git/index.lock":             false, // `git commit -a` (safe to stage into)
		"/repo/.git/next-index-4477.lock":   true,  // `git commit <pathspec>`
		"C:\\repo\\.git\\next-index-9.lock": true,
	}
	for idx, want := range cases {
		t.Setenv("GIT_INDEX_FILE", idx)
		if got := isPartialCommit(); got != want {
			t.Errorf("GIT_INDEX_FILE=%q: got %v, want %v", idx, got, want)
		}
	}
}

// A snooze must only live as long as the exact diff it was made for: if
// the diff goes away and later comes back, the user is asked again.
func TestSnoozeExpiresWhenDiffDisappears(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")

	asked := 0
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer { asked++; return prompt.AnswerNo }
	defer func() { confirmFn = orig }()

	RunPreCommit() // asked, declined -> snoozed
	RunPreCommit() // same diff -> silent
	if asked != 1 {
		t.Fatalf("expected 1 prompt so far, got %d", asked)
	}

	writeFile(t, dir, ".env.example", "NEW_KEY=\n") // diff disappears
	RunPreCommit()                                  // clears the snooze
	writeFile(t, dir, ".env.example", "")           // ...and comes back
	RunPreCommit()
	if asked != 2 {
		t.Fatalf("expected to be asked again after the diff vanished and returned, asked=%d", asked)
	}
}

func TestSilentPathSpawnsNoGit(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "A=1\n")
	writeFile(t, dir, ".env.example", "A=\n")

	// With git unreachable, a no-drift run must still succeed silently,
	// proving the common path does not shell out to git.
	t.Setenv("PATH", t.TempDir())
	if code := RunPreCommit(); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
	if code := RunPostMerge([]string{"0"}); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
}

func TestPrePush_LocalAnswerSilencesKey(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "MACHINE_ONLY=1\n")
	writeFile(t, dir, ".env.example", "")

	withAnswer(t, prompt.AnswerLocal)
	if code := RunPrePush(); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
	withAnswer(t, prompt.AnswerYes)
	if code := RunPrePush(); code != 0 {
		t.Fatalf("a key marked local-only must not be proposed again, got exit %d", code)
	}
	if strings.Contains(readFile(t, dir, ".env.example"), "MACHINE_ONLY") {
		t.Fatal("local-only key leaked into .env.example")
	}
}

func TestRunManual_WithoutTerminalReportsInsteadOfStayingSilent(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")

	withAnswer(t, prompt.AnswerUnavailable)
	if code := RunManual(); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
	if strings.Contains(readFile(t, dir, ".env.example"), "NEW_KEY") {
		t.Fatal("nothing should be written when there is no terminal to confirm on")
	}
}

func TestPrePush_NotSilencedByNoTerminalPreCommit(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")

	orig := confirmFn
	t.Cleanup(func() { confirmFn = orig })

	confirmFn = func(string, bool) prompt.Answer { return prompt.AnswerUnavailable }
	RunPreCommit() // GUI/no terminal: warns once, snoozes

	prePushAsks := 0
	confirmFn = func(string, bool) prompt.Answer { prePushAsks++; return prompt.AnswerNo }
	if code := RunPrePush(); code != 0 {
		t.Fatalf("a declined pre-push must not block, got %d", code)
	}
	if prePushAsks != 1 {
		t.Fatalf("terminal pre-push must still prompt once despite the no-terminal pre-commit, asked=%d", prePushAsks)
	}
}

func TestPrePush_StillSilencedByExplicitNoAtPreCommit(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")

	orig := confirmFn
	t.Cleanup(func() { confirmFn = orig })

	confirmFn = func(string, bool) prompt.Answer { return prompt.AnswerNo }
	RunPreCommit() // explicit "No" snoozes the whole direction

	prePushAsks := 0
	confirmFn = func(string, bool) prompt.Answer { prePushAsks++; return prompt.AnswerNo }
	RunPrePush()
	if prePushAsks != 0 {
		t.Fatalf("explicit No at pre-commit must still silence pre-push for the same diff, asked=%d", prePushAsks)
	}
}

func TestPreCommit_NoTerminalWarnsOnlyOncePerDiff(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "NEW_KEY=x\n")
	writeFile(t, dir, ".env.example", "")

	asked := 0
	orig := confirmFn
	confirmFn = func(string, bool) prompt.Answer { asked++; return prompt.AnswerUnavailable }
	defer func() { confirmFn = orig }()

	RunPreCommit()
	RunPreCommit() // same diff, same hook, still no terminal: must not re-warn
	if asked != 1 {
		t.Fatalf("expected exactly 1 prompt for an unchanged no-terminal diff, got %d", asked)
	}
}

func TestPostMerge_NotSilencedByNoTerminalPostCheckout(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "")
	writeFile(t, dir, ".env.example", "NEW_KEY=\n")

	orig := confirmFn
	t.Cleanup(func() { confirmFn = orig })

	confirmFn = func(string, bool) prompt.Answer { return prompt.AnswerUnavailable }
	RunPostCheckout([]string{"a", "b", "1"})

	mergeAsks := 0
	confirmFn = func(string, bool) prompt.Answer { mergeAsks++; return prompt.AnswerNo }
	RunPostMerge([]string{"0"})
	if mergeAsks != 1 {
		t.Fatalf("terminal post-merge must still prompt once despite the no-terminal post-checkout, asked=%d", mergeAsks)
	}
}

// captureOutErr runs fn with os.Stdout/os.Stderr redirected and returns
// the exit code plus both captured streams (same os.Pipe approach as
// captureStdout in main_test.go, local to this package).
func captureOutErr(t *testing.T, fn func() int) (code int, out, errOut string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	code = fn()
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = origOut, origErr
	var bOut, bErr bytes.Buffer
	io.Copy(&bOut, rOut)
	io.Copy(&bErr, rErr)
	return code, bOut.String(), bErr.String()
}

func TestRunManual_UnreadableEnvIsNotReportedAsInSync(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env.example", "A=1\n")
	writeFile(t, dir, ".env", "A=1\nBIG="+strings.Repeat("A", 11<<20)+"\n") // >10MB line: scanner error

	withAnswer(t, prompt.AnswerNo)
	code, out, errOut := captureOutErr(t, RunManual)
	if code != 1 {
		t.Fatalf("RunManual returned %d, want 1 when a file cannot be scanned", code)
	}
	if strings.Contains(out, "in sync") {
		t.Fatalf("must not claim 'in sync' when the comparison failed, stdout=%q", out)
	}
	if !strings.Contains(errOut, "example: .env: ") {
		t.Fatalf("stderr must name the wrapped failing file after the prefix (\\\"example: .env: \\\"), got %q", errOut)
	}
}

func TestRunManual_UnreadableExampleIsNotReportedAsInSync(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env", "A=1\n")
	writeFile(t, dir, ".env.example", "A=1\nBIG="+strings.Repeat("A", 11<<20)+"\n")

	withAnswer(t, prompt.AnswerNo)
	code, out, errOut := captureOutErr(t, RunManual)
	if code != 1 {
		t.Fatalf("RunManual returned %d, want 1 when a file cannot be scanned", code)
	}
	if strings.Contains(out, "in sync") {
		t.Fatalf("must not claim 'in sync' when the comparison failed, stdout=%q", out)
	}
	if !strings.Contains(errOut, "example: .env.example: ") {
		t.Fatalf("stderr must name the wrapped failing file after the prefix (\\\"example: .env.example: \\\"), got %q", errOut)
	}
}

func TestRunPreCommit_OversizedEnvLineWarnsAndExitsZero(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env.example", "A=1\n")
	writeFile(t, dir, ".env", "A=1\nBIG="+strings.Repeat("A", 11<<20)+"\n")

	withAnswer(t, prompt.AnswerNo)
	code, _, errOut := captureOutErr(t, RunPreCommit)
	if code != 0 {
		t.Fatalf("hook mode must keep exit 0, got %d", code)
	}
	if !strings.Contains(errOut, "internal error (ignored)") {
		t.Fatalf("hook mode must warn via silentFail, stderr=%q", errOut)
	}
}

func TestRunPreCommit_DoesNotStageUnrelatedUnstagedExampleEdits(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env.example", "A=\n")
	writeFile(t, dir, ".env", "A=1\nNEWK=v\n")
	git(t, dir, "add", ".env.example")
	git(t, dir, "commit", "-qm", "init")

	writeFile(t, dir, ".env.example", "A=\n# WIP\n") // unrelated unstaged edit
	writeFile(t, dir, "f", "x\n")
	git(t, dir, "add", "f")

	withAnswer(t, prompt.AnswerYes)
	code, _, errOut := captureOutErr(t, RunPreCommit)
	if code != 0 {
		t.Fatalf("RunPreCommit returned %d, want 0", code)
	}
	git(t, dir, "commit", "-qm", "wip")

	head := git(t, dir, "show", "HEAD:.env.example")
	if strings.Contains(head, "WIP") || strings.Contains(head, "NEWK") {
		t.Fatalf("HEAD:.env.example must contain neither WIP nor NEWK, got %q", head)
	}
	work := readFile(t, dir, ".env.example")
	if !strings.Contains(work, "WIP") || !strings.Contains(work, "NEWK=") {
		t.Fatalf("working .env.example must have both WIP and NEWK=, got %q", work)
	}
	if !strings.Contains(errOut, "NOT staged") {
		t.Fatalf("errOut must mention NOT staged, got %q", errOut)
	}
}

func TestRunPreCommit_StagesSymlinkTargetInsideRepo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git symlink handling on Windows depends on core.symlinks")
	}
	dir := newRepo(t)
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "docs/env.template", "A=\n")
	if err := os.Symlink("docs/env.template", filepath.Join(dir, ".env.example")); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
	writeFile(t, dir, ".env", "NEWK=v\n")
	git(t, dir, "add", ".env.example", "docs")
	git(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "f", "x\n")
	git(t, dir, "add", "f")

	withAnswer(t, prompt.AnswerYes)
	code, _, errOut := captureOutErr(t, RunPreCommit)
	if code != 0 {
		t.Fatalf("RunPreCommit returned %d, want 0", code)
	}
	git(t, dir, "commit", "-qm", "c")

	head := git(t, dir, "show", "HEAD:docs/env.template")
	if !strings.Contains(head, "NEWK=") {
		t.Fatalf("HEAD:docs/env.template must contain NEWK=, got %q", head)
	}
	tree := git(t, dir, "ls-tree", "HEAD", ".env.example")
	if !strings.HasPrefix(tree, "120000") {
		t.Fatalf(".env.example must still be a symlink (mode 120000) in HEAD, got %q", tree)
	}
	if !strings.Contains(errOut, "included it in this commit") {
		t.Fatalf("errOut must say the fix was included in this commit, got %q", errOut)
	}
}

func TestRunPreCommit_SymlinkOutsideRepoIsNotStaged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git symlink handling on Windows depends on core.symlinks")
	}
	dir := newRepo(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "env.template")
	if err := os.WriteFile(outsideFile, []byte("A=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(dir, ".env.example")); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
	writeFile(t, dir, ".env", "NEWK=v\n")
	git(t, dir, "add", ".env.example")
	git(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "f", "x\n")
	git(t, dir, "add", "f")

	withAnswer(t, prompt.AnswerYes)
	code, _, errOut := captureOutErr(t, RunPreCommit)
	if code != 0 {
		t.Fatalf("RunPreCommit returned %d, want 0", code)
	}
	if !strings.Contains(errOut, "outside this repository") {
		t.Fatalf("errOut must mention outside this repository, got %q", errOut)
	}
	data, err := os.ReadFile(outsideFile)
	if err != nil || !strings.Contains(string(data), "NEWK=") {
		t.Fatalf("outside target file must have received NEWK=, got %q err=%v", data, err)
	}
	staged := strings.TrimSpace(git(t, dir, "diff", "--cached", "--name-only"))
	if staged != "f" {
		t.Fatalf("only f must be staged, got %q", staged)
	}
}

func TestRunPreCommit_StagesFixWhenExampleFullyStaged(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, ".env.example", "A=\n")
	writeFile(t, dir, ".env", "A=1\nNEWK=v\n")
	git(t, dir, "add", ".env.example")
	git(t, dir, "commit", "-qm", "init")

	writeFile(t, dir, ".env.example", "A=\nB=\n") // edit, then stage it fully
	git(t, dir, "add", ".env.example")
	writeFile(t, dir, "f", "x\n")
	git(t, dir, "add", "f")

	withAnswer(t, prompt.AnswerYes)
	code, _, errOut := captureOutErr(t, RunPreCommit)
	if code != 0 {
		t.Fatalf("RunPreCommit returned %d, want 0", code)
	}
	git(t, dir, "commit", "-qm", "c")

	head := git(t, dir, "show", "HEAD:.env.example")
	if !strings.Contains(head, "NEWK=") {
		t.Fatalf("fully staged .env.example must still get the fix committed, HEAD:.env.example=%q", head)
	}
	if !strings.Contains(errOut, "included it in this commit") {
		t.Fatalf("errOut must say included in this commit, got %q", errOut)
	}
}
