package oidc

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// MCPResourcePath is the only route dynamically registered OAuth clients may access.
const MCPResourcePath = "/mcp"

type resourceIndicatorContextKey struct{}

type oauthErrorResponse struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

// MCPResourceURL returns the audience used for MCP access tokens.
func MCPResourceURL(issuerURL string) string {
	return strings.TrimRight(issuerURL, "/") + MCPResourcePath
}

// withMCPResourceIndicator validates the resource before Zitadel discards it.
func withMCPResourceIndicator(next http.Handler, issuerURL string) http.Handler {
	expectedResource := MCPResourceURL(issuerURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "authorization request cannot be parsed")
			return
		}
		if !IsDynamicClient(r.Form.Get("client_id")) {
			next.ServeHTTP(w, r)
			return
		}

		resources, ok := r.Form["resource"]
		if !ok || len(resources) != 1 || resources[0] != expectedResource {
			writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource must identify the MCP endpoint")
			return
		}

		ctx := gocontext.WithValue(r.Context(), resourceIndicatorContextKey{}, expectedResource)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func resourceIndicatorFromContext(ctx gocontext.Context) string {
	resource, _ := ctx.Value(resourceIndicatorContextKey{}).(string)
	return resource
}

// mcpTokenEndpointHandler enforces the MCP resource on code and refresh exchanges.
func mcpTokenEndpointHandler(next http.Handler, storage *Storage, issuerURL string) http.Handler {
	expectedResource := MCPResourceURL(issuerURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				setOAuthCORSHeaders(w.Header())
				writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token request cannot be parsed")
				return
			}
		}
		if err := storage.validateTokenResource(r, expectedResource); err != nil {
			setOAuthCORSHeaders(w.Header())
			writeOAuthError(w, http.StatusBadRequest, "invalid_target", err.Error())
			return
		}

		rec := &bufferedResponseWriter{headers: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		for key, values := range rec.headers {
			w.Header()[key] = append([]string(nil), values...)
		}
		setOAuthCORSHeaders(w.Header())
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	})
}

func (s *Storage) validateTokenResource(r *http.Request, expectedResource string) error {
	if r.Method != http.MethodPost {
		return nil
	}

	var clientID, resource string
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		request, err := s.AuthRequestByCode(r.Context(), r.Form.Get("code"))
		if err != nil {
			return nil
		}
		authRequest, ok := request.(*AuthRequest)
		if !ok {
			return nil
		}
		clientID, resource = authRequest.ClientID, authRequest.Resource
	case "refresh_token":
		request, err := s.TokenRequestByRefreshToken(r.Context(), r.Form.Get("refresh_token"))
		if err != nil {
			return nil
		}
		refreshToken, ok := request.(*RefreshToken)
		if !ok {
			return nil
		}
		clientID, resource = refreshToken.ClientID, refreshToken.Resource
	default:
		return nil
	}

	if !IsDynamicClient(clientID) {
		return nil
	}
	if resource != expectedResource {
		return fmt.Errorf("authorization is not valid for the MCP resource")
	}
	if resources, ok := r.Form["resource"]; ok && (len(resources) != 1 || resources[0] != resource) {
		return fmt.Errorf("requested resource does not match the authorized MCP resource")
	}
	return nil
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(oauthErrorResponse{Error: code, Description: description})
}
