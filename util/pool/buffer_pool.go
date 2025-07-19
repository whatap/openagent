package pool

import (
	"sync"
)

// BufferPool manages a pool of byte slices for memory reuse
// This reduces GC pressure by reusing buffers instead of allocating new ones
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new buffer pool with the specified initial capacity
func NewBufferPool(initialCapacity int) *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				// Create a new buffer with the initial capacity
				return make([]byte, 0, initialCapacity)
			},
		},
	}
}

// Get retrieves a buffer from the pool
// The returned buffer may have existing data, so it should be reset if needed
func (bp *BufferPool) Get() []byte {
	return bp.pool.Get().([]byte)
}

// Put returns a buffer to the pool for reuse
// The buffer will be reset to zero length but capacity is preserved
func (bp *BufferPool) Put(buf []byte) {
	// Reset the buffer length to 0 but keep the capacity
	buf = buf[:0]
	bp.pool.Put(buf)
}

// GetWithSize retrieves a buffer from the pool and ensures it has at least the specified capacity
func (bp *BufferPool) GetWithSize(size int) []byte {
	buf := bp.Get()
	if cap(buf) < size {
		// If the buffer is too small, create a new one with the required size
		return make([]byte, 0, size)
	}
	return buf
}