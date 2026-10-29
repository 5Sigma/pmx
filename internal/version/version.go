package version

// Version is the release version. Commit is the short git hash injected at
// build time via -ldflags; it falls back to "unknown" for local go builds.
var (
	Version = "0.2.2"
	Commit  = "unknown"
)

// String returns "0.2.2 (abc1234)" or "0.2.2 (unknown)".
func String() string {
	return Version + " (" + Commit + ")"
}
