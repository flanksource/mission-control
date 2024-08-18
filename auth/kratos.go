package auth

import (
	gocontext "context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/rand"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// kratosLoginWithCache is a wrapper around kratosLogin and adds a cache layer
func (k *kratosMiddleware) kratosLoginWithCache(ctx gocontext.Context, username, password string) (*client.Session, error) {
	cacheKey := basicAuthCacheKey(username, k.basicAuthSeparator, password)

	if val, found := k.authSessionCache.Get(cacheKey); found {
		if sessCache, ok := val.(*sessionCache); ok {
			if sessCache.err != nil {
				return nil, sessCache.err
			}

			return sessCache.session, nil
		}

		logger.Errorf("unexpected value found in auth cache. It is of type [%T]", val)
	}

	session, err := k.kratosLogin(ctx, username, password)
	if err != nil {
		// Cache login failure as well
		if err := k.authSessionCache.Add(cacheKey, &sessionCache{err: err}, time.Minute); err != nil {
			logger.Errorf("failed to cache login failure (username=%s): %s", username, err)
		}

		return nil, fmt.Errorf("failed to login: %w", err)
	}

	if err := k.authSessionCache.Add(cacheKey, &sessionCache{session: session}, cache.DefaultExpiration); err != nil {
		logger.Errorf("failed to cache session (username=%s): %s", username, err)
	}

	return session, nil
}

// kratosLogin performs login with password
func (k *kratosMiddleware) kratosLogin(ctx gocontext.Context, username, password string) (*client.Session, error) {
	loginFlow, _, err := k.client.FrontendApi.CreateNativeLoginFlow(ctx).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to create native login flow: %w", err)
	}

	updateLoginFlowBody := client.UpdateLoginFlowBody{UpdateLoginFlowWithPasswordMethod: client.NewUpdateLoginFlowWithPasswordMethod(username, "password", password)}
	login, _, err := k.client.FrontendApi.UpdateLoginFlow(ctx).Flow(loginFlow.Id).UpdateLoginFlowBody(updateLoginFlowBody).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to update native login flow: %w", err)
	}

	return &login.Session, nil
}

func (k *kratosMiddleware) validateSession(ctx context.Context, r *http.Request) (*client.Session, error) {
	// Skip all kratos calls
	if strings.HasPrefix(r.URL.Path, "/kratos") {
		activeSession := true
		return &client.Session{Active: &activeSession}, nil
	}

	if username, password, ok := r.BasicAuth(); ok {
		// logger.V(4).Infof("Logging in with Basic %s %s%s%s", username, password[0:1], strings.Repeat("*", len(password)-2), password[len(password)-1:])
		if strings.ToLower(username) == "token" {
			accessToken, err := getAccessToken(ctx, password)
			if err != nil {
				return nil, err
			} else if accessToken == nil {
				return &client.Session{Active: lo.ToPtr(false)}, nil
			}

			var agent models.Agent
			if err := ctx.DB().Where("person_id = ?", accessToken.PersonID.String()).Find(&agent).Error; err != nil {
				return nil, err
			}

			s := &client.Session{
				Id:        uuid.NewString(),
				Active:    lo.ToPtr(true),
				ExpiresAt: accessToken.ExpiresAt,
				Identity: client.Identity{
					Id: accessToken.PersonID.String(),
					Traits: map[string]any{
						"agent": agent,
						"name": map[string]string{
							"first": accessToken.Name,
							"last":  "",
						},
					},
				},
			}

			return s, nil
		}

		sess, err := k.kratosLoginWithCache(r.Context(), username, password)
		if err != nil {
			logger.V(4).Infof("Login failed: %v", err)
			return nil, fmt.Errorf("failed to login: %w", err)
		}

		return sess, nil
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

func (k *kratosMiddleware) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if canSkipAuth(c) {
			return next(c)
		}

		ctx := c.Request().Context().(context.Context)
		session, err := k.validateSession(ctx, c.Request())
		if err != nil {
			ctx.GetSpan().RecordError(err)
			if errors.Is(err, errInvalidTokenFormat) {
				return c.String(http.StatusBadRequest, "invalid access token")
			} else if errors.Is(err, errTokenExpired) {
				return c.String(http.StatusUnauthorized, "access token has expired")
			}
			return c.String(http.StatusUnauthorized, "Authorization Error")
		}

		if !*session.Active {
			return c.String(http.StatusUnauthorized, "Session Expired")
		}

		uid, err := uuid.Parse(session.Identity.GetId())
		if err != nil {
			ctx.GetSpan().RecordError(err)
			return c.String(http.StatusUnauthorized, "Authorization Error")
		}

		person := models.Person{ID: uid}

		if traits, ok := session.Identity.GetTraits().(map[string]any); ok {
			if e, ok := traits["email"].(string); ok {
				person.Email = e
			}

			switch v := traits["name"].(type) {
			case map[string]string:
				person.Name = fmt.Sprintf("%s %s", v["first"], v["last"])
			}

			if agent, ok := traits["agent"].(models.Agent); ok {
				ctx = ctx.WithAgent(agent)
			}
		}

		if IdentityRoleMapper != "" {
			if err := mapIDsToRoles(ctx, session, uid); err != nil {
				ctx.GetSpan().RecordError(err)
				logger.Errorf("error mapping ids to roles: %v", err)
			}
		}

		if err := InjectToken(ctx, c, &person, session.Identity.GetId()); err != nil {
			return err
		}

		ctx = ctx.WithUser(&person)
		c.SetRequest(c.Request().WithContext(ctx))

		return next(c)
	}
}

type kratosMiddleware struct {
	client             *client.APIClient
	tokenCache         *cache.Cache
	accessTokenCache   *cache.Cache
	authSessionCache   *cache.Cache
	basicAuthSeparator string
	db                 *gorm.DB
}

func (k *KratosHandler) KratosMiddleware(ctx context.Context) (*kratosMiddleware, error) {
	randString, err := rand.GenerateRandString(30)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random string: %w", err)
	}

	return &kratosMiddleware{
		db:                 ctx.DB(),
		client:             k.client,
		tokenCache:         cache.New(3*24*time.Hour, 12*time.Hour),
		accessTokenCache:   cache.New(3*24*time.Hour, 24*time.Hour),
		authSessionCache:   cache.New(30*time.Minute, time.Hour),
		basicAuthSeparator: randString,
	}, nil
}
