package clientapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// JWT contains the client-visible claims extracted from a bearer token.
type JWT struct {
	Audience  string    `json:"audience,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	UPN       string    `json:"upn,omitempty"`
	Name      string    `json:"name,omitempty"`
	Scopes    string    `json:"scopes,omitempty"`
	AppID     string    `json:"appid,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Raw       string    `json:"-"`
}

// LocalStorageItem mirrors a browser's per-origin local storage entry.
type LocalStorageItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Cookie is the serializable subset of a browser cookie.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

// Cookies is a collection of browser cookies.
type Cookies []Cookie

// SessionOrigin contains local storage captured for one origin.
type SessionOrigin struct {
	Origin       string             `json:"origin"`
	LocalStorage []LocalStorageItem `json:"localStorage,omitempty"`
}

// PlaywrightSessionState is the portable browser authentication state.
type PlaywrightSessionState struct {
	Cookies Cookies         `json:"cookies" pretty:"table"`
	Origins []SessionOrigin `json:"origins,omitempty"`
	Tokens  []JWT           `json:"tokens,omitempty"`
}

// DecodeJWT extracts the claims needed by client-side connection workflows.
func DecodeJWT(token string) *JWT {
	if token == "" {
		return nil
	}
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return nil
	}
	payload := parts[1]
	if remainder := len(payload) % 4; remainder != 0 {
		payload += strings.Repeat("=", 4-remainder)
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(decoded, &claims) != nil {
		return nil
	}

	result := &JWT{Raw: token}
	if value, ok := claims["aud"].(string); ok {
		result.Audience = value
	} else if values, ok := claims["aud"].([]any); ok && len(values) > 0 {
		result.Audience, _ = values[0].(string)
	}
	result.Subject, _ = claims["sub"].(string)
	result.UPN, _ = claims["upn"].(string)
	result.Name, _ = claims["name"].(string)
	result.Scopes, _ = claims["scp"].(string)
	result.AppID, _ = claims["appid"].(string)
	if expires, ok := claims["exp"].(float64); ok {
		result.ExpiresAt = time.Unix(int64(expires), 0)
	}
	return result
}

// NewPlaywrightSessionState combines browser storage into a portable state.
func NewPlaywrightSessionState(cookies Cookies, sessionStorage map[string]string, origins []SessionOrigin, connectionURL string) PlaywrightSessionState {
	state := PlaywrightSessionState{Cookies: cookies, Origins: origins}
	for key, value := range sessionStorage {
		if !strings.Contains(key, "accesstoken") && !strings.Contains(key, "idtoken") {
			continue
		}
		if token := DecodeJWT(ExtractSecret(value)); token != nil {
			state.Tokens = append(state.Tokens, *token)
		}
	}

	if parsed, err := url.Parse(connectionURL); connectionURL != "" && err == nil && parsed.Host != "" {
		origin := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
		found := false
		for _, existing := range state.Origins {
			if existing.Origin == origin {
				found = true
				break
			}
		}
		if !found {
			state.Origins = append(state.Origins, SessionOrigin{Origin: origin})
		}
	}
	return state
}

// ExtractSecret reads the token secret from an MSAL storage value.
func ExtractSecret(value string) string {
	var entry map[string]any
	if json.Unmarshal([]byte(value), &entry) != nil {
		return ""
	}
	secret, _ := entry["secret"].(string)
	return secret
}
