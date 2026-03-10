package core

import "testing"

func TestClampByteIndex_MalformedUTF8Prefix(t *testing.T) {
	t.Parallel()

	line := string([]byte{0x80, 0x81, 0x82, 'a'})

	if got := ClampByteIndex(line, 1); got != 0 {
		t.Fatalf("ClampByteIndex(..., 1) = %d, want 0", got)
	}

	if got := ClampByteIndex(line, 2); got != 0 {
		t.Fatalf("ClampByteIndex(..., 2) = %d, want 0", got)
	}

	if got := ClampByteIndex(line, 4); got != 4 {
		t.Fatalf("ClampByteIndex(..., 4) = %d, want 4", got)
	}
}
