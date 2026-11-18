package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestHasUnstagedChanges(t *testing.T) {
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

	// untracked file: not "unstaged changes" -> false
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := HasUnstagedChanges("new.txt"); err != nil || got {
		t.Fatalf("untracked: got %v, %v; want false, nil", got, err)
	}

	// clean tracked file -> false
	git(t, dir, "add", "new.txt")
	git(t, dir, "commit", "-qm", "init")
	if got, err := HasUnstagedChanges("new.txt"); err != nil || got {
		t.Fatalf("clean: got %v, %v; want false, nil", got, err)
	}

	// modified tracked file -> true
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := HasUnstagedChanges("new.txt"); err != nil || !got {
		t.Fatalf("modified: got %v, %v; want true, nil", got, err)
	}

	// outside any repository -> error
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := HasUnstagedChanges("new.txt"); err == nil {
		t.Fatal("outside a repo: want an error, got nil")
	}
}
