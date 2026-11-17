package log

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// TestLogWriterCloseWhileWriting checks that closing the log file does not run
// concurrently with writes, which can block forever on Windows, and that
// writes after closing are dropped.
func TestLogWriterCloseWhileWriting(t *testing.T) {
	t.Parallel()

	for range 50 {
		w, err := NewFileWriter(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		stop := make(chan struct{})
		for range 4 {
			wg.Go(func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					// Like slog, which writes to the file directly from any
					// goroutine. WriteMessage is only used by the log writer
					// goroutine.
					_, _ = w.Write([]byte("concurrent log line\n"))
				}
			})
		}

		time.Sleep(time.Millisecond)
		closed := make(chan struct{})
		go func() {
			w.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Fatal("closing the log writer blocked while writing")
		}
		close(stop)
		wg.Wait()

		_, err = w.Write([]byte("after close\n"))
		if !errors.Is(err, os.ErrClosed) {
			t.Errorf("write after close: got %v, want %v", err, os.ErrClosed)
		}
		w.WriteMessage(&logLine{msg: "after close", level: InfoLevel, timestamp: time.Now()}, 0)
		w.Close() // Closing twice is fine.
	}
}
