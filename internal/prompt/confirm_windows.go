//go:build windows

package prompt

import "os"

// openTTY opens the process's console directly via the CONIN$/CONOUT$
// device names, independent of redirected stdin/stdout.
//
// NOTE: this path is implemented but not yet verified on real Windows
// hardware (see README's "what's not yet verified" section) — please
// report Windows terminal behavior via an issue if something looks off.
func openTTY() (in *os.File, out *os.File, ok bool) {
	inF, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, false
	}
	outF, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		inF.Close()
		return nil, nil, false
	}
	return inF, outF, true
}
