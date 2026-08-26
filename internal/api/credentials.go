package api

import (
	"context"

	"emperror.dev/errors"
	"github.com/aviator-co/aviator-cli/internal/auth"
	"github.com/aviator-co/aviator-cli/internal/config"
)

// ErrNoAPIToken is returned when no Aviator credentials are available.
var ErrNoAPIToken = errors.Sentinel(
	"no Aviator credentials found; run `aviator login`, " +
		"or set AVIATOR_API_TOKEN for a static API token",
)

// TokenSource supplies the bearer token for each request.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// refresher is a TokenSource that can replace a token the server has rejected.
// Static tokens don't implement it, so they are never retried.
type refresher interface {
	ForceRefresh(ctx context.Context, rejected string) (string, error)
}

// staticToken is a token that never expires, e.g. one from the config file.
type staticToken string

func (t staticToken) Token(context.Context) (string, error) { return string(t), nil }

// HasCredentials reports whether any Aviator credentials are available.
func HasCredentials() bool {
	_, err := resolveTokenSource()
	return err == nil
}

// resolveTokenSource picks credentials in precedence order: a statically
// configured token (environment or config file) first, then the stored OAuth
// session.
func resolveTokenSource() (TokenSource, error) {
	if token := config.Av.Aviator.APIToken; token != "" {
		return staticToken(token), nil
	}
	source, err := auth.NewTokenSource(config.Av.Aviator.APIHost, auth.DefaultStore(), nil)
	if errors.Is(err, auth.ErrNoSession) {
		return nil, ErrNoAPIToken
	}
	if err != nil {
		return nil, err
	}
	return source, nil
}
