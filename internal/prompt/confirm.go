// Package prompt implements the one piece of interactive UI git-envdiff
// has: a yes/no(/local) confirmation.
//
// It always talks to the controlling terminal directly (via the
// platform-specific openTTY in confirm_unix.go / confirm_windows.go),
// never a hook's own stdin/stdout — those are frequently redirected,
// piped, or simply absent when Git invokes a hook (pre-push receives ref
// data on stdin; many GUI clients and IDEs run hooks with no terminal at
// all). And it degrades silently: if no terminal is reachable, it returns
// AnswerUnavailable immediately rather than blocking anything.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Answer is the user's response to a confirmation prompt.
type Answer int

const (
	// AnswerNo means "not now" — the caller is expected to remember this
	// exact diff and not ask about it again until it changes.
	AnswerNo Answer = iota
	// AnswerYes means "make the change".
	AnswerYes
	// AnswerLocal means "never ask about this specific key again". Only
	// offered where the caller passes allowLocal.
	AnswerLocal
	// AnswerUnavailable means no terminal was reachable to prompt on. The
	// caller should treat this like AnswerNo for state purposes, but print
	// a one-line warning instead of having attempted to prompt.
	AnswerUnavailable
)

// Confirm shows message followed by " [y/N]" (or " [y/N/l]" if allowLocal)
// on the controlling terminal and returns the user's answer.
func Confirm(message string, allowLocal bool) Answer {
	// A controlling terminal can exist even when nobody is watching it —
	// e.g. an editor launched from a shell, which then runs Git with its
	// output piped. Prompting there would hang the commit on a question
	// nobody can see. In a real terminal session Git hands hooks the
	// terminal as stderr, so require that too.
	if !stderrIsTerminal() {
		return AnswerUnavailable
	}
	in, out, ok := openTTY()
	if !ok {
		return AnswerUnavailable
	}
	defer in.Close()
	defer out.Close()

	suffix := " [y/N]: "
	if allowLocal {
		suffix = " [y/N/l]: "
	}
	fmt.Fprint(out, message+suffix)

	reader := bufio.NewReader(in)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))

	switch line {
	case "y", "yes":
		return AnswerYes
	case "l", "local":
		if allowLocal {
			return AnswerLocal
		}
		return AnswerNo
	default:
		return AnswerNo
	}
}

// stderrIsTerminal reports whether stderr is a character device other than
// the null device (i.e. a terminal/console, not a pipe, file, or /dev/null).
func stderrIsTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(fi, null) {
		return false
	}
	return true
}
