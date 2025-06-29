// represents `[]byte` promise which is slowly being produced but where there are N consumers wanting to read from it
// whenever content slowly becomes available.
package bytespromise

import (
	"bytes"
	"io"
	"sync"

	. "github.com/function61/gokit/builtin"
)

type Promise struct {
	mu                 *sync.Mutex // protects access to all following members
	buf                *bytes.Buffer
	done               chan Void
	nextChunkAvailable chan Void
}

func New() *Promise {
	return &Promise{
		mu:                 &sync.Mutex{},
		buf:                &bytes.Buffer{},
		done:               make(chan Void),
		nextChunkAvailable: make(chan Void),
	}
}

func (b *Promise) NewWriter() io.WriteCloser {
	return &bytesPromiseWriter{b}
}

func (b *Promise) NewReader() io.Reader {
	return &bytesPromiseReader{
		promise: b,
		offset:  0,
	}
}

func (p *Promise) Write(buf []byte, isLast bool) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n, err := p.buf.Write(buf) // promises to always succeed

	if isLast {
		close(p.done)
	}

	nextChunkAvailable := p.nextChunkAvailable
	// new signal for new batch of readers waiting for the *next* chunk
	p.nextChunkAvailable = make(chan Void)
	// signal all parties waiting on the chunk we wrote just now
	close(nextChunkAvailable)

	return n, err
}

func (p *Promise) Read(out []byte, offset int) (int, error) {
	n, err, nextChunkAvailable := func() (int, error, chan Void) {
		p.mu.Lock()
		defer p.mu.Unlock()

		unreadAvailable := p.buf.Bytes()[offset:]

		n := copy(out, unreadAvailable)
		if n > 0 {
			return n, nil, nil
		}
		// `n==0`
		select {
		case <-p.done:
			return 0, io.EOF, nil
		default: // no EOF but got not data => wait for more data to arrive
			return 0, nil, p.nextChunkAvailable
		}
	}()
	if nextChunkAvailable != nil { // `n` and `err` were nil
		<-nextChunkAvailable
		return p.Read(out, offset) // try again (we shouldn't recurse uncontrollably)
	} else {
		return n, err
	}
}

type bytesPromiseReader struct {
	promise *Promise
	offset  int
}

func (c *bytesPromiseReader) Read(p []byte) (int, error) {
	n, err := c.promise.Read(p, c.offset)
	c.offset += n
	return n, err
}

type bytesPromiseWriter struct {
	promise *Promise
}

func (b *bytesPromiseWriter) Write(p []byte) (n int, err error) {
	return b.promise.Write(p, false)
}

func (b *bytesPromiseWriter) Close() error {
	_, err := b.promise.Write([]byte{}, true)
	return err
}

var _ interface {
	io.WriteCloser
} = (*bytesPromiseWriter)(nil)
