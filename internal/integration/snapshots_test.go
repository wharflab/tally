package integration

import (
	"os"
	"path/filepath"

	"github.com/gkampitakis/go-snaps/snaps"
)

func integrationSnapshotConfig(opts ...func(*snaps.Config)) *snaps.Config {
	opts = append([]func(*snaps.Config){snaps.Dir(integrationSnapshotDir())}, opts...)
	if os.Getenv("INTEGRATION_STRICT_SNAPSHOTS") == "true" {
		opts = append(opts, snaps.Update(false))
	}
	return snaps.WithConfig(opts...)
}

func integrationSnapshotDir() string {
	abs, err := filepath.Abs("__snapshots__")
	if err != nil {
		return "__snapshots__"
	}
	return abs
}
