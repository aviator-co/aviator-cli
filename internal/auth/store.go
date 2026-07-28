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

// Store persists OAuth sessions and client registrations per API host.
type Store interface {
	Load(host string) (*Session, error)
	Save(host string, session *Session) error
	Delete(host string) error
	LoadClient(host string) (*ClientRegistration, error)
	SaveClient(host string, reg *ClientRegistration) error
}

// KeyringStore stores credentials in the OS keychain.
type KeyringStore struct{}

// DefaultStore returns the keychain-backed store.
func DefaultStore() Store { return KeyringStore{} }

func sessionKey(host string) string { return "session:" + normalizeHost(host) }

func clientKey(host string) string { return "client:" + normalizeHost(host) }

func (KeyringStore) Load(host string) (*Session, error) {
	var session Session
	if err := get(sessionKey(host), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (KeyringStore) Save(host string, session *Session) error {
	return set(sessionKey(host), session)
}

func (KeyringStore) Delete(host string) error {
	err := keyring.Delete(keyringService, sessionKey(host))
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNoSession
	case err != nil:
		return errors.Wrap(ErrKeyringUnavailable, err.Error())
	}
	return nil
}

func (KeyringStore) LoadClient(host string) (*ClientRegistration, error) {
	var reg ClientRegistration
	if err := get(clientKey(host), &reg); err != nil {
		return nil, err
	}
	return &reg, nil
}

func (KeyringStore) SaveClient(host string, reg *ClientRegistration) error {
	return set(clientKey(host), reg)
}

func get(key string, out any) error {
	raw, err := keyring.Get(keyringService, key)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNoSession
	case err != nil:
		return errors.Wrap(ErrKeyringUnavailable, err.Error())
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return errors.Wrap(err, "failed to decode the stored Aviator credentials")
	}
	return nil
}

func set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return errors.Wrap(err, "failed to encode the Aviator credentials")
	}
	if err := keyring.Set(keyringService, key, string(raw)); err != nil {
		return errors.Wrap(ErrKeyringUnavailable, err.Error())
	}
	return nil
}
