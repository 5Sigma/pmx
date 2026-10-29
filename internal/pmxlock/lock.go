package pmxlock

import (
	"fmt"
	"os"
	"syscall"
)

type Lock struct {
	f *os.File
}

// TryAcquire opens path and attempts a non-blocking exclusive flock.
// Returns the lock on success. Returns an error (wrapping syscall.EWOULDBLOCK)
// if another process holds the lock, or any other OS error.
func TryAcquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("pmxlock: open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("pmxlock: lock %s: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() error {
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		_ = l.f.Close()
		return err
	}
	return l.f.Close()
}
