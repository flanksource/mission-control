package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("extractBasicLoginCredentials", func() {
	tests := []struct {
		name            string
		setup           func() echo.Context
		expectedUser    string
		expectedPass    string
		expectedSuccess bool
	}{
		{
			name: "from Authorization Basic header",
			setup: func() echo.Context {
				e := echo.New()
				req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", nil)
				req.SetBasicAuth("admin", "secret")
				return e.NewContext(req, httptest.NewRecorder())
			},
			expectedUser:    "admin",
			expectedPass:    "secret",
			expectedSuccess: true,
		},
		{
			name: "from JSON body",
			setup: func() echo.Context {
				e := echo.New()
				body := `{"username":"admin","password":"secret"}`
				req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				return e.NewContext(req, httptest.NewRecorder())
			},
			expectedUser:    "admin",
			expectedPass:    "secret",
			expectedSuccess: true,
		},
		{
			name: "from form body",
			setup: func() echo.Context {
				e := echo.New()
				body := "username=admin&password=secret"
				req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return e.NewContext(req, httptest.NewRecorder())
			},
			expectedUser:    "admin",
			expectedPass:    "secret",
			expectedSuccess: true,
		},
		{
			name: "no credentials",
			setup: func() echo.Context {
				e := echo.New()
				req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", nil)
				return e.NewContext(req, httptest.NewRecorder())
			},
			expectedSuccess: false,
		},
		{
			name: "JSON body with missing password",
			setup: func() echo.Context {
				e := echo.New()
				body := `{"username":"admin"}`
				req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				return e.NewContext(req, httptest.NewRecorder())
			},
			expectedSuccess: false,
		},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			c := tt.setup()
			user, pass, ok := extractBasicLoginCredentials(c)
			Expect(ok).To(Equal(tt.expectedSuccess))
			if tt.expectedSuccess {
				Expect(user).To(Equal(tt.expectedUser))
				Expect(pass).To(Equal(tt.expectedPass))
			}
		})
	}
})
