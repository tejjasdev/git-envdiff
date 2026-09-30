// Package update is the ONLY package in git-envdiff allowed to make a
// network request — and even then, only when the user explicitly runs
// `git-envdiff update` or `git-envdiff update --check`. Nothing else in
// this tool ever touches the network: no telemetry, no automatic update
// checks, no phone-home. (ci.yml enforces this with a grep-based check
// over the rest of the codebase.)
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// maxDownloadBytes bounds any single download; releases are a few MB.
const maxDownloadBytes = 100 << 20

// RepoOwner and RepoName identify where releases are published. Override
// them (and apiBaseURL / releaseBaseURL below) in tests with a local
// httptest server.
var (
	RepoOwner = "tejjasdev"
	RepoName  = "git-envdiff"

	apiBaseURL     = "https://api.github.com"
	releaseBaseURL = "https://github.com"

	httpClient = &http.Client{Timeout: 20 * time.Second}
)

// Receipt records how git-envdiff was installed. It is written only by
// install.sh / install.ps1. Its presence — and its BinaryPath matching the
// currently running executable — is what allows `git-envdiff update` to
// replace itself in place; every other install method (Homebrew, Scoop,
// npm, pip, apt, `go install`, ...) is updated through that method's own
// tooling instead, since git-envdiff overwriting a file owned by a
// package manager would leave that package manager's records wrong.
type Receipt struct {
	Channel     string `json:"channel"`
	BinaryPath  string `json:"binary_path"`
	InstalledAt string `json:"installed_at"`
}

func receiptPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "git-envdiff", "receipt.json"), nil
	}
	return filepath.Join(home, ".git-envdiff", "receipt.json"), nil
}

func readReceipt() (Receipt, bool) {
	path, err := receiptPath()
	if err != nil {
		return Receipt{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Receipt{}, false
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return Receipt{}, false
	}
	return r, true
}

// DetectChannel makes a best-effort guess at how the running binary was
// installed, based on its path. Used only to print a plausible upgrade
// command when there is no install receipt to consult.
func DetectChannel(exePath string) string {
	p := strings.ToLower(strings.ReplaceAll(exePath, "\\", "/"))
	switch {
	case strings.Contains(p, "homebrew") || strings.Contains(p, "linuxbrew"):
		return "homebrew"
	case strings.Contains(p, "scoop"):
		return "scoop"
	case strings.Contains(p, "winget") || strings.Contains(p, "microsoft/windowsapps"):
		return "winget"
	case strings.Contains(p, "pipx"):
		return "pip"
	case strings.Contains(p, "site-packages"):
		return "pip"
	case strings.Contains(p, "node_modules") || strings.Contains(p, "/npm/"):
		return "npm"
	case strings.Contains(p, "cargo"):
		return "cargo"
	case strings.Contains(p, "/go/bin") || strings.Contains(p, "gopath"):
		return "go-install"
	default:
		return "unknown"
	}
}

// Channel describes how the running binary was installed, for `status`.
func Channel() string {
	exe, err := os.Executable()
	if err != nil {
		return "an unknown method"
	}
	if r, ok := readReceipt(); ok && samePath(r.BinaryPath, exe) {
		return "the install script"
	}
	return channelLabel(DetectChannel(exe))
}

// parseVersion extracts MAJOR.MINOR.PATCH from strings like "v1.2.3" or
// "1.2.3-rc1". It reports ok=false for anything else (e.g. "dev").
func parseVersion(v string) (parts [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	fields := strings.Split(v, ".")
	if v == "" || len(fields) > 3 {
		return parts, false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// versionNewer reports whether latest is strictly newer than current. An
// unparseable current version (a "dev" build) counts as older; an
// unparseable latest never counts as newer.
func versionNewer(latest, current string) bool {
	lp, lok := parseVersion(latest)
	if !lok {
		return false
	}
	cp, cok := parseVersion(current)
	if !cok {
		return true
	}
	for i := range lp {
		if lp[i] != cp[i] {
			return lp[i] > cp[i]
		}
	}
	return false
}

// upgradeCommand returns the command to show the user for a given channel.
func upgradeCommand(channel string) string {
	switch channel {
	case "homebrew":
		return "brew upgrade git-envdiff"
	case "scoop":
		return "scoop update git-envdiff"
	case "winget":
		return "winget upgrade git-envdiff"
	case "pip":
		return "pipx upgrade git-envdiff   (or: uv tool upgrade git-envdiff)"
	case "npm":
		return "npm update -g git-envdiff"
	case "cargo":
		return "cargo install git-envdiff --force"
	case "go-install":
		return fmt.Sprintf("go install github.com/%s/%s@latest", RepoOwner, RepoName)
	case "apt":
		return "sudo apt update && sudo apt upgrade git-envdiff"
	case "aur":
		return "yay -Syu git-envdiff-bin   (or your AUR helper of choice)"
	default:
		return fmt.Sprintf("download the latest release from %s/%s/%s/releases/latest", releaseBaseURL, RepoOwner, RepoName)
	}
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func latestRelease() (release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBaseURL, RepoOwner, RepoName)
	resp, err := httpClient.Get(url)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusTooManyRequests:
		return release{}, errors.New("GitHub is rate-limiting anonymous requests from your network; try again later")
	case http.StatusNotFound:
		return release{}, errors.New("no published release found yet")
	default:
		return release{}, fmt.Errorf("unexpected status %d fetching latest release", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, err
	}
	return rel, nil
}

// Check reports the latest published version without installing anything.
// It returns the latest tag and whether it differs from currentVersion.
func Check(currentVersion string) (latestTag string, newer bool, err error) {
	rel, err := latestRelease()
	if err != nil {
		return "", false, err
	}
	return rel.TagName, versionNewer(rel.TagName, currentVersion), nil
}

// Run performs `git-envdiff update`: it self-updates if this exact binary
// was installed via install.sh/install.ps1, and otherwise reports the
// latest version and prints the right command for whatever channel
// installed it instead.
func Run(currentVersion string) error {
	exePath, err := os.Executable()
	if err != nil {
		exePath = ""
	}

	if r, ok := readReceipt(); ok && exePath != "" && samePath(r.BinaryPath, exePath) {
		return selfUpdate(exePath, currentVersion)
	}

	channel := "unknown"
	if exePath != "" {
		channel = DetectChannel(exePath)
	}

	latestTag, newer, err := Check(currentVersion)
	switch {
	case err != nil:
		fmt.Printf("git-envdiff: could not check the latest version (%v).\n", err)
	case newer:
		fmt.Printf("git-envdiff: a newer version is available (%s -> %s).\n", currentVersion, latestTag)
	default:
		fmt.Println("git-envdiff: you already have the latest version.")
	}
	fmt.Printf("Installed via %s; update it with:\n  %s\n", channelLabel(channel), upgradeCommand(channel))
	return nil
}

func channelLabel(channel string) string {
	if channel == "unknown" {
		return "an unrecognized method"
	}
	return channel
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return ra == rb
}

// ---- self-update (install.sh / install.ps1 installs only) ---------------

func assetName(version string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("git-envdiff_%s_%s_%s.%s", strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH, ext)
}

func selfUpdate(exePath, currentVersion string) error {
	rel, err := latestRelease()
	if err != nil {
		return fmt.Errorf("checking latest release: %w", err)
	}
	if !versionNewer(rel.TagName, currentVersion) {
		fmt.Println("git-envdiff: already up to date (" + currentVersion + ").")
		return nil
	}

	want := assetName(rel.TagName)
	var assetURL, checksumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case want:
			assetURL = a.BrowserDownloadURL
		case "checksums.txt":
			checksumsURL = a.BrowserDownloadURL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no release asset found for this platform (%s); download manually from %s/%s/%s/releases/latest",
			want, releaseBaseURL, RepoOwner, RepoName)
	}
	if checksumsURL == "" {
		return errors.New("release is missing checksums.txt; refusing to install an unverified binary")
	}

	archiveData, err := download(assetURL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", want, err)
	}
	checksumsData, err := download(checksumsURL)
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := verifyChecksum(archiveData, string(checksumsData), want); err != nil {
		return err
	}

	newBinary, err := extractBinary(archiveData, want)
	if err != nil {
		return fmt.Errorf("extracting %s: %w", want, err)
	}
	if err := replaceBinary(exePath, newBinary); err != nil {
		return err
	}

	fmt.Printf("git-envdiff: updated %s -> %s\n", currentVersion, rel.TagName)
	return nil
}

func download(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownloadBytes {
		return nil, errors.New("download is unexpectedly large; refusing to continue")
	}
	return data, nil
}

// verifyChecksum checks data's SHA-256 against the matching line in a
// `sha256sum`-style checksums file. The update is refused outright (an
// error is returned, nothing is installed) if the checksum doesn't match
// or isn't found at all.
func verifyChecksum(data []byte, checksumsFile, wantAssetName string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])

	for _, line := range strings.Split(checksumsFile, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != wantAssetName {
			continue
		}
		if fields[0] != got {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", wantAssetName, fields[0], got)
		}
		return nil
	}
	return fmt.Errorf("no checksum entry found for %s", wantAssetName)
}

func extractBinary(archiveData []byte, archiveName string) ([]byte, error) {
	binaryName := "git-envdiff"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	if strings.HasSuffix(archiveName, ".zip") {
		return extractFromZip(archiveData, binaryName)
	}
	return extractFromTarGz(archiveData, binaryName)
}

func extractFromTarGz(data []byte, wantName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == wantName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", wantName)
}

func extractFromZip(data []byte, wantName string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if filepath.Base(f.Name) == wantName {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", wantName)
}

// replaceBinary atomically swaps the running executable for newBinary.
// On Unix this is a same-filesystem rename. On Windows the currently
// running .exe can't be overwritten directly, so the old file is renamed
// aside first (left for cleanup on the next update) before the new one
// takes its place.
func replaceBinary(exePath string, newBinary []byte) error {
	dir := filepath.Dir(exePath)

	tmp, err := os.CreateTemp(dir, ".git-envdiff-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	mode := os.FileMode(0o755)
	if info, err := os.Stat(exePath); err == nil {
		mode = info.Mode()
	}
	_ = os.Chmod(tmpPath, mode)

	if runtime.GOOS == "windows" {
		oldPath := exePath + ".old"
		_ = os.Remove(oldPath) // best-effort cleanup left over from a previous update
		if err := os.Rename(exePath, oldPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("could not move the running binary aside: %w", err)
		}
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
