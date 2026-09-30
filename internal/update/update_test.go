package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withFakeGitHub points the package at an httptest server instead of the
// real github.com / api.github.com, and restores the real values after
// the test. No test in this file ever makes a real network request.
func withFakeGitHub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	origAPI, origRelease, origOwner, origName := apiBaseURL, releaseBaseURL, RepoOwner, RepoName
	apiBaseURL = srv.URL
	releaseBaseURL = srv.URL
	RepoOwner = "test-owner"
	RepoName = "git-envdiff"
	t.Cleanup(func() {
		apiBaseURL, releaseBaseURL, RepoOwner, RepoName = origAPI, origRelease, origOwner, origName
	})
	return srv
}

func TestDetectChannel(t *testing.T) {
	cases := map[string]string{
		"/home/x/.linuxbrew/bin/git-envdiff":                    "homebrew",
		"/opt/homebrew/bin/git-envdiff":                         "homebrew",
		"C:\\Users\\x\\scoop\\shims\\git-envdiff.exe":           "scoop",
		"/home/x/.local/pipx/venvs/git-envdiff/bin/git-envdiff": "pip",
		"/home/x/go/bin/git-envdiff":                            "go-install",
		"/home/x/.cargo/bin/git-envdiff":                        "cargo",
		"/usr/local/bin/git-envdiff":                            "unknown",
	}
	for path, want := range cases {
		if got := DetectChannel(path); got != want {
			t.Errorf("DetectChannel(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestUpgradeCommand_CoversEveryDetectedChannel(t *testing.T) {
	channels := []string{"homebrew", "scoop", "winget", "pip", "npm", "cargo", "go-install", "apt", "aur", "unknown"}
	for _, c := range channels {
		cmd := upgradeCommand(c)
		if cmd == "" {
			t.Errorf("upgradeCommand(%q) returned an empty string", c)
		}
	}
}

func TestCheck_ReportsNewerVersion(t *testing.T) {
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name": "v1.2.0", "assets": []}`)
	})

	tag, newer, err := Check("v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.2.0" || !newer {
		t.Fatalf("got tag=%q newer=%v, want tag=v1.2.0 newer=true", tag, newer)
	}
}

func TestCheck_ReportsUpToDate(t *testing.T) {
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name": "v1.2.0", "assets": []}`)
	})

	_, newer, err := Check("v1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if newer {
		t.Fatal("expected newer=false when versions match")
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("archive-contents")
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])

	checksums := hexSum + "  git-envdiff_1.0.0_linux_amd64.tar.gz\n" +
		"deadbeef  some-other-file.tar.gz\n"

	if err := verifyChecksum(data, checksums, "git-envdiff_1.0.0_linux_amd64.tar.gz"); err != nil {
		t.Fatalf("expected checksum to match, got: %v", err)
	}

	if err := verifyChecksum([]byte("tampered"), checksums, "git-envdiff_1.0.0_linux_amd64.tar.gz"); err == nil {
		t.Fatal("expected a checksum mismatch error, got nil")
	}

	if err := verifyChecksum(data, checksums, "not-in-the-list.tar.gz"); err == nil {
		t.Fatal("expected an error for a filename with no checksum entry")
	}
}

func buildTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "some-dir/" + name, Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func buildZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("some-dir/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return buf.Bytes()
}

func TestExtractFromTarGz_FindsBinaryByBaseName(t *testing.T) {
	archive := buildTarGz(t, "git-envdiff", []byte("binary-content"))
	got, err := extractFromTarGz(archive, "git-envdiff")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary-content" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractFromZip_FindsBinaryByBaseName(t *testing.T) {
	archive := buildZip(t, "git-envdiff.exe", []byte("binary-content"))
	got, err := extractFromZip(archive, "git-envdiff.exe")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary-content" {
		t.Fatalf("got %q", got)
	}
}

// buildRelease serves a fake GitHub "latest release" response plus the
// asset bytes it references, all from the same httptest server.
func buildRelease(t *testing.T, tag string, assetData, checksumsData []byte) *httptest.Server {
	t.Helper()
	assetFileName := assetName(tag)

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/test-owner/git-envdiff/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name": %q,
			"assets": [
				{"name": %q, "browser_download_url": %q},
				{"name": "checksums.txt", "browser_download_url": %q}
			]
		}`, tag, assetFileName, srv.URL+"/download/asset", srv.URL+"/download/checksums")
	})
	mux.HandleFunc("/download/asset", func(w http.ResponseWriter, r *http.Request) {
		w.Write(assetData)
	})
	mux.HandleFunc("/download/checksums", func(w http.ResponseWriter, r *http.Request) {
		w.Write(checksumsData)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSelfUpdate_ChecksumMismatchAbortsAndLeavesBinaryUntouched(t *testing.T) {
	binaryName := "git-envdiff"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	assetData := buildArchiveForCurrentPlatform(t, binaryName, []byte("NEW-CONTENT"))
	wrongChecksums := []byte("0000000000000000000000000000000000000000000000000000000000000000  " + assetName("v9.9.9") + "\n")

	srv := buildRelease(t, "v9.9.9", assetData, wrongChecksums)
	origAPI, origOwner, origName := apiBaseURL, RepoOwner, RepoName
	apiBaseURL, RepoOwner, RepoName = srv.URL, "test-owner", "git-envdiff"
	t.Cleanup(func() { apiBaseURL, RepoOwner, RepoName = origAPI, origOwner, origName })

	dir := t.TempDir()
	exePath := filepath.Join(dir, "git-envdiff")
	if err := os.WriteFile(exePath, []byte("OLD-CONTENT"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := selfUpdate(exePath, "v0.0.1")
	if err == nil {
		t.Fatal("expected a checksum mismatch error, got nil")
	}

	data, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "OLD-CONTENT" {
		t.Fatalf("binary was modified despite a checksum mismatch: %q", data)
	}
}

func TestSelfUpdate_SuccessReplacesBinaryAtomically(t *testing.T) {
	binaryName := "git-envdiff"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	newContent := []byte("NEW-CONTENT")
	assetData := buildArchiveForCurrentPlatform(t, binaryName, newContent)

	sum := sha256.Sum256(assetData)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + assetName("v9.9.9") + "\n")

	srv := buildRelease(t, "v9.9.9", assetData, checksums)
	origAPI, origOwner, origName := apiBaseURL, RepoOwner, RepoName
	apiBaseURL, RepoOwner, RepoName = srv.URL, "test-owner", "git-envdiff"
	t.Cleanup(func() { apiBaseURL, RepoOwner, RepoName = origAPI, origOwner, origName })

	dir := t.TempDir()
	exePath := filepath.Join(dir, "git-envdiff")
	if err := os.WriteFile(exePath, []byte("OLD-CONTENT"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := selfUpdate(exePath, "v0.0.1"); err != nil {
		t.Fatalf("selfUpdate: %v", err)
	}

	data, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(newContent) {
		t.Fatalf("got %q, want %q", data, newContent)
	}

	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatal("expected the replaced binary to remain executable")
	}
}

func TestSelfUpdate_AlreadyUpToDateDoesNothing(t *testing.T) {
	srv := buildRelease(t, "v1.0.0", nil, nil)
	origAPI, origOwner, origName := apiBaseURL, RepoOwner, RepoName
	apiBaseURL, RepoOwner, RepoName = srv.URL, "test-owner", "git-envdiff"
	t.Cleanup(func() { apiBaseURL, RepoOwner, RepoName = origAPI, origOwner, origName })

	dir := t.TempDir()
	exePath := filepath.Join(dir, "git-envdiff")
	os.WriteFile(exePath, []byte("UNCHANGED"), 0o755)

	if err := selfUpdate(exePath, "v1.0.0"); err != nil {
		t.Fatalf("selfUpdate: %v", err)
	}
	data, _ := os.ReadFile(exePath)
	if string(data) != "UNCHANGED" {
		t.Fatal("binary should not have been touched when already up to date")
	}
}

// buildArchiveForCurrentPlatform wraps binaryContent in whatever archive
// format assetName() expects for the platform the test is actually
// running on (tar.gz everywhere except Windows, which gets zip).
func buildArchiveForCurrentPlatform(t *testing.T, binaryName string, binaryContent []byte) []byte {
	t.Helper()
	if runtime.GOOS == "windows" {
		return buildZip(t, binaryName, binaryContent)
	}
	return buildTarGz(t, binaryName, binaryContent)
}

func TestReceiptRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", home)
	}

	if _, ok := readReceipt(); ok {
		t.Fatal("expected no receipt before one is written")
	}

	path, err := receiptPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"channel":"script","binary_path":"/x/git-envdiff","installed_at":"now"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r, ok := readReceipt()
	if !ok {
		t.Fatal("expected to read back the receipt just written")
	}
	if r.Channel != "script" || r.BinaryPath != "/x/git-envdiff" {
		t.Fatalf("unexpected receipt contents: %+v", r)
	}
}

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.2.0", "1.2.0", false},
		{"v1.10.0", "v1.9.0", true}, // numeric, not lexical
		{"v1.0.0", "v2.0.0", false}, // a newer local build is not "outdated"
		{"v0.1.0", "dev", true},     // dev builds count as older
		{"not-a-version", "v1.0.0", false},
		{"v1.2.0", "v1.2.0-rc1", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.latest, c.current); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
