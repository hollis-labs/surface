//go:build unix

package ops

import (
	"syscall"
	"testing"
	"time"
)

func TestReadTrackerDoesNotBlockOnAFIFO(t *testing.T) {
	fifo := t.TempDir() + "/tracker.json"
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no FIFO here:", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadTracker(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || err.Error() != errTrackerPath.Error() {
			t.Fatalf("a FIFO: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
}
