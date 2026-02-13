package integration

import "testing"

func TestFix(t *testing.T) {
	t.Parallel()
	for _, tc := range fixCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runFixCase(t, tc)
		})
	}
}
