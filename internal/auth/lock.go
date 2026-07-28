package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"

	"emperror.dev/errors"
	"github.com/gofrs/flock"
)

const (
	lockTimeout    = 30 * time.Second
	lockRetryDelay = 50 * time.Millisecond
)

// lockDirOverride redirects the lock file in tests.
var lockDirOverride string

// lockRefresh serializes token refreshes across concurrent CLI invocations.
// Refresh tokens rotate, so two processes refreshing at once would sign the
// user out. The lock file holds no secrets.
//
// It is best effort. A lock that cannot be taken at all — no usable cache
// directory, e.g. an unset HOME under cron or CI — only warns, since a working
// session is worth more than the guarantee. Losing the race for a lock that
// does work is different, and fails: another refresh being in flight is exactly
// what would sign the user out.
func lockRefresh(ctx context.Context, host string, warn io.Writer) (func(), error) {
	noop := func() {}

	path, err := refreshLockPath(host)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o700)
	}
	if err == nil {
		var lock *flock.Flock
		lock, err = takeLock(ctx, path)
		if err == nil {
			return func() { _ = lock.Unlock() }, nil
		}
		if errors.Is(err, errLockContended) {
			return noop, err
		}
	}
	warnf(warn, "could not lock the Aviator credential store: %v\n"+
		"  refreshing without it; concurrent aviator commands may sign you out\n", err)
	return noop, nil
}

var errLockContended = errors.Sentinel(
	"timed out waiting for another aviator process to refresh credentials",
)

func takeLock(ctx context.Context, path string) (*flock.Flock, error) {
	ctx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()

	lock := flock.New(path)
	locked, err := lock.TryLockContext(ctx, lockRetryDelay)
	switch {
	case err != nil && ctx.Err() == nil:
		return nil, errors.Wrap(err, "failed to lock the Aviator credential store")
	case !locked:
		return nil, errLockContended
	}
	return lock, nil
}

func refreshLockPath(host string) (string, error) {
	dir := lockDirOverride
	if dir == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return "", errors.Wrap(err, "failed to locate the user cache directory")
		}
		dir = filepath.Join(cacheDir, "aviator")
	}
	sum := sha256.Sum256([]byte(normalizeHost(host)))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".refresh.lock"), nil
}
