// Package buildinfo holds the single piece of state that varies between a
// release build and a local build: the version string.
package buildinfo

// Version is overridden at release time via:
//
//	-ldflags "-X github.com/tejjasdev/git-envdiff/internal/buildinfo.Version=v1.2.3"
//
// GoReleaser does this automatically from the Git tag (see .goreleaser.yaml).
// A binary built without that flag (e.g. `go build .` locally) reports "dev".
var Version = "dev"
