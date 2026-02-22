package fileval

import (
	"io"
	"os"
	"unicode/utf8"
)

const chunkSize = 32 * 1024 // 32 KB

// LooksUTF8 checks whether the file at path appears to contain valid UTF-8 text.
// It reads in 32 KB chunks, carrying up to 3 trailing bytes between reads to
// handle code points split across chunk boundaries. It stops after maxBytes
// (a heuristic "looks like UTF-8" threshold) and fails fast on the first
// definitely-invalid chunk.
func LooksUTF8(path string, maxBytes int64) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, chunkSize)
	var carry []byte // up to 3 bytes from previous read
	var totalRead int64

	for maxBytes <= 0 || totalRead < maxBytes {
		n, readErr := f.Read(buf)
		if n == 0 && readErr != nil {
			if readErr == io.EOF {
				// Check any remaining carry bytes.
				if len(carry) > 0 && !utf8.Valid(carry) {
					return false, nil
				}
				break
			}
			return false, readErr
		}

		totalRead += int64(n)

		// Prepend carry from previous iteration.
		chunk := buf[:n]
		if len(carry) > 0 {
			chunk = append(carry, chunk...)
			carry = nil
		}

		// Peel off up to 3 trailing bytes that might be a partial code point.
		// A valid UTF-8 code point is at most 4 bytes, so if the last 1–3
		// bytes don't form a complete rune, carry them to the next read.
		trail := trailingIncomplete(chunk)
		if trail > 0 {
			carry = make([]byte, trail)
			copy(carry, chunk[len(chunk)-trail:])
			chunk = chunk[:len(chunk)-trail]
		}

		if !utf8.Valid(chunk) {
			return false, nil
		}

		if readErr == io.EOF {
			// Validate any remaining carry.
			if len(carry) > 0 && !utf8.Valid(carry) {
				return false, nil
			}
			break
		}
	}

	return true, nil
}

// trailingIncomplete returns the number of trailing bytes in data that form
// an incomplete UTF-8 sequence. Returns 0 if the data ends on a complete
// code-point boundary.
func trailingIncomplete(data []byte) int {
	n := len(data)
	if n == 0 {
		return 0
	}

	// Walk backwards up to 3 bytes looking for a leading byte of a
	// multi-byte sequence that isn't complete yet.
	for i := 1; i <= 3 && i <= n; i++ {
		b := data[n-i]
		if b < 0x80 {
			// ASCII byte — everything before it is complete.
			return 0
		}
		if b&0xC0 == 0xC0 {
			// Leading byte of a multi-byte sequence.
			var expected int
			switch {
			case b&0xE0 == 0xC0:
				expected = 2
			case b&0xF0 == 0xE0:
				expected = 3
			case b&0xF8 == 0xF0:
				expected = 4
			default:
				// Invalid leading byte — not incomplete, just bad.
				return 0
			}
			if i < expected {
				return i
			}
			// The sequence is complete.
			return 0
		}
		// Continuation byte (10xxxxxx) — keep scanning.
	}

	return 0
}
