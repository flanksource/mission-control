package clientcmd

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/auth/oidc/static"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/clientcmd/mccontext"
	"github.com/spf13/cobra"
)

var AuthLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in via OIDC browser flow, or store an access token with --token",
	RunE:  runAuthLogin,
}

var (
	loginServer          string
	loginToken           string
	loginCredentialStore string
)

func init() {
	AuthLoginCmd.Flags().StringVar(&loginServer, "server", "", "Mission Control server URL (required)")
	AuthLoginCmd.Flags().StringVar(&loginToken, "token", "", "Store this access token instead of starting the OIDC browser flow")
	AuthLoginCmd.Flags().StringVar(&loginCredentialStore, "credential-store", "",
		"Where to keep credentials: keychain (OS keychain) or file. Detected once at login and recorded in the config")
	_ = AuthLoginCmd.MarkFlagRequired("server")
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	apiServer, err := mccontext.ResolveAPIBase(loginServer)
	if err != nil {
		return err
	}
	if loginToken != "" {
		contextName, store, err := saveTokenContext(apiServer, loginToken)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Token stored for %s (%s)\n", apiServer, store)
		fmt.Fprintf(cmd.OutOrStdout(), "Context %q saved for %s\n", contextName, apiServer)
		fmt.Fprintf(cmd.OutOrStdout(), "Run `whoami` to verify connectivity.\n")
		return nil
	}

	tokens, endpoints, err := performOIDCLoginForAPIBase(cmd, apiServer, cmd.OutOrStdout())
	if err != nil {
		return err
	}
	contextName, store, err := saveLoginContext(apiServer, tokens, endpoints)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nLogin successful!\n\n")
	fmt.Fprintf(cmd.OutOrStdout(), "Credentials saved to: %s (%s)\n", mccontext.ConfigDir(), store)
	fmt.Fprintf(cmd.OutOrStdout(), "Context %q saved for %s\n", contextName, apiServer)
	fmt.Fprintf(cmd.OutOrStdout(), "Access token expires: %s\n", tokens.ExpiresAt.Format(time.RFC3339))

	return nil
}

func performOIDCLoginForAPIBase(cmd *cobra.Command, apiServer string, status io.Writer) (*oidcclient.Tokens, *oidcclient.Discovery, error) {
	var lastErr error
	for _, loginServer := range mccontext.OIDCLoginServerCandidates(apiServer) {
		tokens, endpoints, err := oidcLogin(cmd, loginServer, status)
		if err == nil {
			return tokens, endpoints, nil
		}
		lastErr = err
	}
	return nil, nil, fmt.Errorf("OIDC login failed for %s: %w", apiServer, lastErr)
}

func saveLoginContext(serverURL string, tokens *oidcclient.Tokens, endpoints *oidcclient.Discovery) (string, string, error) {
	return saveAuthContext(serverURL, func(ctx *mccontext.MCContext) {
		ctx.SetOIDCTokens(tokens)
		ctx.Endpoints = endpoints
	})
}

func saveTokenContext(serverURL, token string) (string, string, error) {
	return saveAuthContext(serverURL, func(ctx *mccontext.MCContext) {
		ctx.Token = token
		ctx.OIDC = nil
		ctx.NeedsReauth = ""
	})
}

func saveAuthContext(serverURL string, apply func(*mccontext.MCContext)) (string, string, error) {
	cfg, err := mccontext.LoadConfig()
	if err != nil {
		return "", "", err
	}
	if err := mccontext.ChooseCredentialStore(cfg, loginCredentialStore); err != nil {
		return "", "", err
	}

	name := mccontext.ServerToContextName(serverURL)
	ctx := mccontext.MCContext{Name: name, Server: serverURL}
	if existing := cfg.GetContext(name); existing != nil {
		ctx = *existing
		ctx.Server = serverURL
	}
	apply(&ctx)
	cfg.SetContext(ctx)
	cfg.CurrentContext = name
	return name, cfg.CredentialStore, mccontext.SaveConfig(cfg)
}

var oidcLogin = PerformOIDCLogin

// PerformOIDCLogin runs the authorization-code flow with PKCE and returns the
// tokens along with the provider metadata they came from, so the caller can pin
// the token endpoint instead of re-discovering it on every refresh.
func PerformOIDCLogin(cmd *cobra.Command, serverURL string, status io.Writer) (*oidcclient.Tokens, *oidcclient.Discovery, error) {
	if status == nil {
		status = io.Discard
	}
	endpoints, err := oidcclient.Discover(serverURL + "/.well-known/openid-configuration")
	if err != nil {
		return nil, nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}

	verifier, challenge, err := oidcclient.GeneratePKCE()
	if err != nil {
		return nil, nil, fmt.Errorf("PKCE generation failed: %w", err)
	}
	state := oidcclient.RandomBase64(16)
	nonce := oidcclient.RandomBase64(16)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start local server: %w", err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	// Render the success page with absolute URLs to the MC server's static assets
	successHTML := renderCallbackSuccessHTML(serverURL)

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

	fmt.Fprintf(status, "Opening browser for login...\n%s\n\n", authURL)
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(status, "Failed to open browser: %v\nOpen the URL manually.\n\n", err)
	}

	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return nil, nil, err
	case <-time.After(5 * time.Minute):
		return nil, nil, fmt.Errorf("login timed out")
	}

	tokens, err := oidcclient.ExchangeCode(endpoints.TokenEndpoint, code, redirectURI, verifier)
	if err != nil {
		return nil, nil, fmt.Errorf("token exchange failed: %w", err)
	}

	if err := oidcclient.ValidateNonce(tokens.IDToken, nonce); err != nil {
		return nil, nil, fmt.Errorf("nonce validation failed: %w", err)
	}
	return tokens, endpoints, nil
}

func renderCallbackSuccessHTML(serverURL string) string {
	assetBase := html.EscapeString(serverURL + "/oidc/static/")
	return strings.ReplaceAll(static.CallbackSuccessHTML, "/oidc/static/", assetBase)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
