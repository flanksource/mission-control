package auth

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/rbac"
)

// accessTokenTrait holds the access token a Kratos session was built from.
const accessTokenTrait = "access_token"

// withAccessToken marks the request as made with the access token, whose Roles then limit what the request's user,
// the token's owner, may do (rbac.TokenLimit).
func withAccessToken(ctx context.Context, token *models.AccessToken) context.Context {
	return rbac.WithAccessToken(ctx, token)
}

// madeWithAccessToken reports whether the request was authenticated by an access token.
func madeWithAccessToken(ctx context.Context) bool {
	return rbac.MadeWithAccessToken(ctx)
}
