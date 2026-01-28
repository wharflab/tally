package version

var (
	version = "dev"
	commit  = "unknown"
)

// Version returns the current version string
func Version() string {
	commitHash := Commit()
	if commitHash != "unknown" && len(commitHash) > 7 {
		return version + " (" + commitHash[:7] + ")"
	}
	return version
}

// Commit returns the git commit hash.
func Commit() string {
	return commit
}
