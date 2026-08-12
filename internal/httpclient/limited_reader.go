package httpclient

import (
	"fmt"
	"io"
)

// limitedReadCloser wraps an io.ReadCloser and returns an error once more than
// maxSize bytes have been read in total, instead of silently truncating like
// io.LimitReader. This gives streaming responses the same size-safety
// guarantee as the buffered path (which rejects bodies over maxResponseSize)
// without requiring the whole body to be read upfront.
type limitedReadCloser struct {
	rc      io.ReadCloser
	maxSize int
	read    int
}

func newLimitedReadCloser(rc io.ReadCloser, maxSize int) io.ReadCloser {
	return &limitedReadCloser{rc: rc, maxSize: maxSize}
}

// Read satisfies io.Reader. Per the io.Reader contract, callers must process
// the n > 0 bytes returned even when err is also set, so exceeding the limit
// mid-chunk still surfaces every byte read before the error is returned.
func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.rc.Read(p)
	l.read += n
	if l.read > l.maxSize {
		return n, fmt.Errorf("stream response exceeds maximum size of %d bytes", l.maxSize)
	}
	return n, err
}

func (l *limitedReadCloser) Close() error {
	return l.rc.Close()
}
