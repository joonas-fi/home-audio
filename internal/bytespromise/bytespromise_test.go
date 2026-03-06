package bytespromise

import (
	"cmp"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/function61/gokit/sync/syncutil"
	"github.com/function61/gokit/testing/assert"
)

func TestBytesPromise(t *testing.T) {
	bp := New()

	go func() {
		w := bp.NewWriter()
		_, _ = w.Write([]byte("audio: "))
		time.Sleep(75 * time.Millisecond)
		_, _ = w.Write([]byte("hello world"))
		time.Sleep(25 * time.Millisecond)
		_, _ = w.Write([]byte(" from my slow stream"))
		w.Close()
	}()

	// launch 100 readers all consuming the same slow stream
	tasks := []<-chan error{}
	for i := 0; i < 100; i++ {
		tasks = append(tasks, syncutil.Async(func() error {
			b, err := io.ReadAll(bp.NewReader())
			err2 := func() error {
				const expected = "audio: hello world from my slow stream"
				if string(b) != expected {
					return fmt.Errorf("actual[%s] != expected[%s]", string(b), expected)
				}
				return nil
			}()
			return cmp.Or(err, err2)
		}))
	}

	// in 10ms check that only the first chunk is available
	time.Sleep(10 * time.Millisecond)
	buf := [128]byte{}
	n, err := bp.NewReader().Read(buf[:])
	assert.Ok(t, err)
	assert.Equal(t, n, 7)
	assert.Equal(t, string(buf[:n]), "audio: ")

	for _, task := range tasks {
		assert.Ok(t, <-task)
	}
}
