package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
)

const DefaultPostgrestRole = "postgrest_api"

type kratosMiddleware struct {
	client    *client.APIClient
	jwtSecret string
}

func (k *KratosHandler) KratosMiddleware() *kratosMiddleware {
	return &kratosMiddleware{
		client:    k.client,
		jwtSecret: k.jwtSecret,
	}
}

func (k *kratosMiddleware) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		session, err := k.validateSession(c.Request())
		if err != nil {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}
		if !*session.Active {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		// Adding Authorization Token for PostgREST
		token, err := k.generateDBToken(session.Identity.GetId())
		if err != nil {
			logger.Errorf("Error generating JWT Token: %v", err)
		}
		c.Request().Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

		return next(c)
	}
}
func (k *kratosMiddleware) validateSession(r *http.Request) (*client.Session, error) {
	// Skip all kratos calls
	if strings.HasPrefix(r.URL.Path, "/kratos") {
		activeSession := true
		return &client.Session{Active: &activeSession}, nil
	}

	cookie, err := r.Cookie("ory_kratos_session")
	if err != nil {
		return nil, err
	}
	if cookie == nil {
		return nil, errors.New("no session found in cookie")
	}

	session, _, err := k.client.V0alpha2Api.ToSession(context.Background()).Cookie(cookie.String()).Execute()
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (k *kratosMiddleware) generateDBToken(id string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"role": DefaultPostgrestRole,
		"id":   id,
	})
	return token.SignedString([]byte(k.jwtSecret))
}
