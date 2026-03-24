package main

import "sync"

const (
	// defaultBufferCapacity is the default ring buffer size per session (1MB).
	defaultBufferCapacity = 1 << 20
)

// RingBuffer is a fixed-capacity circular byte buffer for storing recent
// terminal output. Thread-safe.
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

// Write appends p to the ring buffer, overwriting the oldest bytes if full.
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

// Bytes returns a copy of the current buffer contents in chronological order.
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.size == 0 {
		return nil
	}

	out := make([]byte, rb.size)
	if rb.size < rb.cap {
		copy(out, rb.buf[:rb.size])
	} else {
		start := rb.head
		n := copy(out, rb.buf[start:rb.cap])
		copy(out[n:], rb.buf[:start])
	}
	return out
}

// Reset clears the buffer.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.size = 0
}
