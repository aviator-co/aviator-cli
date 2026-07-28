package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// The server rotates refresh tokens and revokes the whole token family if one
// is presented twice, so two processes refreshing at once would log the user
// out. The lock file holds no secrets.
func lockRefresh(ctx context.Context, host string) (func(), error) {
	path, err := refreshLockPath(host)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, errors.Wrap(err, "failed to create the Aviator cache directory")
	}

	ctx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()

	lock := flock.New(path)
	locked, err := lock.TryLockContext(ctx, lockRetryDelay)
	if err != nil {
		return nil, errors.Wrap(err, "failed to lock the Aviator credential store")
	}
	if !locked {
		return nil, errors.New("timed out waiting for another aviator process to refresh credentials")
	}
	return func() { _ = lock.Unlock() }, nil
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
