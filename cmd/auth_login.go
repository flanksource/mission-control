package cmd

import (
	"context"
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

	"github.com/flanksource/incident-commander/auth/oidc/static"
	"github.com/flanksource/incident-commander/auth/oidcclient"
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

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	serverURL := strings.TrimRight(loginServer, "/")

	endpoints, err := oidcclient.Discover(serverURL + "/.well-known/openid-configuration")
	if err != nil {
		return fmt.Errorf("OIDC discovery failed: %w", err)
	}

	verifier, challenge, err := oidcclient.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("PKCE generation failed: %w", err)
	}
	state := oidcclient.RandomBase64(16)
	nonce := oidcclient.RandomBase64(16)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local server: %w", err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	// Render the success page with absolute URLs to the MC server's static assets
	successHTML := strings.ReplaceAll(static.CallbackSuccessHTML, "/oidc/static/", serverURL+"/oidc/static/")

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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, successHTML)
		codeCh <- code
	})

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer func() { _ = server.Shutdown(context.Background()) }()

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

	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return err
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("login timed out")
	}

	tokens, err := oidcclient.ExchangeCode(endpoints.TokenEndpoint, code, redirectURI, verifier)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	if err := oidcclient.ValidateNonce(tokens.IDToken, nonce); err != nil {
		return fmt.Errorf("nonce validation failed: %w", err)
	}

	tokenPath, err := storeTokens(serverURL, tokens)
	if err != nil {
		return fmt.Errorf("failed to save tokens: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nLogin successful!\n\n")
	fmt.Fprintf(cmd.OutOrStdout(), "Tokens saved to: %s\n\n", tokenPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Access token (expires %s):\n%s\n\n", tokens.ExpiresAt.Format("15:04:05"), tokens.AccessToken)
	fmt.Fprintf(cmd.OutOrStdout(), "Refresh token:\n%s\n\n", tokens.RefreshToken)

	return nil
}

func storeTokens(serverURL string, tokens *oidcclient.Tokens) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "mission-control")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}

	host := strings.NewReplacer("://", "_", "/", "_", ":", "_").Replace(serverURL)
	path := filepath.Join(dir, fmt.Sprintf("tokens_%s.json", host))

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0600)
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
