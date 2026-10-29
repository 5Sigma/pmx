//go:build windows

package pmxlock

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type Lock struct {
	f *os.File
}

// TryAcquire opens path and attempts a non-blocking exclusive lock via LockFileEx.
// Returns an error wrapping windows.ERROR_LOCK_VIOLATION if another process holds
// the lock, or any other OS error.
func TryAcquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("pmxlock: open %s: %w", path, err)
	}
	ol := new(windows.Overlapped)
	const lockExclusive = 0x00000002
	const lockFailImmediately = 0x00000001
	err = windows.LockFileEx(windows.Handle(f.Fd()), lockExclusive|lockFailImmediately, 0, 1, 0, ol)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("pmxlock: lock %s: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() error {
	ol := new(windows.Overlapped)
	if err := windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, ol); err != nil {
		_ = l.f.Close()
		return err
	}
	return l.f.Close()
}
