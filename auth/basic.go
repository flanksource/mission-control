package auth

import (
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/tg123/go-htpasswd"
)

var HtpasswdFile string

var checker *htpasswd.File

func UseBasic(e *echo.Echo) {
	var err error
	checker, err = htpasswd.New(HtpasswdFile, htpasswd.DefaultSystems, nil)
	if err != nil {
		panic(err)
	}

	e.Use(middleware.BasicAuthWithConfig(middleware.BasicAuthConfig{
		Skipper: canSkipAuth,
		Realm:   "Mission Control",
		Validator: func(user, pass string, c echo.Context) (bool, error) {
			if !checker.Match(user, pass) {
				return false, nil
			}

			ctx := c.Request().Context().(context.Context)
			user = strings.ToLower(user)
			var person models.Person
			if err := ctx.DB().Where("LOWER(name) = ? or LOWER(email) = ?", user, user).First(&person).Error; err != nil {
				logger.Warnf("user authenticated via htpasswd, but not found in the db: %s", user)
				return false, c.String(http.StatusUnauthorized, "User not found")
			}

			if err := InjectToken(ctx, c, &person, ""); err != nil {
				return false, err
			}

			ctx = ctx.WithUser(&person)

			c.SetRequest(c.Request().WithContext(ctx))

			return true, nil
		},
	}))
}
