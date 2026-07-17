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

var _ = ginkgo.Describe("sanitizeNext", func() {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty falls back to /ui", input: "", expected: "/ui"},
		{name: "simple relative path", input: "/ui/topology", expected: "/ui/topology"},
		{name: "relative path with query", input: "/ui/topology?id=1", expected: "/ui/topology?id=1"},
		{name: "protocol-relative // is rejected", input: "//evil.com", expected: "/ui"},
		{name: "backslash-relative /\\ is rejected", input: `/\evil.com`, expected: "/ui"},
		{name: "backslash-relative /\\/ is rejected", input: `/\/evil.com`, expected: "/ui"},
		{name: "absolute http URL is rejected", input: "http://evil.com", expected: "/ui"},
		{name: "absolute https URL is rejected", input: "https://evil.com", expected: "/ui"},
		{name: "scheme-only target is rejected", input: "javascript:alert(1)", expected: "/ui"},
		{name: "path not starting with slash is rejected", input: "evil.com", expected: "/ui"},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			Expect(sanitizeNext(tt.input)).To(Equal(tt.expected))
		})
	}
})
