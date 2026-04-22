package fix

import (
	"context"
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

func TestDL4001CleanupResolver_ID(t *testing.T) {
	t.Parallel()
	r := &dl4001CleanupResolver{}
	if got := r.ID(); got != rules.DL4001CleanupResolverID {
		t.Errorf("ID() = %q, want %q", got, rules.DL4001CleanupResolverID)
	}
}

func TestDL4001CleanupResolver_PreservesMixedEnv(t *testing.T) {
	t.Parallel()

	// ENV binds a tool-config key (CURL_HOME) alongside unrelated keys
	// (APP_VERSION, PATH). Deleting the whole instruction would drop
	// APP_VERSION and PATH, which is data loss. The resolver must either
	// leave the instruction alone or surgically drop only CURL_HOME; the
	// current MVP chooses "leave it alone" and still removes the COPY
	// heredoc writing .curlrc.
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
ENV CURL_HOME=/etc/curl APP_VERSION=1.0 PATH=/usr/local/bin:$PATH
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry 5
EOF
RUN curl https://example.com/one
RUN wget https://example.com/two
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, e := range edits {
		// Lines 3 is the ENV with mixed keys — must not be deleted.
		if e.Location.Start.Line <= 3 && e.Location.End.Line > 3 && e.NewText == "" {
			t.Fatalf("cleanup deleted mixed ENV on line 3: edit=%+v", e)
		}
	}
}

func TestDL4001CleanupResolver_DeletesDedicatedEnv(t *testing.T) {
	t.Parallel()

	// An ENV that binds only a tool-config key is safe to delete entirely.
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
ENV CURL_HOME=/etc/curl
RUN curl https://example.com/one
RUN wget https://example.com/two
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	foundEnvDelete := false
	for _, e := range edits {
		if e.Location.Start.Line == 3 && e.Location.End.Line == 4 && e.NewText == "" {
			foundEnvDelete = true
			break
		}
	}
	if !foundEnvDelete {
		t.Fatalf("expected ENV-only line 3 to be deleted, edits=%+v", edits)
	}
}

func TestDL4001CleanupResolver_DropsCurlrcCopy(t *testing.T) {
	t.Parallel()

	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
COPY --chmod=0644 <<EOF /etc/.curlrc
--retry 5
EOF
RUN curl https://example.com/one
RUN wget https://example.com/two
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// At least one edit should delete lines covering the COPY heredoc (lines 3-5).
	found := false
	for _, e := range edits {
		if e.NewText != "" {
			continue
		}
		if e.Location.Start.Line == 3 && e.Location.End.Line >= 6 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected curlrc COPY heredoc (lines 3-5) deletion, edits=%+v", edits)
	}
}
