// Command git-envdiff keeps a project's .env and .env.example in sync.
//
// See README.md for the full picture; docs/HOW_IT_WORKS.md for the
// technical detail behind the hook and install strategy.
package main

import (
	"fmt"
	"os"

	"github.com/tejjasdev/git-envdiff/internal/buildinfo"
	"github.com/tejjasdev/git-envdiff/internal/gitutil"
	"github.com/tejjasdev/git-envdiff/internal/hooks"
	"github.com/tejjasdev/git-envdiff/internal/install"
	"github.com/tejjasdev/git-envdiff/internal/state"
	"github.com/tejjasdev/git-envdiff/internal/update"
)

const usage = `git-envdiff — keep .env and .env.example in sync.

Usage:
  git-envdiff                 Check both directions right now and offer to fix
  git-envdiff install         Install Git hooks (global if Git >= 2.54, else this repo only)
  git-envdiff install --repo  Install hooks into this repository only
  git-envdiff uninstall       Remove whatever "install" added (same flags as install)
  git-envdiff status          Show what's currently installed
  git-envdiff on / off        Enable / disable git-envdiff in this repository
  git-envdiff update          Update git-envdiff itself
  git-envdiff update --check  Only check whether a newer version exists
  git-envdiff --version       Print the version
  git-envdiff --help          Show this help

git-envdiff never reads or transmits the values inside your .env file —
only variable names — and it never uses the network except for
"git-envdiff update". Learn more: https://github.com/tejjasdev/git-envdiff
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return hooks.RunManual()
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0

	case "-v", "--version", "version":
		fmt.Println("git-envdiff " + buildinfo.Version)
		return 0

	case "install":
		res, err := install.Install(hasFlag(args[1:], "--repo"))
		printResult(res, err)
		return exitFor(err)

	case "uninstall":
		res, err := install.Uninstall(hasFlag(args[1:], "--repo"))
		printResult(res, err)
		return exitFor(err)

	case "status":
		fmt.Printf("git-envdiff %s (installed via %s)\n", buildinfo.Version, update.Channel())
		fmt.Print(install.Status())
		return 0

	case "on":
		return toggle(true)

	case "off":
		return toggle(false)

	case "update":
		return runUpdate(hasFlag(args[1:], "--check"))

	case "hook":
		// Hidden entry point: only Git itself is meant to call this.
		return runHook(args[1:])

	default:
		fmt.Fprintf(os.Stderr, "git-envdiff: unknown command %q\n\n", args[0])
		fmt.Print(usage)
		return 1
	}
}

func toggle(enable bool) int {
	gitDir, err := gitutil.GitDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "git-envdiff: not inside a git repository")
		return 1
	}
	if enable {
		if err := state.SetDisabled(gitDir, false); err != nil {
			fmt.Fprintf(os.Stderr, "git-envdiff: %v\n", err)
			return 1
		}
		fmt.Println("git-envdiff: enabled in this repository.")
		return 0
	}
	if err := state.SetDisabled(gitDir, true); err != nil {
		fmt.Fprintf(os.Stderr, "git-envdiff: %v\n", err)
		return 1
	}
	fmt.Println("git-envdiff: disabled in this repository. Run `git-envdiff on` to re-enable.")
	return 0
}

func runUpdate(checkOnly bool) int {
	if checkOnly {
		tag, newer, err := update.Check(buildinfo.Version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git-envdiff: %v\n", err)
			return 1
		}
		if newer {
			fmt.Printf("A newer version is available: %s -> %s\n", buildinfo.Version, tag)
		} else {
			fmt.Println("You already have the latest version.")
		}
		return 0
	}
	if err := update.Run(buildinfo.Version); err != nil {
		fmt.Fprintf(os.Stderr, "git-envdiff: update failed: %v\n", err)
		return 1
	}
	return 0
}

// runHook is the hidden entry point Git's hooks call:
// `git-envdiff hook <event> [args...]`. It always exits 0, except for the
// pre-push event, which may exit 1 to block a push — see internal/hooks
// for exactly when and why.
func runHook(args []string) int {
	if len(args) == 0 {
		return 0
	}
	event, rest := args[0], args[1:]
	switch event {
	case "pre-commit":
		return guard(hooks.RunPreCommit)
	case "pre-push":
		return guard(hooks.RunPrePush)
	case "post-merge":
		return guard(func() int { return hooks.RunPostMerge(rest) })
	case "post-checkout":
		return guard(func() int { return hooks.RunPostCheckout(rest) })
	default:
		return 0
	}
}

// guard runs fn and turns a panic into a one-line warning and exit code 0,
// so a bug in git-envdiff can never fail the Git command that invoked the
// hook. Only runHook is guarded; user-invoked subcommands still crash loudly.
func guard(fn func() int) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "git-envdiff: internal error (ignored): %v\n", r)
			code = 0
		}
	}()
	return fn()
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func printResult(res install.Result, err error) {
	for _, m := range res.Messages {
		fmt.Println(m)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-envdiff: %v\n", err)
	}
}

func exitFor(err error) int {
	if err != nil {
		return 1
	}
	return 0
}
