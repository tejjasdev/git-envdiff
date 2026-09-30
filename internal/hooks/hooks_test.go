package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
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
