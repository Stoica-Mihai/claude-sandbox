package main

import "sync"

// RingBuffer is a fixed-capacity circular byte buffer for storing recent
// terminal output. It allows replaying scrollback when a WebSocket client
// reattaches to a running PTY session.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
	head int // write position (next byte goes here)
	size int // number of valid bytes currently stored
}

// NewRingBuffer creates a RingBuffer with the given byte capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		buf: make([]byte, capacity),
		cap: capacity,
	}
}

// Write appends p to the ring buffer, overwriting the oldest bytes if the
// buffer is full. This implements a circular write: new data always goes in,
// old data is silently evicted when capacity is exceeded.
func (rb *RingBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for _, b := range p {
		rb.buf[rb.head] = b
		rb.head = (rb.head + 1) % rb.cap
		if rb.size < rb.cap {
			rb.size++
		}
	}
}

// Bytes returns a copy of the current buffer contents in chronological order
// (oldest bytes first). Safe for concurrent use.
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.size == 0 {
		return nil
	}

	out := make([]byte, rb.size)
	if rb.size < rb.cap {
		// Buffer hasn't wrapped yet — data starts at index 0.
		copy(out, rb.buf[:rb.size])
	} else {
		// Buffer has wrapped — oldest byte is at head (the next write position).
		start := rb.head // oldest byte
		n := copy(out, rb.buf[start:rb.cap])
		copy(out[n:], rb.buf[:start])
	}
	return out
}
