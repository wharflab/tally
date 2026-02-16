package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestInvalidDefaultArgInFromRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewInvalidDefaultArgInFromRule().Metadata())
}

func TestInvalidDefaultArgInFromRule_Check(t *testing.T) {
	t.Parallel()
	r := NewInvalidDefaultArgInFromRule()

	cases := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name: "flags missing default for tag in base name",
			content: `ARG VERSION
FROM busybox:$VERSION
`,
			wantCount: 1,
		},
		{
			name: "flags missing default for full base image arg",
			content: `ARG IMAGE
FROM $IMAGE
`,
			wantCount: 1,
		},
		{
			name: "flags invalid default expansion that leaves trailing colon",
			content: `ARG SFX="box:"
FROM busy${SFX}
`,
			wantCount: 1,
		},
		{
			name: "allows valid default in base name",
			content: `ARG VERSION="latest"
FROM busybox:${VERSION}
`,
			wantCount: 0,
		},
		{
			name: "allows empty default that still produces valid base image",
			content: `ARG BUSYBOX_VARIANT=""
FROM busybox:stable${BUSYBOX_VARIANT}
`,
			wantCount: 0,
		},
		{
			name: "allows unset arg used as suffix that still produces valid base image",
			content: `ARG BUSYBOX_VARIANT
FROM busybox:stable${BUSYBOX_VARIANT}
`,
			wantCount: 0,
		},
		{
			name: "expands meta arg defaults transitively",
			content: `ARG NAME=busybox
ARG TAG=latest
ARG BASE=${NAME}:${TAG}
FROM ${BASE}
`,
			wantCount: 0,
		},
		{
			name: "flags invalid expanded default",
			content: `ARG TAG=
ARG BASE=busybox:${TAG}
FROM ${BASE}
`,
			wantCount: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tc.content)
			violations := r.Check(input)
			if len(violations) != tc.wantCount {
				t.Fatalf("got %d violations, want %d", len(violations), tc.wantCount)
			}
		})
	}
}
