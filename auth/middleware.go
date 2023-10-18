package auth

import (
	gocontext "context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/hash"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/rand"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
)

const (
	DefaultPostgrestRole = "postgrest_api"
)

var (
	errInvalidTokenFormat = errors.New("invalid access token format")
	errTokenExpired       = errors.New("access token has expired")
)

type kratosMiddleware struct {
	client             *client.APIClient
	jwtSecret          string
	tokenCache         *cache.Cache
	accessTokenCache   *cache.Cache
	authSessionCache   *cache.Cache
	basicAuthSeparator string
	db                 *gorm.DB
}

func (k *KratosHandler) KratosMiddleware() (*kratosMiddleware, error) {
	randString, err := rand.GenerateRandString(30)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random string: %w", err)
	}

	return &kratosMiddleware{
		client:             k.client,
		jwtSecret:          k.jwtSecret,
		tokenCache:         cache.New(3*24*time.Hour, 12*time.Hour),
		accessTokenCache:   cache.New(3*24*time.Hour, 24*time.Hour),
		authSessionCache:   cache.New(30*time.Minute, time.Hour),
		basicAuthSeparator: randString,
	}, nil
}

var skipAuthPaths = []string{"/health", "/metrics", "/kratos/*"}

func canSkipAuth(c echo.Context) bool {
	return collections.Contains(skipAuthPaths, c.Path())
}

func (k *kratosMiddleware) Session(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if canSkipAuth(c) {
			return next(c)
		}
		session, err := k.validateSession(c.Request())
		if err != nil {
			if errors.Is(err, errInvalidTokenFormat) {
				return c.String(http.StatusBadRequest, "invalid access token")
			} else if errors.Is(err, errTokenExpired) {
				return c.String(http.StatusUnauthorized, "access token has expired")
			}

			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		if !*session.Active {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		token, err := getDBToken(k.tokenCache, k.jwtSecret, session.Id, session.Identity.GetId())
		if err != nil {
			logger.Errorf("Error generating JWT Token: %v", err)
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}
		c.Request().Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		c.Request().Header.Set(api.UserIDHeaderKey, session.Identity.GetId())

		ctx := c.Request().Context().(context.Context)
		var email string
		if traits, ok := session.Identity.GetTraits().(map[string]any); ok {
			if e, ok := traits["email"].(string); ok {
				email = e
			}
		}

		if uid, err := uuid.Parse(session.Identity.GetId()); err != nil {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		} else {
			ctx = ctx.WithUser(&models.Person{ID: uid, Email: email})
			c.SetRequest(c.Request().WithContext(ctx))
		}

		return next(c)
	}
}

func (k *kratosMiddleware) getAccessToken(ctx gocontext.Context, token string) (*models.AccessToken, error) {
	if token, ok := k.accessTokenCache.Get(token); ok {
		return token.(*models.AccessToken), nil
	}

	fields := strings.Split(token, ".")
	if len(fields) != 5 {
		return nil, errInvalidTokenFormat
	}

	var (
		password = fields[0]
		salt     = fields[1]
	)

	timeCost, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return nil, errInvalidTokenFormat
	}

	memoryCost, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil {
		return nil, errInvalidTokenFormat
	}

	parallelism, err := strconv.ParseUint(fields[4], 10, 8)
	if err != nil {
		return nil, errInvalidTokenFormat
	}

	hash := argon2.IDKey([]byte(password), []byte(salt), uint32(timeCost), uint32(memoryCost), uint8(parallelism), 20)
	encodedHash := base64.URLEncoding.EncodeToString(hash)

	query := `SELECT access_tokens.* FROM access_tokens WHERE value = ?`
	var acessToken models.AccessToken
	if err := k.db.Raw(query, encodedHash).First(&acessToken).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	k.accessTokenCache.Set(token, &acessToken, time.Until(acessToken.ExpiresAt))

	return &acessToken, nil
}

func (k *kratosMiddleware) validateSession(r *http.Request) (*client.Session, error) {
	// Skip all kratos calls
	if strings.HasPrefix(r.URL.Path, "/kratos") {
		activeSession := true
		return &client.Session{Active: &activeSession}, nil
	}

	if username, password, ok := r.BasicAuth(); ok {
		if username == "TOKEN" {
			accessToken, err := k.getAccessToken(r.Context(), password)
			if err != nil {
				return nil, err
			} else if accessToken == nil {
				return &client.Session{Active: utils.Ptr(false)}, nil
			}

			if accessToken.ExpiresAt.Before(time.Now()) {
				return nil, errTokenExpired
			}

			s := &client.Session{
				Id:        uuid.NewString(),
				Active:    utils.Ptr(true),
				ExpiresAt: &accessToken.ExpiresAt,
				Identity: client.Identity{
					Id: accessToken.PersonID.String(),
				},
			}

			return s, nil
		}

		sess, err := k.kratosLoginWithCache(r.Context(), username, password)
		if err != nil {
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

// sessionCache is used to cache the response of a login attempt.
type sessionCache struct {
	session *client.Session
	err     error
}

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

func basicAuthCacheKey(username, separator, password string) string {
	return hash.Sha256Hex(fmt.Sprintf("%s:%s:%s", username, separator, password))
}
