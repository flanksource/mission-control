package utils

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
)

type kratosMiddleware struct {
	client *client.APIClient
}

func KratosMiddleware(kratosAdminAPI string) *kratosMiddleware {
	configuration := client.NewConfiguration()
	configuration.Servers = []client.ServerConfiguration{
		{
			URL: kratosAdminAPI, // Kratos Admin API
		},
	}
	return &kratosMiddleware{
		client: client.NewAPIClient(configuration),
	}
}
func (k *kratosMiddleware) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		session, err := k.validateSession(c.Request())
		if err != nil {
			return c.Redirect(http.StatusMovedPermanently, "/login")
		}
		if !*session.Active {
			return c.Redirect(http.StatusMovedPermanently, "/login")
		}
		return next(c)
	}
}
func (k *kratosMiddleware) validateSession(r *http.Request) (*client.Session, error) {
	cookie, err := r.Cookie("ory_kratos_session")
	if err != nil {
		return nil, err
	}
	if cookie == nil {
		return nil, errors.New("no session found in cookie")
	}
	resp, _, err := k.client.V0alpha2Api.ToSession(context.Background()).Cookie(cookie.String()).Execute()
	if err != nil {
		return nil, err
	}
	return resp, nil
}
