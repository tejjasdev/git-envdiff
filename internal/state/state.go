// Package state stores git-envdiff's small amount of per-repository
// memory: which differences the user has already dismissed ("snoozed"),
// which keys they've explicitly marked as intentionally local-only, and
// whether the tool has been switched off for this repository.
//
// All of it lives as plain text files under the repository's own .git
// directory — never in a tracked file, and never shared with anyone else
// who clones the repo. None of these files ever contains an environment
// variable's *value*.
package state

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	snoozeFileName    = "git-envdiff-snooze"
	localFileName     = "git-envdiff-local"
	disabledFileName  = "git-envdiff-disabled"
	disabledFileNotes = "git-envdiff is disabled in this repository.\nRemove this file, or run `git-envdiff on`, to re-enable.\n"
)

// Fingerprint returns a short, stable identifier for "this exact set of
// missing keys, in this direction". It is used only to recognize when a
// diff is unchanged since the user last dismissed it — it identifies
// neither the repository nor the user, and never incorporates a value.
func Fingerprint(direction string, keys []string) string {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(direction))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(sorted, ",")))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func snoozePath(gitDir string) string { return filepath.Join(gitDir, snoozeFileName) }
func localPath(gitDir string) string  { return filepath.Join(gitDir, localFileName) }

func snoozeLine(direction, fingerprint string) string { return direction + " " + fingerprint }

// IsSnoozed reports whether this exact diff (direction + fingerprint) was
// previously declined and has been continuously present since.
func IsSnoozed(gitDir, direction, fingerprint string) bool {
	lines, err := readLines(snoozePath(gitDir))
	if err != nil {
		return false
	}
	want := snoozeLine(direction, fingerprint)
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// Snooze records that this diff was declined, so it is not asked about
// again until it changes (or disappears — see ClearSnoozed).
func Snooze(gitDir, direction, fingerprint string) error {
	return appendLineIfMissing(snoozePath(gitDir), snoozeLine(direction, fingerprint))
}

// ClearSnoozed forgets every snoozed diff for one direction. Hooks call
// it whenever they find NO difference in that direction, so a snooze only
// ever applies while the exact diff it was made for keeps existing: if
// the diff goes away and later comes back, you get asked again.
//
// It is a single failed os.Open on the common path (no snooze file).
func ClearSnoozed(gitDir, direction string) error {
	path := snoozePath(gitDir)
	lines, err := readLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	prefix := direction + " "
	kept := lines[:0:0]
	for _, l := range lines {
		if !strings.HasPrefix(l, prefix) {
			kept = append(kept, l)
		}
	}
	if len(kept) == len(lines) {
		return nil
	}
	if len(kept) == 0 {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}

// LocalKeys returns the set of variable names the user has marked as
// intentionally local-only (never to be proposed for .env.example).
func LocalKeys(gitDir string) (map[string]bool, error) {
	lines, err := readLines(localPath(gitDir))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	set := make(map[string]bool, len(lines))
	for _, l := range lines {
		set[l] = true
	}
	return set, nil
}

// MarkLocal records that key should never be proposed for .env.example.
func MarkLocal(gitDir, key string) error {
	return appendLineIfMissing(localPath(gitDir), key)
}

// SetDisabled turns git-envdiff off (true) or back on (false) for the
// repository whose .git directory is gitDir.
func SetDisabled(gitDir string, disabled bool) error {
	path := filepath.Join(gitDir, disabledFileName)
	if disabled {
		return os.WriteFile(path, []byte(disabledFileNotes), 0o644)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// IsDisabled reports whether git-envdiff has been turned off here.
func IsDisabled(gitDir string) bool {
	_, err := os.Stat(filepath.Join(gitDir, disabledFileName))
	return err == nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, sc.Err()
}

func appendLineIfMissing(path, line string) error {
	existing, err := readLines(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, l := range existing {
		if l == line {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}
