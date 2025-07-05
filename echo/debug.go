package echo

import (
	"bytes"
	"io"
	"strings"

	"github.com/flanksource/commons/logger"
	echov4 "github.com/labstack/echo/v4"
)

// For capturing post body for specific endpoints or entire group
func LogPostDataMiddleware() echov4.MiddlewareFunc {
	return echov4.MiddlewareFunc(func(next echov4.HandlerFunc) echov4.HandlerFunc {
		return func(c echov4.Context) error {
			if c.Request().Method == "POST" {
				// Read the body
				body, err := io.ReadAll(c.Request().Body)
				if err != nil {
					return err
				}

				// Restore the body for next handlers
				c.Request().Body = io.NopCloser(bytes.NewBuffer(body))

				// Log based on content type
				contentType := c.Request().Header.Get("Content-Type")

				if strings.Contains(contentType, "application/json") {
					logger.Infof("JSON Body: %s", string(body))
				} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
					logger.Infof("Form Data: %s", string(body))
				} else if strings.Contains(contentType, "multipart/form-data") {
					// Parse multipart form
					c.Request().ParseMultipartForm(32 << 20) // 32MB max
					logger.Infof("Multipart Form Values: %v", c.Request().MultipartForm.Value)
					logger.Infof("Multipart Form Files: %v", c.Request().MultipartForm.File)
				} else {
					logger.Infof("Raw Body: %s", string(body))
				}
			}

			return next(c)
		}
	})
}
