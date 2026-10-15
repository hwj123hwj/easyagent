package main

import "sync"

// ringBuffer is a fixed-capacity, concurrency-safe byte buffer that keeps
// only the most recent writes. Used to capture the tail of the core's
// stdout/stderr so its dying words can be surfaced in the connection error.
type ringBuffer struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{buf: make([]byte, 0, capacity), cap: capacity}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= r.cap {
		r.buf = append(r.buf[:0], p[len(p)-r.cap:]...)
		return len(p), nil
	}
	r.buf = append(r.buf, p...)
	if over := len(r.buf) - r.cap; over > 0 {
		r.buf = r.buf[over:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
