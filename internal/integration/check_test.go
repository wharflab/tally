package integration

import "testing"

func TestCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range checkCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runCheckCase(t, tc)
		})
	}
}
