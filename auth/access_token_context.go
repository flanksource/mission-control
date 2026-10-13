package auth

import (
	"github.com/flanksource/duty/context"
)

type accessTokenKey struct{}

// accessTokenTrait marks a Kratos session built from an access token.
const accessTokenTrait = "access_token"

// withAccessToken marks the request as made with an access token.
func withAccessToken(ctx context.Context) context.Context {
	return ctx.WithValue(accessTokenKey{}, true)
}

// madeWithAccessToken reports whether the request was authenticated by an access token.
func madeWithAccessToken(ctx context.Context) bool {
	v, _ := ctx.Value(accessTokenKey{}).(bool)
	return v
}
