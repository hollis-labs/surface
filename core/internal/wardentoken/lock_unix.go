//go:build unix

package wardentoken

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock on path (created 0600). Every process sharing a
// parent file takes it around read, mint and save.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: the configured parent file's lock
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // G115: fd fits in int
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // G115: fd fits in int
		_ = f.Close()
	}, nil
}
