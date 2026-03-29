package main

import "sync"

const (
	defaultBufferCapacity = 1 << 20
)

type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
	head int
	size int
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		buf: make([]byte, capacity),
		cap: capacity,
	}
}

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
