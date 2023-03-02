package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	"github.com/patrickmn/go-cache"
)

const DefaultPostgrestRole = "postgrest_api"

type kratosMiddleware struct {
	client     *client.APIClient
	jwtSecret  string
	tokenCache *cache.Cache
}

func (k *KratosHandler) KratosMiddleware() *kratosMiddleware {
	return &kratosMiddleware{
		client:     k.client,
		jwtSecret:  k.jwtSecret,
		tokenCache: cache.New(3*24*time.Hour, 12*time.Hour),
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

		token, err := k.getDBToken(session.Id, session.Identity.GetId())
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

	username, password, hasBasicAuth := r.BasicAuth()
	if hasBasicAuth {
		loginFlow, _, err := k.client.FrontendApi.CreateNativeLoginFlow(r.Context()).Execute()
		if err != nil {
			return nil, fmt.Errorf("failed to create native login flow: %w", err)
		}

		updateLoginFlowBody := client.UpdateLoginFlowBody{UpdateLoginFlowWithPasswordMethod: client.NewUpdateLoginFlowWithPasswordMethod(username, "password", password)}
		login, _, err := k.client.FrontendApi.UpdateLoginFlow(r.Context()).Flow(loginFlow.Id).UpdateLoginFlowBody(updateLoginFlowBody).Execute()
		if err != nil {
			return nil, fmt.Errorf("failed to update native login flow: %w", err)
		}

		return &login.Session, nil
	}

	if strings.HasPrefix(r.URL.Path, "/upstream_push") && !hasBasicAuth {
		return nil, errors.New("endpoint requires basic auth")
	}

	cookie, err := r.Cookie("ory_kratos_session")
	if err != nil {
		return nil, err
	}
	if cookie == nil {
		return nil, errors.New("no session found in cookie")
	}

	session, _, err := k.client.FrontendApi.ToSession(r.Context()).Cookie(cookie.String()).Execute()
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

func (k *kratosMiddleware) getDBToken(sessionID, userID string) (string, error) {
	cacheKey := sessionID + userID
	if token, exists := k.tokenCache.Get(cacheKey); exists {
		return token.(string), nil
	}
	// Adding Authorization Token for PostgREST
	token, err := k.generateDBToken(userID)
	if err != nil {
		return "", err
	}
	k.tokenCache.SetDefault(cacheKey, token)
	return token, nil
}
