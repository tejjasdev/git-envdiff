package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// whatever was written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestRun_Version(t *testing.T) {
	out := captureStdout(t, func() {
		if code := run([]string{"--version"}); code != 0 {
			t.Errorf("got exit %d, want 0", code)
		}
	})
	if !strings.Contains(out, "git-envdiff") {
		t.Fatalf("expected version output to mention git-envdiff, got: %q", out)
	}
}

func TestRun_Help(t *testing.T) {
	out := captureStdout(t, func() {
		if code := run([]string{"--help"}); code != 0 {
			t.Errorf("got exit %d, want 0", code)
		}
	})
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("expected help text, got: %q", out)
	}
}

func TestRun_UnknownCommandReturnsNonZero(t *testing.T) {
	// Discard stderr/stdout noise; we only care about the exit code here.
	origErr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = origErr; w.Close() }()

	captureStdout(t, func() {
		if code := run([]string{"this-is-not-a-command"}); code == 0 {
			t.Error("expected a non-zero exit code for an unknown command")
		}
	})
}

func TestRun_HookWithNoEventIsANoOp(t *testing.T) {
	if code := run([]string{"hook"}); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
}

func TestRun_HookWithUnknownEventIsANoOp(t *testing.T) {
	if code := run([]string{"hook", "not-a-real-event"}); code != 0 {
		t.Fatalf("got %d, want 0", code)
	}
}

func TestRun_NoArgsInEmptyRepoIsANoOp(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "T")

	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	out := captureStdout(t, func() {
		if code := run(nil); code != 0 {
			t.Errorf("got %d, want 0", code)
		}
	})
	if !strings.Contains(out, "Nothing to check") {
		t.Fatalf("expected the no-.env/.env.example message, got: %q", out)
	}
}

func TestHasFlag(t *testing.T) {
	if !hasFlag([]string{"install", "--repo"}, "--repo") {
		t.Fatal("expected --repo to be found")
	}
	if hasFlag([]string{"install"}, "--repo") {
		t.Fatal("did not expect --repo to be found")
	}
}
