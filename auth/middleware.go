package auth

import (
	"context"
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
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/utils"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	client "github.com/ory/client-go"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
)

const (
	DefaultPostgrestRole = "postgrest_api"
	UserIDHeaderKey      = "X-User-ID"
)

type kratosMiddleware struct {
	client             *client.APIClient
	jwtSecret          string
	tokenCache         *cache.Cache
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
		db:                 k.db,
		jwtSecret:          k.jwtSecret,
		tokenCache:         cache.New(3*24*time.Hour, 12*time.Hour),
		authSessionCache:   cache.New(30*time.Minute, time.Hour),
		basicAuthSeparator: randString,
	}, nil
}

var skipAuthPaths = []string{"/health", "/metrics"}

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
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}
		if !*session.Active {
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}

		token, err := k.getDBToken(session.Id, session.Identity.GetId())
		if err != nil {
			logger.Errorf("Error generating JWT Token: %v", err)
			return c.String(http.StatusUnauthorized, "Unauthorized")
		}
		c.Request().Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
		c.Request().Header.Add(UserIDHeaderKey, session.Identity.GetId())

		return next(c)
	}
}

var errInvalidTokenFormat = errors.New("invalid token format")

func (k *kratosMiddleware) getAccessToken(ctx context.Context, token string) (*models.AccessToken, error) {
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

	hash := argon2.IDKey([]byte(password), []byte(salt), uint32(timeCost), uint32(memoryCost), uint8(parallelism), 32)
	encodedHash := base64.RawStdEncoding.EncodeToString(hash)

	query := `SELECT access_tokens.* FROM access_tokens WHERE value = ?`
	var acessToken models.AccessToken
	if err := k.db.Raw(query, encodedHash).First(&acessToken).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

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
				return nil, fmt.Errorf("failed to validate agent: %w", err)
			} else if accessToken == nil {
				return &client.Session{Active: utils.Ptr(false)}, nil
			}

			if accessToken.ExpiresAt.Before(time.Now()) {
				return &client.Session{Active: utils.Ptr(false)}, nil
			}

			s := &client.Session{
				Id:     uuid.NewString(),
				Active: utils.Ptr(true),
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
func (k *kratosMiddleware) kratosLoginWithCache(ctx context.Context, username, password string) (*client.Session, error) {
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
func (k *kratosMiddleware) kratosLogin(ctx context.Context, username, password string) (*client.Session, error) {
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

func basicAuthCacheKey(username, separator, password string) string {
	return hash.Sha256Hex(fmt.Sprintf("%s:%s:%s", username, separator, password))
}
