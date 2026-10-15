package main

import (
	"strings"
	"sync"
	"testing"
)

func TestRingBuffer(t *testing.T) {
	rb := newRingBuffer(16)
	rb.Write([]byte("hello"))
	// Overflow: keep only the last 16 bytes.
	rb.Write([]byte("12345678901234567890"))
	if got := rb.String(); got != "5678901234567890" {
		t.Fatalf("overflow tail = %q", got)
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	rb := newRingBuffer(1024)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rb.Write([]byte(strings.Repeat("x", 10)))
		}(i)
	}
	wg.Wait()
	if got := rb.String(); len(got) != 80 {
		t.Fatalf("len = %d, want 80", len(got))
	}
}
