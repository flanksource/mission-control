package oidc

// This file binds an OIDC authorization request to the browser that initiated
// it. The Zitadel OIDC library creates the auth request and redirects to the
// login UI with an auth_request_id in the URL. URL parameters are attacker
// controlled, so completing login from only auth_request_id plus an upstream
// Kratos/Clerk session would allow login CSRF: a victim could be tricked into
// completing an attacker's pending authorization request.
//
// To prevent that, /authorize issues a short-lived, HMAC-signed transaction
// cookie when the auth request is first created. Subsequent login and callback
// steps must present a cookie whose embedded auth_request_id matches the URL.
// The cookie is HttpOnly, SameSite=Lax, per-auth-request, and expires with the
// auth request window.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const oidcTransactionCookieTTL = 10 * time.Minute

var (
	errOIDCTransactionCookieMissing = errors.New("oidc transaction cookie missing")
	errOIDCTransactionCookieInvalid = errors.New("oidc transaction cookie invalid")
)

type transactionCookieManager struct {
	key    [32]byte
	secure bool
	ttl    time.Duration
}

type transactionCookiePayload struct {
	AuthRequestID string `json:"auth_request_id"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
}

func newTransactionCookieManager(key [32]byte, issuerURL string) *transactionCookieManager {
	return &transactionCookieManager{
		key:    key,
		secure: strings.HasPrefix(strings.ToLower(issuerURL), "https://"),
		ttl:    oidcTransactionCookieTTL,
	}
}

func (m *transactionCookieManager) issueOnAuthorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&transactionCookieResponseWriter{ResponseWriter: w, manager: m}, r)
	})
}

func (m *transactionCookieManager) requireOnAuthorizeCallback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing auth request id", http.StatusBadRequest)
			return
		}
		if err := m.validateRequest(r, id); err != nil {
			http.Error(w, "invalid oidc transaction", http.StatusUnauthorized)
			return
		}
		m.clearCookie(w, id)

		next.ServeHTTP(w, r)
	})
}

func (m *transactionCookieManager) validateEcho(c echo.Context, authRequestID string) error {
	if authRequestID == "" {
		return errOIDCTransactionCookieInvalid
	}
	return m.validateRequest(c.Request(), authRequestID)
}

func (m *transactionCookieManager) validateRequest(r *http.Request, authRequestID string) error {
	cookie, err := r.Cookie(m.cookieName(authRequestID))
	if err != nil || cookie.Value == "" {
		return errOIDCTransactionCookieMissing
	}

	payload, err := m.parse(cookie.Value)
	if err != nil {
		return err
	}
	if payload.AuthRequestID != authRequestID {
		return errOIDCTransactionCookieInvalid
	}
	if time.Now().Unix() > payload.ExpiresAt {
		return errOIDCTransactionCookieInvalid
	}
	return nil
}

func (m *transactionCookieManager) issueCookie(w http.ResponseWriter, authRequestID string) {
	token, err := m.sign(authRequestID)
	if err != nil {
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName(authRequestID),
		Value:    token,
		Path:     "/",
		MaxAge:   int(m.ttl.Seconds()),
		Expires:  time.Now().Add(m.ttl),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *transactionCookieManager) clearCookie(w http.ResponseWriter, authRequestID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName(authRequestID),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *transactionCookieManager) sign(authRequestID string) (string, error) {
	now := time.Now()
	payload := transactionCookiePayload{
		AuthRequestID: authRequestID,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(m.ttl).Unix(),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	sig := m.signature(encodedPayload)
	return encodedPayload + "." + sig, nil
}

func (m *transactionCookieManager) parse(token string) (*transactionCookiePayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errOIDCTransactionCookieInvalid
	}

	expectedSig := m.signature(parts[0])
	if !hmac.Equal([]byte(expectedSig), []byte(parts[1])) {
		return nil, errOIDCTransactionCookieInvalid
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errOIDCTransactionCookieInvalid
	}

	var payload transactionCookiePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, errOIDCTransactionCookieInvalid
	}
	if payload.AuthRequestID == "" || payload.ExpiresAt == 0 {
		return nil, errOIDCTransactionCookieInvalid
	}
	return &payload, nil
}

func (m *transactionCookieManager) signature(encodedPayload string) string {
	mac := hmac.New(sha256.New, m.key[:])
	_, _ = mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (m *transactionCookieManager) cookieName(authRequestID string) string {
	prefix := "mc_oidc_tx_"
	if m.secure {
		prefix = "__Host-mc_oidc_tx_"
	}
	sum := sha256.Sum256([]byte(authRequestID))
	return prefix + base64.RawURLEncoding.EncodeToString(sum[:])[:22]
}

type transactionCookieResponseWriter struct {
	http.ResponseWriter
	manager *transactionCookieManager
	wrote   bool
}

func (w *transactionCookieResponseWriter) WriteHeader(statusCode int) {
	if !w.wrote {
		w.wrote = true
		if statusCode >= http.StatusMultipleChoices && statusCode < http.StatusBadRequest {
			if id := authRequestIDFromLoginRedirect(w.Header().Get("Location")); id != "" {
				w.manager.issueCookie(w.ResponseWriter, id)
			}
		}
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *transactionCookieResponseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func authRequestIDFromLoginRedirect(location string) string {
	if location == "" {
		return ""
	}

	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	if u.Path != "/oidc/login" {
		return ""
	}
	return u.Query().Get("auth_request_id")
}
