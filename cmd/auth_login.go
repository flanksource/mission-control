package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in via OIDC browser flow",
	RunE:  runAuthLogin,
}

var loginServer string

func init() {
	authLoginCmd.Flags().StringVar(&loginServer, "server", "", "Mission Control server URL (required)")
	_ = authLoginCmd.MarkFlagRequired("server")
	Auth.AddCommand(authLoginCmd)
}

type oidcTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	serverURL := strings.TrimRight(loginServer, "/")

	// Discover OIDC endpoints
	discoveryURL := serverURL + "/.well-known/openid-configuration"
	endpoints, err := discoverOIDC(discoveryURL)
	if err != nil {
		return fmt.Errorf("OIDC discovery failed: %w", err)
	}

	// Generate PKCE values
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return fmt.Errorf("PKCE generation failed: %w", err)
	}
	state := randomBase64(16)
	nonce := randomBase64(16)

	// Start local callback server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local server: %w", err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server := &http.Server{}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if s := q.Get("state"); s != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			errCh <- fmt.Errorf("state mismatch")
			return
		}
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			http.Error(w, fmt.Sprintf("%s: %s", e, desc), http.StatusBadRequest)
			errCh <- fmt.Errorf("auth error: %s: %s", e, desc)
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("missing authorization code")
			return
		}
		fmt.Fprintf(w, "<html><body><h2>Login successful!</h2><p>You can close this tab.</p></body></html>")
		codeCh <- code
	})

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Build authorize URL
	authURL := fmt.Sprintf("%s?client_id=mc-cli&response_type=code&scope=%s&redirect_uri=%s&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		endpoints.AuthorizationEndpoint,
		url.QueryEscape("openid profile email offline_access"),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
		url.QueryEscape(nonce),
		url.QueryEscape(challenge),
	)

	fmt.Fprintf(cmd.OutOrStdout(), "Opening browser for login...\n%s\n\n", authURL)
	openBrowser(authURL)

	// Wait for callback
	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return err
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("login timed out")
	}

	// Exchange code for tokens
	tokens, err := exchangeCode(endpoints.TokenEndpoint, code, redirectURI, verifier)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	// Validate nonce in ID token
	if err := validateNonce(tokens.IDToken, nonce); err != nil {
		return fmt.Errorf("nonce validation failed: %w", err)
	}

	// Store tokens
	if err := storeTokens(serverURL, tokens); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save tokens: %v\n", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nLogin successful!\n\n")
	fmt.Fprintf(cmd.OutOrStdout(), "Access token:\n%s\n\n", tokens.AccessToken)
	fmt.Fprintf(cmd.OutOrStdout(), "Usage example:\n  curl -H \"Authorization: Bearer %s\" %s/auth/whoami\n", tokens.AccessToken, serverURL)

	return nil
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func discoverOIDC(discoveryURL string) (*oidcDiscovery, error) {
	resp, err := httpClient.Get(discoveryURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func randomBase64(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func exchangeCode(tokenEndpoint, code, redirectURI, verifier string) (*oidcTokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {"mc-cli"},
		"code_verifier": {verifier},
	}

	resp, err := httpClient.PostForm(tokenEndpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &oidcTokens{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		IDToken:      result.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}, nil
}

// validateNonce extracts the nonce claim from an ID token (without full verification).
func validateNonce(idToken, expectedNonce string) error {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid ID token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode ID token payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("parse ID token claims: %w", err)
	}
	nonce, _ := claims["nonce"].(string)
	if nonce != expectedNonce {
		return fmt.Errorf("nonce mismatch")
	}
	return nil
}

func storeTokens(serverURL string, tokens *oidcTokens) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "mission-control")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Store per server
	host := strings.NewReplacer("://", "_", "/", "_", ":", "_").Replace(serverURL)
	path := filepath.Join(dir, fmt.Sprintf("tokens_%s.json", host))

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func openBrowser(url string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, url).Start()
}
