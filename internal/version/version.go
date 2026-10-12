// Package version carries the build identity (set with -ldflags "-X github.com/koneb71/pyloggle/internal/version.Version=...").
package version

// Version is the release or git describe string; Commit the short hash when known.
var (
	Version = "0.3.0-dev"
	Commit  = ""
)

// String renders "version (commit)".
func String() string {
	if Commit != "" {
		return Version + " (" + Commit + ")"
	}
	return Version
}
