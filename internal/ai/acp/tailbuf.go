package acp

import (
	"sync"

	"github.com/armon/circbuf"
)

// tailBuffer is an io.Writer that retains only the last N bytes written.
// It is safe for concurrent use.
type tailBuffer struct {
	mu  sync.Mutex
	buf *circbuf.Buffer
}

func newTailBuffer(limit int) *tailBuffer {
	if limit <= 0 {
		return &tailBuffer{}
	}
	b, err := circbuf.NewBuffer(int64(limit))
	if err != nil {
		// Should never happen for limit > 0, but degrade gracefully.
		return &tailBuffer{}
	}
	return &tailBuffer{buf: b}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.buf == nil || n == 0 {
		return n, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *tailBuffer) String() string {
	if b.buf == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
