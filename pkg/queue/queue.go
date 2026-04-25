// Package queue implements a simple FIFO queue for float32 audio samples.
package queue

import "sync/atomic"

// Queue is a simple FIFO queue for float32 audio samples.
// It is backed by a dynamic slice and is used to buffer audio chunks
// between the real-time recording callback and the compression/writing loop.
type Queue struct {
	// buf holds the queued audio samples.
	buf []float32

	// isClosed is a flag indicating whether the queue is closed.
	isClosed int32
}

// New creates a new Queue with a pre-allocated capacity of bufLen.
// The initial length is 0.
func New(bufLen int) *Queue {
	return &Queue{
		buf: make([]float32, 0, bufLen),
	}
}

// Push appends the given slice of audio samples to the end of the queue.
// This operation may trigger a reallocation if the underlying capacity is exceeded.
func (q *Queue) Push(v []float32) {
	q.buf = append(q.buf, v...)
}

// Pop copies up to len(p) samples from the front of the queue into p.
// It returns the number of samples actually copied.
// If the queue contains fewer samples than len(p), only the available samples are copied.
// The popped samples are removed from the queue.
func (q *Queue) Pop(p []float32) int {
	if len(q.buf) == 0 {
		return 0
	}

	n := len(p)
	n = min(n, len(q.buf))

	copy(p[:n], q.buf[:n])
	q.buf = q.buf[n:]

	return n
}

// Reset clears the queue, setting its length to 0 while retaining the underlying capacity.
func (q *Queue) Reset() {
	q.buf = q.buf[:0]
}

// Len returns the current number of samples in the queue.
func (q *Queue) Len() int {
	return len(q.buf)
}

// Close sets the isClosed flag to 1 (close).
func (q *Queue) Close() {
	atomic.StoreInt32(&q.isClosed, 1)
}

// IsClosed returns true if the queue is closed.
func (q *Queue) IsClosed() bool {
	return atomic.LoadInt32(&q.isClosed) == 1
}
