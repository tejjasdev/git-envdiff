package prompt

import (
	"os"
	"testing"
)

// When stderr is not a terminal (a pipe here, exactly what a GUI Git
// client or an editor provides), Confirm must not prompt or block.
func TestConfirm_NoPromptWhenStderrIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	if got := Confirm("question?", true); got != AnswerUnavailable {
		t.Fatalf("got %v, want AnswerUnavailable", got)
	}
}
