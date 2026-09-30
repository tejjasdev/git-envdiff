//go:build !windows

package prompt

import "os"

// openTTY opens the controlling terminal for reading and writing,
// independent of the process's own stdin/stdout/stderr. Returns ok=false
// if there is no controlling terminal (CI, most GUI Git clients, many
// IDE-integrated terminals) — the caller must not block in that case.
func openTTY() (in *os.File, out *os.File, ok bool) {
	inF, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, false
	}
	outF, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		inF.Close()
		return nil, nil, false
	}
	return inF, outF, true
}
