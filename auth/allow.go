package auth

import (
	"context"
	"net/http"
)

// AllowAuth creates an authenticator that permits any access. This is used
// for goma client/server setups that do not require any authentication.
type AllowAuth struct{}

// Auth always returns success
func (a AllowAuth) Auth(ctx context.Context, req *http.Request) (context.Context, error) {
	return ctx, nil
}
