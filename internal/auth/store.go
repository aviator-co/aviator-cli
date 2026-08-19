package auth

import (
	"encoding/json"

	"emperror.dev/errors"
	"github.com/zalando/go-keyring"
)

const keyringService = "aviator-cli"

// ErrNoSession is returned when no OAuth session is stored for a host.
var ErrNoSession = errors.Sentinel("no stored Aviator session")

// ErrKeyringUnavailable is returned when the OS secret store can't be reached,
// e.g. a headless Linux box with no secret service running.
var ErrKeyringUnavailable = errors.Sentinel(
	"cannot access the system keychain to store Aviator credentials; set AVIATOR_API_TOKEN instead",
)

// errCorruptEntry marks a keychain entry that can't be decoded.
var errCorruptEntry = errors.Sentinel("the stored Aviator credentials could not be decoded")

// Store persists an OAuth session per API host.
type Store interface {
	Load(host string) (*Session, error)
	Save(host string, session *Session) error
	Delete(host string) error
}

// keyringStore stores credentials in the OS keychain.
type keyringStore struct{}

// DefaultStore returns the keychain-backed store.
func DefaultStore() Store { return keyringStore{} }

func sessionKey(host string) string { return "session:" + normalizeHost(host) }

func (keyringStore) Load(host string) (*Session, error) {
	var session Session
	if err := get(sessionKey(host), &session); err != nil {
		return nil, discardCorrupt(sessionKey(host), err)
	}
	return &session, nil
}

func (keyringStore) Save(host string, session *Session) error {
	return set(sessionKey(host), session)
}

func (keyringStore) Delete(host string) error {
	return remove(sessionKey(host))
}

func get(key string, out any) error {
	raw, err := keyring.Get(keyringService, key)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNoSession
	case err != nil:
		return keyringUnavailable(err)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return errors.Wrap(errCorruptEntry, err.Error())
	}
	return nil
}

func set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return errors.Wrap(err, "failed to encode the Aviator credentials")
	}
	if err := keyring.Set(keyringService, key, string(raw)); err != nil {
		return keyringUnavailable(err)
	}
	return nil
}

func remove(key string) error {
	err := keyring.Delete(keyringService, key)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNoSession
	case err != nil:
		return keyringUnavailable(err)
	}
	return nil
}

// discardCorrupt drops an entry the CLI can no longer decode and reports it as
// missing, so `aviator login` can repair a store it would otherwise fail on.
func discardCorrupt(key string, err error) error {
	if !errors.Is(err, errCorruptEntry) {
		return err
	}
	_ = keyring.Delete(keyringService, key)
	return ErrNoSession
}

// keyringError leads with the actionable guidance while keeping the underlying
// OS error inspectable, so both errors.Is(ErrKeyringUnavailable) and an
// errors.As on the platform error still match.
type keyringError struct{ err error }

func (e keyringError) Error() string {
	return ErrKeyringUnavailable.Error() + ": " + e.err.Error()
}

func (e keyringError) Unwrap() []error { return []error{ErrKeyringUnavailable, e.err} }

func keyringUnavailable(err error) error { return keyringError{err: err} }
