package auth

import (
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	incAPI "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth/basic_static"
	"github.com/flanksource/incident-commander/auth/oidc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/tg123/go-htpasswd"
)

var (
	HtpasswdFile       string
	OIDCEnabled        bool
	OIDCSigningKeyPath string

	checker       *htpasswd.File
	localhostOnly bool
)

const basicAuthCookieName = "authorization"

func UseBasic(e *echo.Echo) {
	logger.Infof("Using basic authentication with htpasswd file: %s", HtpasswdFile)
	checker = nil
	localhostOnly = false
	var err error
	checker, err = htpasswd.New(HtpasswdFile, htpasswd.DefaultSystems, nil)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warnf("htpasswd file %s not found, only localhost requests will be allowed", HtpasswdFile)
			localhostOnly = true
		} else {
			panic(err)
		}
	}

	e.POST("/auth/login", BasicLogin)
	e.GET("/auth/basic/login", ShowBasicLoginForm)
	e.GET("/auth/logout", BasicLogout)
	e.POST("/auth/logout", BasicLogout)

	// The basic login page references /oidc/static/{tailwind.min.js,logo.svg}.
	// oidc.MountRoutes only runs when OIDCEnabled=true, so mount the static
	// assets here too so they're reachable on a plain basic-auth deployment.
	oidc.RegisterStaticAssets(e)

	e.Use(basicAuthMiddleware)
}

// ShowBasicLoginForm renders the HTML sign-in page. Safe to call without
// credentials — it is registered under /auth/basic/ which is skipped from
// the auth middleware.
func ShowBasicLoginForm(c echo.Context) error {
	next := sanitizeNext(c.QueryParam("next"))
	return c.HTML(http.StatusOK, fmt.Sprintf(basic_static.LoginHTML, html.EscapeString(next), ""))
}

// BasicLogout clears the auth cookie and redirects HTML clients to the login
// form; JSON clients receive a 200 message.
func BasicLogout(c echo.Context) error {
	c.SetCookie(&http.Cookie{
		Name:     basicAuthCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	if wantsHTML(c) {
		return c.Redirect(http.StatusFound, "/auth/basic/login")
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "logged out"})
}

// wantsHTML reports whether the client prefers an HTML response — used to
// decide between rendering the login page and returning 401 JSON.
func wantsHTML(c echo.Context) bool {
	accept := c.Request().Header.Get("Accept")
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		ct := strings.TrimSpace(part)
		if i := strings.IndexByte(ct, ';'); i >= 0 {
			ct = ct[:i]
		}
		if ct == "text/html" {
			return true
		}
	}
	return false
}

// sanitizeNext strips dangerous redirect targets (external URLs, schemes) and
// returns a safe relative path. Falls back to /ui when the input is empty or
// rejected.
func sanitizeNext(raw string) string {
	if raw == "" {
		return "/ui"
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/ui"
	}
	if _, err := url.Parse(raw); err != nil {
		return "/ui"
	}
	return raw
}

func isLocalhostRequest(c echo.Context) bool {
	// Use RemoteAddr to avoid spoofing via X-Forwarded-For headers
	host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
	if err != nil {
		return false
	}
	ip := host
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

func authenticateFromCookie(c echo.Context) bool {
	cookie, err := c.Request().Cookie(basicAuthCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}

	config := api.DefaultConfig
	token, err := jwt.Parse(cookie.Value, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(config.Postgrest.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return false
	}

	userID, ok := claims["id"].(string)
	if !ok || userID == "" {
		return false
	}

	ctx := c.Request().Context().(context.Context)
	var person models.Person
	if err := ctx.DB().Where("id = ?", userID).First(&person).Error; err != nil {
		return false
	}

	if err := InjectToken(ctx, c, &person, ""); err != nil {
		return false
	}

	ctx = ctx.WithUser(&person)
	c.SetRequest(c.Request().WithContext(ctx))
	return true
}

func validateBasicAuth(c echo.Context, user, pass string) (bool, error) {
	if localhostOnly || checker == nil {
		return false, nil
	}
	logger.Tracef("authenticating user %s:%s via htpasswd", user, logger.PrintableSecret(pass))
	if !checker.Match(user, pass) {
		return false, nil
	}

	ctx := c.Request().Context().(context.Context)
	person, err := lookupPerson(ctx, user)
	if err != nil {
		return false, nil
	}

	if err := InjectToken(ctx, c, person, ""); err != nil {
		return false, err
	}

	ctx = ctx.WithUser(person)
	c.SetRequest(c.Request().WithContext(ctx))
	return true, nil
}

// setWWWAuthenticate adds the WWW-Authenticate header pointing to the OAuth
// Protected Resource Metadata endpoint so that MCP clients can discover the
// OIDC provider and initiate the authorization flow.
// rejectUnauthenticated sends the right unauthenticated response for the
// caller. Browsers (Accept: text/html) are redirected to the login form with
// ?next= pointing back at the original URL so they resume where they left
// off; JSON/API clients get a 401 as before.
func rejectUnauthenticated(c echo.Context) error {
	if wantsHTML(c) {
		next := c.Request().URL.RequestURI()
		return c.Redirect(http.StatusFound, "/auth/basic/login?next="+url.QueryEscape(next))
	}
	setWWWAuthenticate(c)
	return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

func setWWWAuthenticate(c echo.Context) {
	if OIDCEnabled {
		resourceMetadataURL := strings.TrimRight(incAPI.PublicURL, "/") + "/.well-known/oauth-protected-resource"
		c.Response().Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s"`, resourceMetadataURL))
	}
}

func basicAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if canSkipAuth(c) || authenticateFromCookie(c) {
			return next(c)
		}

		if localhostOnly {
			if isLocalhostRequest(c) {
				return next(c)
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "only localhost requests are allowed when htpasswd file is not configured"})
		}

		if token, ok := extractBearerAuthToken(c.Request().Header); ok {
			if authenticated, err := authenticateOIDCToken(c, token); err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
			} else if authenticated {
				return next(c)
			}
			// fall through to access token check
			if accessToken, err := getAccessToken(c.Request().Context().(context.Context), token); err == nil && accessToken != nil {
				ctx := c.Request().Context().(context.Context)
				var person models.Person
				if err := ctx.DB().Where("id = ?", accessToken.PersonID).First(&person).Error; err == nil {
					if err := InjectToken(ctx, c, &person, ""); err == nil {
						ctx = ctx.WithUser(&person)
						c.SetRequest(c.Request().WithContext(ctx))
						return next(c)
					}
				}
			}
			return rejectUnauthenticated(c)
		}

		user, pass, ok := c.Request().BasicAuth()
		if !ok {
			return rejectUnauthenticated(c)
		}

		if valid, err := validateBasicAuth(c, user, pass); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		} else if !valid {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		}

		return next(c)
	}
}

// LookupPersonByUsername finds a person by username or email, returning the person ID.
// Used by the OIDC login handler to map authenticated users to persons.
func LookupPersonByUsername(ctx context.Context, username string) (string, error) {
	person, err := lookupPerson(ctx, username)
	if err != nil {
		return "", err
	}
	return person.ID.String(), nil
}

// HtpasswdChecker wraps htpasswd.File for password-based OIDC login.
type HtpasswdChecker struct {
	file *htpasswd.File
}

func NewHtpasswdChecker(path string) (*HtpasswdChecker, error) {
	f, err := htpasswd.New(path, htpasswd.DefaultSystems, nil)
	if err != nil {
		return nil, err
	}
	return &HtpasswdChecker{file: f}, nil
}

func (h *HtpasswdChecker) Match(ctx context.Context, user, pass string) error {
	match := h.file.Match(user, pass)
	if !match {
		return fmt.Errorf("invalid credentials")
	}
	return nil
}

func lookupPerson(ctx context.Context, user string) (*models.Person, error) {
	user = strings.ToLower(user)
	var person models.Person
	if err := ctx.DB().Where("LOWER(name) = ? or LOWER(email) = ?", user, user).First(&person).Error; err != nil {
		logger.Warnf("user authenticated via htpasswd, but not found in the db: %s", user)
		return nil, err
	}
	return &person, nil
}

type basicLoginRequest struct {
	Username string `json:"username" form:"username"`
	Password string `json:"password" form:"password"`
}

func BasicLogin(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	if localhostOnly {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "basic login is unavailable when htpasswd file is not configured"})
	}

	next := sanitizeNext(c.FormValue("next"))

	loginErr := func(status int, msg string) error {
		if wantsHTML(c) {
			return c.HTML(status, fmt.Sprintf(basic_static.LoginHTML,
				html.EscapeString(next),
				`<p class="mt-3 text-sm text-red-600">`+html.EscapeString(msg)+`</p>`))
		}
		return c.JSON(status, map[string]string{"error": msg})
	}

	username, password, ok := extractBasicLoginCredentials(c)
	if !ok {
		return loginErr(http.StatusBadRequest, "credentials required")
	}

	if !checker.Match(username, password) {
		return loginErr(http.StatusUnauthorized, "invalid credentials")
	}

	person, err := lookupPerson(ctx, username)
	if err != nil {
		return loginErr(http.StatusUnauthorized, "user not found in database")
	}

	token, err := GetOrCreateJWTToken(ctx, person, "")
	if err != nil {
		return loginErr(http.StatusInternalServerError, "failed to generate token")
	}

	c.SetCookie(&http.Cookie{
		Name:     basicAuthCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	AddLoginContext(c, person)

	if wantsHTML(c) {
		return c.Redirect(http.StatusFound, next)
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "logged in"})
}

func extractBasicLoginCredentials(c echo.Context) (string, string, bool) {
	if user, pass, ok := c.Request().BasicAuth(); ok {
		return user, pass, true
	}

	contentType := c.Request().Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var req basicLoginRequest
		if err := json.NewDecoder(c.Request().Body).Decode(&req); err == nil && req.Username != "" && req.Password != "" {
			return req.Username, req.Password, true
		}
	}

	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		if err := c.Request().ParseForm(); err == nil {
			user := c.Request().FormValue("username")
			pass := c.Request().FormValue("password")
			if user != "" && pass != "" {
				return user, pass, true
			}
		}
	}

	return "", "", false
}
