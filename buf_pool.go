package zipfs

import "sync"

// This is an experiment with hiding private data from the
// rest of the package.
var bufPool struct {
	Get  func() []byte // Allocate a buffer
	Free func([]byte)  // Free the buffer
}

func init() {
	// Private data
	var pool sync.Pool
	const bufSize = 32768

	bufPool.Get = func() []byte {
		b, ok := pool.Get().([]byte)
		if !ok || len(b) < bufSize {
			b = make([]byte, bufSize)
		}
		return b
	}

	bufPool.Free = func(b []byte) {
		if len(b) >= bufSize {
			pool.Put(b)
		}
	}
}
