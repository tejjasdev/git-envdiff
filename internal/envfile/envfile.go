// Package envfile implements the only file handling git-envdiff needs, and
// it is deliberately narrow:
//
//   - ParseKeys reads a .env-style file and returns only the variable
//     NAMES it defines. It never returns, logs, or retains a value.
//   - ParseKeyValues does return values, and its doc comment explains why
//     that is still safe: it must only ever be called on .env.example, a
//     file already committed to version control, never on .env.
//   - AppendBlankKeys and AppendKeyValues only ever append new lines to
//     the end of a file. Nothing already in the file is ever reordered,
//     rewritten, or deleted.
//
// This is intentionally not a general-purpose dotenv parser: it only needs
// to be correct about *which lines introduce a new KEY=*, not about the
// full shell-quoting semantics of arbitrary values.
package envfile

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// keyLineRE matches a line that assigns a value to an environment
// variable, with an optional "export " prefix.
//
//	group 1: "export " (optional, discarded)
//	group 2: the variable name (the only thing ParseKeys ever returns)
//	group 3: the raw, unparsed remainder after "=" (only used by
//	         ParseKeyValues, and only ever on .env.example)
var keyLineRE = regexp.MustCompile(`^\s*(export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$`)

// ErrAlreadyExists is returned by CreateFromTemplate when the destination
// file already exists, since overwriting an existing .env is never safe.
var ErrAlreadyExists = errors.New("file already exists")

// assignment is one KEY=value line as found while scanning a file.
type assignment struct {
	key   string
	value string // empty unless scan was called with wantValues=true; then only for single-line, non-multiline values
}

// scan walks path line by line and returns the first occurrence of each
// assigned key, in the order it first appears. Later re-assignments of an
// already-seen key are ignored: git-envdiff only cares whether a key is
// *defined at all*, not its final value.
//
// Recognized syntax:
//   - "KEY=value" and "export KEY=value"
//   - "#" comments and blank lines are skipped
//   - "# KEY=value" (a commented-out assignment) does NOT count as defined
//   - a value that opens a quote (' or ") without closing it on the same
//     line is treated as the start of a multi-line value (e.g. a
//     multi-line PEM key or base64 blob); every line up to and including
//     the one that closes the quote is skipped entirely, and is never
//     scanned for a new key. This is what keeps a base64 line that
//     happens to contain "=" from being mistaken for a new variable.
//
// wantValues controls whether a parsed value is kept. ParseKeys passes
// false, so for key-only scans (every scan of a real .env) no value is
// stored in the returned rows. The raw line is
// still read into memory while it is being parsed (the scanner must read
// it to find the key and track quotes), but nothing derived from the value
// is kept after the call returns. ParseKeyValues and CreateFromTemplate
// pass true and must only ever be given .env.example-style templates.
func scan(path string, wantValues bool) ([]assignment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []assignment
	seen := make(map[string]bool)

	sc := bufio.NewScanner(f)
	// Raise the scanner's line-length ceiling: real .env files sometimes
	// contain long base64-encoded values (certificates, keys) on a single
	// line, and the default 64KB bufio limit would otherwise truncate
	// scanning silently.
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	inMultiline := false
	var quoteChar byte
	first := true

	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff") // strip a UTF-8 BOM, if present
			first = false
		}

		if inMultiline {
			if strings.IndexByte(line, quoteChar) >= 0 {
				inMultiline = false
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		m := keyLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[2]
		rawValue := strings.TrimSpace(m[3])

		multilineOpen := false
		if len(rawValue) > 0 && (rawValue[0] == '"' || rawValue[0] == '\'') {
			q := rawValue[0]
			rest := rawValue[1:]
			// An even number of matching quote characters after the
			// opening one means it was never closed on this line (the
			// opening quote is the odd one out), so the value continues
			// on following lines.
			//
			// Known limitation: this does not understand backslash-escaped
			// quotes inside a value. That's an accepted simplification —
			// see the package doc comment.
			if strings.Count(rest, string(q))%2 == 0 {
				multilineOpen = true
				quoteChar = q
				inMultiline = true
			}
		}

		if seen[key] {
			continue
		}
		seen[key] = true

		val := ""
		if wantValues && !multilineOpen {
			// Copy the template's text after "=" verbatim (rawValue is
			// already TrimSpace(m[3])): stripping quotes or a trailing
			// inline comment could change what dotenv parsers see.
			val = rawValue
		}
		rows = append(rows, assignment{key: key, value: val})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// ParseKeys returns the variable names path defines, in first-seen order.
// Values are never returned or retained: scan runs with wantValues=false,
// so no value is stored in its result. (Each line is necessarily read
// while parsing, but nothing derived from a value is kept afterwards.)
func ParseKeys(path string) ([]string, error) {
	rows, err := scan(path, false)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	return keys, nil
}

// ParseKeyValues returns the values assigned to the given keys in path.
//
// Unlike ParseKeys, this DOES read and return value text. Callers MUST
// only ever pass a .env.example-style template path here — a file whose
// contents are already committed to version control — and never a real
// .env. It exists for exactly one purpose: copying a template's own
// (already-public) default values into a freshly created or synced .env.
// Values are the raw text after "=", copied verbatim (quotes and inline
// comments preserved).
//
// A key whose value spans multiple lines (an unclosed quote) is reported
// as an empty string rather than an incorrect partial value.
func ParseKeyValues(path string, wanted []string) (map[string]string, error) {
	rows, err := scan(path, true)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(wanted))
	for _, k := range wanted {
		want[k] = true
	}
	out := make(map[string]string, len(wanted))
	for _, r := range rows {
		if want[r.key] {
			out[r.key] = r.value
		}
	}
	return out, nil
}

// Exists reports whether path exists and is readable as a regular file.
func Exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Diff returns the keys present in fromKeys but absent from toKeys,
// preserving fromKeys' original order.
func Diff(fromKeys, toKeys []string) []string {
	toSet := make(map[string]bool, len(toKeys))
	for _, k := range toKeys {
		toSet[k] = true
	}
	var missing []string
	for _, k := range fromKeys {
		if !toSet[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

// AppendBlankKeys appends a "KEY=" line (the name only — never a value)
// to path, for each key in keys, in order.
//
// This is the ONLY function git-envdiff ever uses to add a key to
// .env.example, and it is what guarantees a real secret value from .env
// can never end up there: there is no code path that writes anything
// after the "=" here.
func AppendBlankKeys(path string, keys []string) error {
	values := make(map[string]string, len(keys))
	for _, k := range keys {
		values[k] = ""
	}
	return appendKeyValues(path, keys, values)
}

// AppendKeyValues appends a "KEY=value" line to path for each key in
// order, using the corresponding entry in values (a missing entry is
// written as an empty value). Used on the pull side, where the values
// being written come from ParseKeyValues(.env.example, ...) — i.e.
// already-public template defaults — never from a real .env.
func AppendKeyValues(path string, order []string, values map[string]string) error {
	return appendKeyValues(path, order, values)
}

func appendKeyValues(path string, order []string, values map[string]string) error {
	eol := "\n"
	var needsLeadingNewline bool

	if existing, err := os.ReadFile(path); err == nil {
		if strings.Contains(string(existing), "\r\n") {
			eol = "\r\n"
		}
		if len(existing) > 0 && existing[len(existing)-1] != '\n' {
			needsLeadingNewline = true
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	var b strings.Builder
	if needsLeadingNewline {
		b.WriteString(eol)
	}
	for _, k := range order {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(values[k])
		b.WriteString(eol)
	}

	return atomicAppend(path, []byte(b.String()))
}

// atomicAppend writes path's existing content plus suffix to a temp file
// in the same directory, then renames it into place. A crash or power
// loss mid-write can therefore never leave a half-written .env or
// .env.example behind — the rename is the only moment the visible file
// changes, and renames within one directory are atomic on every platform
// git-envdiff targets.
func atomicAppend(path string, suffix []byte) error {
	// Resolve symlinks first: the rename below must replace the real file,
	// not the link — otherwise a linked .env would silently become a
	// regular file and its target would stop receiving updates. If the
	// path does not exist yet (or the link dangles), keep the original
	// path and behave exactly as before.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	dir := filepath.Dir(path)

	var existing []byte
	mode := os.FileMode(0o644)
	if data, err := os.ReadFile(path); err == nil {
		existing = data
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode()
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".git-envdiff-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(existing); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(suffix); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// CreateFromTemplate creates a brand new file at path containing every
// KEY=value pair found in templatePath, copying the template's own
// (already-public) values verbatim.
//
// It refuses outright if path already exists (ErrAlreadyExists), so this
// can never be used to overwrite a real .env that already has real
// secrets in it.
func CreateFromTemplate(path, templatePath string) error {
	if Exists(path) {
		return ErrAlreadyExists
	}
	rows, err := scan(templatePath, true)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, r := range rows {
		if _, err := w.WriteString(r.key + "=" + r.value + "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}
