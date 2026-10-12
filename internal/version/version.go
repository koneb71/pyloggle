// Package version carries the build identity. Release builds set it with
// -ldflags "-X github.com/koneb71/pyloggle/internal/version.Version=1.0.0 -X ...Commit=abc1234".
package version

import (
	"runtime/debug"
	"strings"
)

// Version is the release (without a leading "v"); Commit is the short hash when known.
var (
	Version = "dev"
	Commit  = ""
)

// Get is the version to report: Version when the build set it, else the module version Go
// recorded (for `go install github.com/koneb71/pyloggle/cmd/ingestd@v1.0.0`), else "dev".
func Get() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}

// String renders "version (commit)".
func String() string {
	if Commit != "" {
		return Get() + " (" + Commit + ")"
	}
	return Get()
}
