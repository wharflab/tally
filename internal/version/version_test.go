package version

import (
	"strings"
	"sync"
	"testing"
)

var versionTestMu sync.Mutex

func TestVersionNormalizesPlaceholderEmbedLabel(t *testing.T) {
	t.Parallel()

	versionTestMu.Lock()
	t.Cleanup(versionTestMu.Unlock)

	originalVersion := version
	version = "{BUILD_EMBED_LABEL}"
	t.Cleanup(func() {
		version = originalVersion
	})

	got := Version()
	if strings.Contains(got, "{BUILD_EMBED_LABEL}") {
		t.Fatalf("Version() leaked placeholder: %q", got)
	}
	if !strings.HasPrefix(got, "dev") {
		t.Fatalf("Version() prefix mismatch: %q", got)
	}
}
