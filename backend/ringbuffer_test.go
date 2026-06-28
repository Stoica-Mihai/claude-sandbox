package main

import (
	"bytes"
	"testing"
)

func TestRingBufferBelowCapacity(t *testing.T) {
	rb := NewRingBuffer(16)
	rb.Write([]byte("hello"))
	if got := rb.Bytes(); !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestRingBufferEmpty(t *testing.T) {
	rb := NewRingBuffer(16)
	if got := rb.Bytes(); got != nil {
		t.Fatalf("got %q, want nil", got)
	}
}

func TestRingBufferWrapAround(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("abcdefghij")) // 10 bytes into cap 8 → keep last 8
	if got := rb.Bytes(); !bytes.Equal(got, []byte("cdefghij")) {
		t.Fatalf("got %q, want %q", got, "cdefghij")
	}
}

func TestRingBufferExactCapacity(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write([]byte("12345"))
	if got := rb.Bytes(); !bytes.Equal(got, []byte("12345")) {
		t.Fatalf("got %q, want %q", got, "12345")
	}
}

func TestRingBufferMultipleWritesWrap(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("ab"))
	rb.Write([]byte("cde")) // total "abcde" → keep last 4 "bcde"
	if got := rb.Bytes(); !bytes.Equal(got, []byte("bcde")) {
		t.Fatalf("got %q, want %q", got, "bcde")
	}
}
