package zipfs

import "sync"

var bufPool sync.Pool

const bufSize = 32768

func getBuf() []byte {
	b, ok := bufPool.Get().([]byte)
	if !ok || len(b) < bufSize {
		b = make([]byte, bufSize)
	}
	return b
}

func freeBuf(b []byte) {
	if len(b) >= bufSize {
		bufPool.Put(b)
	}
}
