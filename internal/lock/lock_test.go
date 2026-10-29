package lock_test

import (
	"path/filepath"
	"testing"

	"utrack/internal/lock"
)

func TestSingleInstanceLock(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "test.lock")

	l1, err := lock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire on the same path must fail
	l2, err := lock.Acquire(lockPath)
	if err == nil {
		_ = l2.Release()
		t.Fatalf("second acquire expected to fail, but succeeded")
	}

	// Release first lock
	if err := l1.Release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	// Now acquire should succeed again
	l3, err := lock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
	_ = l3.Release()
}
