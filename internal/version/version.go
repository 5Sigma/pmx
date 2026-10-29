package version

// Version is the release version. Commit is the short git hash injected at
// build time via -ldflags; it falls back to "unknown" for local go builds.
var (
	Version = "0.2.0"
	Commit  = "unknown"
)

// String returns "0.2.0 (abc1234)" or "0.2.0 (unknown)".
func String() string {
	return Version + " (" + Commit + ")"
}
