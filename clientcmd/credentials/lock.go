package credentials

import (
	gocontext "context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// LockTimeout bounds how long WithLock waits for another process. A refresh
// takes well under a second, so exceeding this means a stale or wedged holder
// and the user deserves an error rather than a hang.
var LockTimeout = 10 * time.Second

const lockFile = ".lock"

// WithLock runs fn holding an exclusive cross-process lock over dir.
//
// Locking is deliberately separate from Store: a keychain has no lock
// primitive, so the lock is always a file and is shared by every backend. Do
// not nest WithLock calls — flock is per open file description, so a nested
// acquisition in the same process deadlocks against itself.
func WithLock(dir string, fn func() error) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	lock := flock.New(filepath.Join(dir, lockFile))
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), LockTimeout)
	defer cancel()

	locked, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock credential store %s: %w", dir, err)
	}
	if !locked {
		return fmt.Errorf("timed out after %s waiting for the credential store lock at %s", LockTimeout, filepath.Join(dir, lockFile))
	}
	defer func() { _ = lock.Unlock() }()

	return fn()
}
