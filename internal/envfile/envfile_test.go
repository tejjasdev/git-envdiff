package envfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func TestParseKeys(t *testing.T) {
	bs := string(rune(92)) // the single backslash char, built by concatenation not retyped
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "plain",
			content: "A=1\nB=2\n",
			want:    []string{"A", "B"},
		},
		{
			name:    "export prefix",
			content: "export FOO=bar\nBAZ=qux\n",
			want:    []string{"FOO", "BAZ"},
		},
		{
			name:    "comments and blanks skipped",
			content: "# a comment\n\nA=1\n   \n# B=2 (commented out, should not count)\nC=3\n",
			want:    []string{"A", "C"},
		},
		{
			name:    "duplicate key keeps first occurrence only",
			content: "A=1\nA=2\n",
			want:    []string{"A"},
		},
		{
			name:    "quoted single line value with equals inside",
			content: `A="x=y=z"` + "\nB=2\n",
			want:    []string{"A", "B"},
		},
		{
			name: "multiline quoted value is skipped, not mistaken for new keys",
			content: "PRIVATE_KEY=\"-----BEGIN KEY-----\n" +
				"AAAABBBBCCCC====\n" +
				"DDDDEEEEFFFF====\n" +
				"-----END KEY-----\"\n" +
				"NEXT=1\n",
			want: []string{"PRIVATE_KEY", "NEXT"},
		},
		{
			name:    "BOM on first line is stripped",
			content: "\ufeffA=1\nB=2\n",
			want:    []string{"A", "B"},
		},
		{
			name:    "no trailing newline",
			content: "A=1\nB=2",
			want:    []string{"A", "B"},
		},
		{
			name:    "empty file",
			content: "",
			want:    nil,
		},
		{
			name:    "odd number of escaped quotes does not start a multi-line value",
			content: "A=1\n" + `MSG="He said \"hi"` + "\nLATER=1\n",
			want:    []string{"A", "MSG", "LATER"},
		},
		{
			name: "escaped quote inside a real multi-line value does not close it early",
			content: "PK=\"-----BEGIN\n" +
				`AAAA\"BBB` + "\n" +
				"FAKE=notakey\n" +
				`-----END"` + "\n" +
				"NEXT=1\n",
			want: []string{"PK", "NEXT"},
		},
		{
			name:    "even number of escaped quotes, closed value",
			content: `J="{\"a\":1}"` + "\nNEXT=1\n",
			want:    []string{"J", "NEXT"},
		},
		{
			name:    "escaped backslash before closing quote",
			content: `P="C:\\dir\\"` + "\nNEXT=1\n",
			want:    []string{"P", "NEXT"},
		},
		{
			name:    "multiline value without escapes still skipped",
			content: "PK=\"-----BEGIN\nAAAA====\n-----END\"\nNEXT=1\n",
			want:    []string{"PK", "NEXT"},
		},
		{
			name:    "unclosed double quote does not swallow the rest of the file",
			content: "A=1\nPASSWORD=\"abc\nK1=1\nK2=2\nK3=3\n",
			want:    []string{"A", "PASSWORD", "K1", "K2", "K3"},
		},
		{
			name:    "unclosed single quote does not swallow the rest of the file",
			content: "A=1\nB='abc\nC=1\n",
			want:    []string{"A", "B", "C"},
		},
		{
			name:    "unclosed quote on the first line",
			content: "B=\"x\nC=1\nD=2\n",
			want:    []string{"B", "C", "D"},
		},
		{
			name:    "windows path ending in backslash before the closing quote",
			content: "A=1\nDIR=\"C:" + bs + "logs" + bs + "\"\nLATER=1\n",
			want:    []string{"A", "DIR", "LATER"},
		},
		{
			name:    "BOM on first line with unclosed quote",
			content: "\ufeffA=\"x\nB=1\n",
			want:    []string{"A", "B"},
		},
		{
			name:    "CRLF line endings with unclosed quote",
			content: "A=\"x\r\nB=1\r\n",
			want:    []string{"A", "B"},
		},
		{
			name:    "duplicate key whose second occurrence has an unclosed quote",
			content: "A=1\nA=\"x\nB=1\n",
			want:    []string{"A", "B"},
		},
		{
			name:    "unclosed quote on the very last line",
			content: "A=1\nB=\"x",
			want:    []string{"A", "B"},
		},
		{
			// Single quotes have no escapes, so the backslash in 'it\'s'
			// does not escape the middle quote: the value opens a
			// "multi-line" that is never closed, which the GE-13 recovery
			// pins back to single-line — so NEXT is still found.
			// Expected keys derived by running the fixed scanner.
			name:    "single-quoted value containing a backslash-quote",
			content: "X='it" + bs + "'s'\nNEXT=1\n",
			want:    []string{"X", "NEXT"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := write(t, dir, ".env", tc.content)

			got, err := ParseKeys(path)
			if err != nil {
				t.Fatalf("ParseKeys: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestDiff(t *testing.T) {
	missing := Diff([]string{"A", "B", "C"}, []string{"B"})
	want := []string{"A", "C"}
	if len(missing) != len(want) || missing[0] != want[0] || missing[1] != want[1] {
		t.Fatalf("got %v, want %v", missing, want)
	}
}

func TestAppendBlankKeys_NeverWritesAValue(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, ".env.example", "EXISTING=\n")

	if err := AppendBlankKeys(path, []string{"NEW_API_KEY", "OTHER"}); err != nil {
		t.Fatalf("AppendBlankKeys: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasSuffix(line, "=") {
			t.Fatalf("expected every appended line to be exactly KEY=, got %q", line)
		}
	}
	if !strings.Contains(content, "NEW_API_KEY=\n") {
		t.Fatalf("expected NEW_API_KEY= to be appended, got:\n%s", content)
	}
}

// TestLeak_SecretValueNeverReachesExampleFile is the privacy contract test:
// given a .env containing a realistic-looking secret, running the normal
// "detect missing key, then append it to .env.example" flow must never let
// that secret's value appear anywhere in .env.example.
func TestLeak_SecretValueNeverReachesExampleFile(t *testing.T) {
	dir := t.TempDir()
	secret := "sk_live_totally_real_secret_value_12345"
	envPath := write(t, dir, ".env", "NEW_API_KEY="+secret+"\nEXISTING=1\n")
	examplePath := write(t, dir, ".env.example", "EXISTING=\n")

	envKeys, err := ParseKeys(envPath)
	if err != nil {
		t.Fatal(err)
	}
	exampleKeys, err := ParseKeys(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	missing := Diff(envKeys, exampleKeys)

	if err := AppendBlankKeys(examplePath, missing); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("secret value leaked into .env.example:\n%s", data)
	}
	if !strings.Contains(string(data), "NEW_API_KEY=\n") {
		t.Fatalf("expected NEW_API_KEY= (blank) in .env.example, got:\n%s", data)
	}
}

func TestAppendKeyValues_PreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, ".env", "A=1\r\nB=2\r\n")

	if err := AppendKeyValues(path, []string{"C"}, map[string]string{"C": "3"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "C=3\r\n") {
		t.Fatalf("expected CRLF-terminated appended line, got: %q", data)
	}
}

func TestAppendKeyValues_AddsLeadingNewlineIfFileDidNotEndWithOne(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, ".env", "A=1") // no trailing newline

	if err := AppendKeyValues(path, []string{"B"}, map[string]string{"B": "2"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "A=1\nB=2\n"
	if string(data) != want {
		t.Fatalf("got %q, want %q", data, want)
	}
}

func TestParseKeyValues_OnTemplateOnly(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, ".env.example", "A=\nB=default-value\nC=\"quoted value\"\n")

	values, err := ParseKeyValues(path, []string{"A", "B", "C", "MISSING"})
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "" || values["B"] != "default-value" || values["C"] != "\"quoted value\"" {
		t.Fatalf("unexpected values: %+v", values)
	}
	if _, ok := values["MISSING"]; ok {
		t.Fatalf("did not expect a value for a key not in the file")
	}
}

func TestCreateFromTemplate_RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	envPath := write(t, dir, ".env", "EXISTING=secret\n")
	templatePath := write(t, dir, ".env.example", "EXISTING=\nNEW=\n")

	err := CreateFromTemplate(envPath, templatePath)
	if err != ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	data, _ := os.ReadFile(envPath)
	if string(data) != "EXISTING=secret\n" {
		t.Fatalf("existing .env was modified: %q", data)
	}
}

func TestCreateFromTemplate_CopiesTemplateValues(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	templatePath := write(t, dir, ".env.example", "A=\nB=default\n")

	if err := CreateFromTemplate(envPath, templatePath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "A=\nB=default\n"
	if string(data) != want {
		t.Fatalf("got %q, want %q", data, want)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	if Exists(filepath.Join(dir, "nope")) {
		t.Fatal("expected false for a nonexistent file")
	}
	path := write(t, dir, "here", "x")
	if !Exists(path) {
		t.Fatal("expected true for an existing file")
	}
	if Exists(dir) {
		t.Fatal("expected false for a directory")
	}
}

func TestScan_KeysOnly_NeverBuildsValues(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, ".env", "SECRET=hunter2\nPLAIN=abc\nQUOTED=\"shh\"\n")
	rows, err := scan(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for _, r := range rows {
		if r.value != "" {
			t.Errorf("key %q: value was built and stored as %q even though wantValues=false", r.key, r.value)
		}
	}
}

func TestScan_WantValues_StillReadsThem(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, ".env.example", "SECRET=hunter2\nQUOTED=\"shh\"\n")
	rows, err := scan(path, true)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.key] = r.value
	}
	if got["SECRET"] != "hunter2" || got["QUOTED"] != "\"shh\"" {
		t.Fatalf("unexpected values: %+v", got)
	}
}

func TestCreateFromTemplate_PreservesQuotesAndSpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	template := "A=1\nTOKEN=\"abc#def\"\nRAW='$HOME/x'\nPAD=\"  keep  \"\nINLINE=foo # a comment\n"
	templatePath := write(t, dir, ".env.example", template)
	envPath := filepath.Join(dir, ".env")
	if err := CreateFromTemplate(envPath, templatePath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != template {
		t.Fatalf("got %q, want template copied verbatim %q", data, template)
	}
}

func TestAppendKeyValues_PreservesRawValueText(t *testing.T) {
	dir := t.TempDir()
	envPath := write(t, dir, ".env", "A=1\n")
	templatePath := write(t, dir, ".env.example", "A=1\nTOKEN=\"abc#def\"\nRAW='$HOME/x'\nPAD=\"  keep  \"\nINLINE=foo # a comment\n")
	order := []string{"TOKEN", "RAW", "PAD", "INLINE"}
	values, err := ParseKeyValues(templatePath, order)
	if err != nil {
		t.Fatal(err)
	}
	if err := AppendKeyValues(envPath, order, values); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "A=1\nTOKEN=\"abc#def\"\nRAW='$HOME/x'\nPAD=\"  keep  \"\nINLINE=foo # a comment\n"
	if string(data) != want {
		t.Fatalf("got %q, want %q", data, want)
	}
}

func TestAppendBlankKeys_FollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := write(t, dir, "real.example", "A=1\n")
	if err := os.Chmod(real, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(real)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".env.example")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
	if err := AppendBlankKeys(link, []string{"NEW"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced by a regular file: mode=%v err=%v", fi, err)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "A=1\nNEW=\n" {
		t.Fatalf("target not updated: %q", data)
	}
	after, err := os.Stat(real)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode() != before.Mode() {
		t.Fatalf("target mode not preserved: before=%v after=%v", before.Mode(), after.Mode())
	}
}

func TestAppendKeyValues_FollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := write(t, dir, "real.env", "A=1\n")
	link := filepath.Join(dir, ".env")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
	if err := AppendKeyValues(link, []string{"NEW"}, map[string]string{"NEW": "x"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced by a regular file: mode=%v err=%v", fi, err)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "A=1\nNEW=x\n" {
		t.Fatalf("target not updated: %q", data)
	}
}

func TestAppendBlankKeys_DanglingSymlinkKeepsOldBehaviour(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, ".env.example")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
	if err := AppendBlankKeys(link, []string{"NEW"}); err != nil {
		t.Fatal(err)
	}
	// Old behaviour for a dangling link: the link itself is replaced by a
	// regular file containing just the new line.
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dangling link should be replaced as before: mode=%v err=%v", fi, err)
	}
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "NEW=\n" {
		t.Fatalf("got %q, want %q", data, "NEW=\n")
	}
}

func TestUnescapedQuotes(t *testing.T) {
	bs := string(rune(92)) // the single backslash char
	cases := []struct {
		s    string
		q    byte
		want int
	}{
		{`a"b`, '"', 1},
		{`a\"b`, '"', 0},
		{`"`, '"', 1},
		{`a\\"b`, '"', 1},             // escaped backslash, then an unescaped quote
		{"'it" + bs + "'s'", '\'', 3}, // single quotes have no escapes: BS does not escape, all 3 quotes count
		{`""`, '\'', 0},
	}
	for _, tc := range cases {
		if got := unescapedQuotes(tc.s, tc.q); got != tc.want {
			t.Errorf("unescapedQuotes(%q, %q) = %d, want %d", tc.s, tc.q, got, tc.want)
		}
	}
}

func TestScan_ManyStrayQuotesTerminates(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 2001; i++ {
		fmt.Fprintf(&sb, "K%d=\"x\n", i) // every line opens a quote that is never closed
	}
	path := write(t, t.TempDir(), ".env", sb.String())

	start := time.Now()
	if _, err := ParseKeys(path); err != nil {
		t.Fatalf("ParseKeys: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("scanning 2001 stray-quote lines took %v, want well under 2s", d)
	}
}
