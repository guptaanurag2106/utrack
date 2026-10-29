package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock represents an acquired file lock ensuring single instance execution.
type Lock struct {
	file *os.File
}

// Acquire tries to acquire an exclusive non-blocking lock on path.
// If the lock is held by another process, it returns an error.
func Acquire(path string) (*Lock, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create lock dir %s: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another instance of utrack daemon is already running (locked %s)", path)
	}

	// Write PID for debugging / status verification
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Sync()

	return &Lock{file: f}, nil
}

// Release closes and unlocks the file lock.
func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	defer func() {
		_ = l.file.Close()
		l.file = nil
	}()
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
