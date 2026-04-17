package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	gocontext "context"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/connection"
)

type browserLoginFlags struct {
	Name               string
	Namespace          string
	URL                string
	Domains            []string
	WaitForURL         string
	Timeout            time.Duration
	Cookies            bool
	Session            bool
	Bearer             bool
	RequireBearerAud   string
	RequireBearerScope string
}

type browserSessionData struct {
	Cookies        []*network.Cookie
	SessionStorage map[string]string
	BearerTokens   map[string]string // audience -> token
}

var connectionLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Launch a browser to capture authentication state and create/update an HTTP connection",
	Long: `Opens a browser window for interactive login, captures cookies, session storage,
and/or bearer tokens, then saves them on an HTTP connection.

Examples:
  mission-control connection login --name my-site --url https://example.com --cookies
  mission-control connection login azure --name my-azure`,
	PersistentPreRun: PreRun,
	SilenceUsage:     true,
}

var browserFlags browserLoginFlags
var azureLoginURL string
var azurePageURL string
var azureRequiredScope string

var (
	browserTestName          string
	browserTestNamespace     string
	browserTestScreenshotURL string
	browserTestOutput        string
)

var connectionTestBrowserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Test a browser connection by logging in and taking a screenshot",
	Long: `Loads a connection from the database, injects its cookies and bearer tokens
into a browser session, then navigates to a URL and takes a screenshot.

Examples:
  # Test Azure connection by screenshotting the directory settings page
  app connection test browser --name my-azure --screenshot-url "https://portal.azure.com/#settings/directory"

  # Default screenshot URL for connections with portal.azure.com
  app connection test browser --name my-azure`,
	PersistentPreRun: PreRun,
	SilenceUsage:     true,
	RunE:             runBrowserTest,
}

var connectionLoginAzureCmd = &cobra.Command{
	Use:   "azure",
	Short: "Login to Azure Portal and capture authentication state",
	Long: `Launches a browser to Azure Portal, captures cookies, MSAL session tokens,
and bearer tokens for Azure APIs.

By default captures all three (--cookies --session --bearer).
Use flags to select specific capture types.`,
	RunE: runBrowserLogin,
}

func init() {
	connectionLoginCmd.PersistentFlags().StringVar(&browserFlags.Name, "name", "", "Connection name (required)")
	connectionLoginCmd.PersistentFlags().StringVar(&browserFlags.Namespace, "namespace", "default", "Connection namespace")
	connectionLoginCmd.PersistentFlags().StringVar(&browserFlags.URL, "url", "", "URL to navigate to")
	connectionLoginCmd.PersistentFlags().StringSliceVar(&browserFlags.Domains, "domains", nil, "Domains to capture cookies from")
	connectionLoginCmd.PersistentFlags().StringVar(&browserFlags.WaitForURL, "wait-for-url", "", "Auto-detect login completion when URL matches this pattern")
	connectionLoginCmd.PersistentFlags().DurationVar(&browserFlags.Timeout, "timeout", 5*time.Minute, "Timeout for browser login")
	connectionLoginCmd.PersistentFlags().BoolVar(&browserFlags.Cookies, "cookies", false, "Capture cookies")
	connectionLoginCmd.PersistentFlags().BoolVar(&browserFlags.Session, "session", false, "Capture sessionStorage (MSAL token cache)")
	connectionLoginCmd.PersistentFlags().BoolVar(&browserFlags.Bearer, "bearer", false, "Extract bearer tokens from MSAL session cache")
	_ = connectionLoginCmd.MarkPersistentFlagRequired("name")

	connectionLoginCmd.RunE = runBrowserLogin

	connectionLoginAzureCmd.PersistentFlags().StringVar(&azureLoginURL, "login-url", "https://portal.azure.com", "URL to open for browser login")
	connectionLoginAzureCmd.PersistentFlags().StringVar(&azurePageURL, "page", "https://portal.azure.com/#view/Microsoft_AAD_IAM/ActiveDirectoryMenuBlade/~/Overview", "Portal page to navigate to after login")
	connectionLoginAzureCmd.PersistentFlags().StringVar(&azureRequiredScope, "required-scope", "AuditLog.Read.All", "Required scope substring in the captured msgraph token")

	connectionLoginAzureCmd.PreRun = func(cmd *cobra.Command, args []string) {
		browserFlags.URL = azurePageURL
		if len(browserFlags.Domains) == 0 {
			browserFlags.Domains = []string{".azure.com", ".microsoft.com", ".microsoftonline.com", ".windows.net", ".live.com"}
		}
		if !browserFlags.Cookies && !browserFlags.Session && !browserFlags.Bearer {
			browserFlags.Cookies = true
			browserFlags.Session = true
			browserFlags.Bearer = true
		}
		browserFlags.RequireBearerAud = "graph.microsoft.com"
		browserFlags.RequireBearerScope = azureRequiredScope
	}

	connectionLoginCmd.AddCommand(connectionLoginAzureCmd)
	Connection.AddCommand(connectionLoginCmd)

	connectionTestBrowserCmd.Flags().StringVar(&browserTestName, "name", "", "Connection name to load from database (required)")
	connectionTestBrowserCmd.Flags().StringVar(&browserTestNamespace, "namespace", "default", "Connection namespace")
	connectionTestBrowserCmd.Flags().StringVar(&browserTestScreenshotURL, "screenshot-url", "", "URL to navigate to and screenshot after login")
	connectionTestBrowserCmd.Flags().StringVar(&browserTestOutput, "output", "screenshot.png", "Screenshot output file path")
	_ = connectionTestBrowserCmd.MarkFlagRequired("name")
	ConnectionTest.AddCommand(connectionTestBrowserCmd)
}

func runBrowserLogin(cmd *cobra.Command, args []string) error {
	if browserFlags.URL == "" {
		return fmt.Errorf("--url is required")
	}

	targetURL, err := url.Parse(browserFlags.URL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if len(browserFlags.Domains) == 0 {
		browserFlags.Domains = []string{"." + targetURL.Hostname()}
	}

	if !browserFlags.Cookies && !browserFlags.Session && !browserFlags.Bearer {
		browserFlags.Cookies = true
	}

	data, err := launchBrowserAndCapture(cmd.Context(), browserFlags)
	if err != nil {
		return err
	}

	if browserFlags.RequireBearerAud != "" || browserFlags.RequireBearerScope != "" {
		if !hasRequiredToken(data.BearerTokens, browserFlags) {
			return fmt.Errorf("no valid token found matching audience=%q scope=%q", browserFlags.RequireBearerAud, browserFlags.RequireBearerScope)
		}
	}

	return saveConnection(cmd, browserFlags, data)
}

func launchBrowserAndCapture(ctx gocontext.Context, flags browserLoginFlags) (*browserSessionData, error) {
	userDataDir := ProfileDir(flags.Namespace, flags.Name)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.NoSandbox,
		chromedp.UserDataDir(userDataDir),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	if err := chromedp.Run(browserCtx, chromedp.Navigate(flags.URL)); err != nil {
		return nil, fmt.Errorf("failed to navigate to %s: %w", flags.URL, err)
	}

	fmt.Fprintf(os.Stderr, "Browser opened at %s\n", flags.URL)
	if flags.Bearer {
		fmt.Fprintln(os.Stderr, "Please log in. Will auto-detect when a valid token is available (or press Enter to skip).")
	} else {
		fmt.Fprintln(os.Stderr, "Please log in. Press Enter when done.")
	}

	autoSelectAccountPicker(browserCtx)
	waitForLoginComplete(browserCtx, flags)

	data := &browserSessionData{}

	verbose := clicky.Flags.LevelCount

	if flags.Cookies {
		cookies, err := extractCookies(browserCtx, flags.Domains)
		if err != nil {
			return nil, err
		}
		data.Cookies = cookies
	}

	if flags.Session || flags.Bearer {
		session, err := extractSessionStorage(browserCtx)
		if err != nil {
			return nil, err
		}
		data.SessionStorage = session

		if flags.Bearer {
			data.BearerTokens = extractBearerTokens(session)
		}
	}

	// Build and display session state summary
	var cookies connection.Cookies
	for _, c := range data.Cookies {
		cookies = append(cookies, connection.Cookie{
			Name: c.Name, Value: c.Value, Domain: c.Domain,
			Path: c.Path, Expires: float64(c.Expires),
			HTTPOnly: c.HTTPOnly, Secure: c.Secure,
			SameSite: string(c.SameSite),
		})
	}
	state := connection.NewPlaywrightSessionState(cookies, data.SessionStorage, nil, flags.URL)
	if verbose >= 2 {
		fmt.Fprintln(os.Stderr, state.PrettyFull().ANSI())
	} else if verbose >= 1 {
		fmt.Fprintln(os.Stderr, state.Pretty().ANSI())
	} else {
		selectedAud, _ := selectBearerToken(data.BearerTokens, flags.RequireBearerAud, flags.RequireBearerScope)
		for _, aud := range sortedAudiences(data.BearerTokens) {
			if jwt := connection.DecodeJWT(data.BearerTokens[aud]); jwt != nil {
				t := jwt.Pretty()
				if aud == selectedAud {
					t = api.Text{}.Add(icons.Check.WithStyle("text-green-500")).Append(" bearer ").Add(t)
				}
				fmt.Fprintln(os.Stderr, t.ANSI())
			}
		}
	}

	return data, nil
}

func autoSelectAccountPicker(browserCtx gocontext.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var nodes int
				err := chromedp.Run(browserCtx, chromedp.Evaluate(
					`document.querySelectorAll('#tilesHolder .tile-container .table[role="button"]').length`, &nodes))
				if err != nil || nodes == 0 {
					continue
				}
				fmt.Fprintf(os.Stderr, "Account picker detected, selecting first account\n")
				_ = chromedp.Run(browserCtx, chromedp.Click(
					`#tilesHolder .tile-container:first-child .table[role="button"]`, chromedp.ByQuery))
				return
			case <-browserCtx.Done():
				return
			}
		}
	}()
}

func waitForLoginComplete(browserCtx gocontext.Context, flags browserLoginFlags) {
	doneCh := make(chan struct{}, 3)

	go func() {
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')
		select {
		case doneCh <- struct{}{}:
		default:
		}
	}()

	if flags.WaitForURL != "" {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					var currentURL string
					if err := chromedp.Run(browserCtx, chromedp.Location(&currentURL)); err == nil {
						if strings.Contains(currentURL, flags.WaitForURL) {
							time.Sleep(2 * time.Second)
							doneCh <- struct{}{}
							return
						}
					}
				case <-browserCtx.Done():
					return
				}
			}
		}()
	}

	if flags.Bearer {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			lastReported := ""
			for {
				select {
				case <-ticker.C:
					session, err := extractSessionStorage(browserCtx)
					if err != nil {
						continue
					}
					validAuds := make([]string, 0)
					var matched string
					var matchedJWT *connection.JWT
					for aud, token := range extractBearerTokens(session) {
						jwt := connection.DecodeJWT(token)
						if jwt == nil || time.Until(jwt.ExpiresAt) <= 0 {
							continue
						}
						validAuds = append(validAuds, aud)
						if matched != "" {
							continue
						}
						audMatches := flags.RequireBearerAud == "" || strings.Contains(aud, flags.RequireBearerAud)
						scopeMatches := flags.RequireBearerScope == "" || strings.Contains(jwt.Scopes, flags.RequireBearerScope)
						if audMatches && scopeMatches {
							matched = aud
							matchedJWT = jwt
						}
					}
					if matched != "" {
						fmt.Fprintf(os.Stderr, "Found valid token for %s (scopes=%d, expires in %s)\n", matched, matchedJWT.ScopeCount(), time.Until(matchedJWT.ExpiresAt).Round(time.Second))
						doneCh <- struct{}{}
						return
					}
					if (flags.RequireBearerAud != "" || flags.RequireBearerScope != "") && len(validAuds) > 0 {
						sort.Strings(validAuds)
						summary := strings.Join(validAuds, ", ")
						if summary != lastReported {
							waiting := flags.RequireBearerAud
							if flags.RequireBearerScope != "" {
								waiting += " with scope " + flags.RequireBearerScope
							}
							fmt.Fprintf(os.Stderr, "Waiting for %s token (have: %s)\n", waiting, summary)
							lastReported = summary
						}
					}
				case <-browserCtx.Done():
					return
				}
			}
		}()
	}

	select {
	case <-doneCh:
	case <-time.After(flags.Timeout):
		fmt.Fprintln(os.Stderr, "Login timed out")
	}
}

func extractCookies(browserCtx gocontext.Context, domains []string) ([]*network.Cookie, error) {
	var allCookies []*network.Cookie
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx gocontext.Context) error {
		var err error
		allCookies, err = storage.GetCookies().Do(ctx)
		return err
	})); err != nil {
		return nil, fmt.Errorf("failed to extract cookies: %w", err)
	}

	var filtered []*network.Cookie
	for _, c := range allCookies {
		for _, d := range domains {
			if strings.HasSuffix(c.Domain, d) || c.Domain == strings.TrimPrefix(d, ".") {
				filtered = append(filtered, c)
				break
			}
		}
	}
	return filtered, nil
}

func extractSessionStorage(browserCtx gocontext.Context) (map[string]string, error) {
	var resultJSON string
	err := chromedp.Run(browserCtx, chromedp.Evaluate(`JSON.stringify(
		Object.fromEntries(
			Object.keys(sessionStorage).map(k => [k, sessionStorage.getItem(k)])
		)
	)`, &resultJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to extract sessionStorage: %w", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("failed to parse sessionStorage: %w", err)
	}
	return result, nil
}

func hasRequiredToken(tokens map[string]string, flags browserLoginFlags) bool {
	for aud, token := range tokens {
		if flags.RequireBearerAud != "" && !strings.Contains(aud, flags.RequireBearerAud) {
			continue
		}
		jwt := connection.DecodeJWT(token)
		if jwt == nil || time.Until(jwt.ExpiresAt) <= 0 {
			continue
		}
		if flags.RequireBearerScope != "" && !strings.Contains(jwt.Scopes, flags.RequireBearerScope) {
			continue
		}
		return true
	}
	return false
}

func extractBearerTokens(session map[string]string) map[string]string {
	tokens := make(map[string]string)
	scopeCounts := make(map[string]int)
	for key, value := range session {
		if !strings.Contains(key, "accesstoken") {
			continue
		}
		var tokenEntry map[string]any
		if err := json.Unmarshal([]byte(value), &tokenEntry); err != nil {
			continue
		}
		secret, _ := tokenEntry["secret"].(string)
		if secret == "" {
			continue
		}
		jwt := connection.DecodeJWT(secret)
		if jwt == nil || jwt.Audience == "" {
			continue
		}
		if !jwt.ExpiresAt.IsZero() && time.Until(jwt.ExpiresAt) <= 0 {
			continue
		}
		if jwt.ScopeCount() > scopeCounts[jwt.Audience] {
			tokens[jwt.Audience] = secret
			scopeCounts[jwt.Audience] = jwt.ScopeCount()
		}
	}
	return tokens
}

func selectBearerToken(tokens map[string]string, requiredAud, requiredScope string) (string, error) {
	var bestAud string
	var bestScopes int
	for aud, token := range tokens {
		if !strings.Contains(aud, requiredAud) {
			continue
		}
		jwt := connection.DecodeJWT(token)
		if jwt == nil {
			continue
		}
		if requiredScope != "" && !strings.Contains(jwt.Scopes, requiredScope) {
			continue
		}
		if jwt.ScopeCount() > bestScopes {
			bestAud = aud
			bestScopes = jwt.ScopeCount()
		}
	}
	if bestAud != "" {
		return bestAud, nil
	}
	return "", fmt.Errorf("no token found for required audience %q (have: %s)", requiredAud, strings.Join(sortedAudiences(tokens), ", "))
}

func sortedAudiences(tokens map[string]string) []string {
	auds := make([]string, 0, len(tokens))
	for aud := range tokens {
		auds = append(auds, aud)
	}
	sort.Strings(auds)
	return auds
}

func saveConnection(cmd *cobra.Command, flags browserLoginFlags, data *browserSessionData) error {
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

	props := make(map[string]string)

	// Convert chromedp cookies to connection.Cookies
	var cookies connection.Cookies
	for _, c := range data.Cookies {
		cookies = append(cookies, connection.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			SameSite: string(c.SameSite),
		})
	}

	// Build Playwright-compatible storage state
	sessionState := connection.NewPlaywrightSessionState(cookies, data.SessionStorage, nil, flags.URL)
	storageJSON, err := json.Marshal(sessionState)
	if err != nil {
		return fmt.Errorf("failed to marshal storage state: %w", err)
	}
	props["storageState"] = string(storageJSON)

	// Also store cookies as headers for HTTP connection compatibility
	if len(data.Cookies) > 0 {
		parts := make([]string, len(data.Cookies))
		for i, c := range data.Cookies {
			parts[i] = c.Name + "=" + c.Value
		}
		headersJSON, err := json.Marshal([]types.EnvVar{{Name: "Cookie", ValueStatic: strings.Join(parts, "; ")}})
		if err != nil {
			return fmt.Errorf("failed to marshal headers: %w", err)
		}
		props["headers"] = string(headersJSON)
	}

	// Persist sessionStorage so MSAL-style SPAs (e.g. Azure portal) can be
	// rehydrated by Playwright via addInitScript. Tied to the captured origin
	// so the receiver knows which page to inject into.
	if len(data.SessionStorage) > 0 {
		if u, err := url.Parse(flags.URL); err == nil && u.Host != "" {
			payload := map[string]any{
				"origin": u.Scheme + "://" + u.Host,
				"items":  data.SessionStorage,
			}
			if sessionJSON, err := json.Marshal(payload); err == nil {
				props["sessionStorage"] = string(sessionJSON)
			}
		}
	}

	// Store bearer tokens
	if len(data.BearerTokens) > 0 {
		for aud, token := range data.BearerTokens {
			props["bearer_"+aud] = token
		}
		selectedAud, err := selectBearerToken(data.BearerTokens, flags.RequireBearerAud, flags.RequireBearerScope)
		if err != nil {
			return err
		}
		props["bearer"] = data.BearerTokens[selectedAud]
	}

	connURL := flags.URL
	if props["bearer"] != "" && strings.Contains(flags.RequireBearerAud, "graph.microsoft.com") {
		connURL = "https://graph.microsoft.com/v1.0/me"
	}

	conn := models.Connection{
		Name:       flags.Name,
		Namespace:  flags.Namespace,
		Type:       models.ConnectionTypeHTTP,
		URL:        connURL,
		Source:     models.SourceUI,
		Properties: props,
	}

	var existing models.Connection
	err = ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", flags.Name, flags.Namespace).First(&existing).Error
	if err == nil {
		conn.ID = existing.ID
		conn.CreatedAt = existing.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check existing connection: %w", err)
	} else {
		conn.ID = uuid.New()
	}

	if err := ctx.DB().Save(&conn).Error; err != nil {
		return fmt.Errorf("failed to save connection: %w", err)
	}

	action := "created"
	if existing.ID != uuid.Nil {
		action = "updated"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Connection '%s' %s in namespace '%s'\n", flags.Name, action, flags.Namespace)

	if len(data.Cookies) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  Cookies: %d\n", len(data.Cookies))
	}
	if len(data.SessionStorage) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  Session storage: %d keys\n", len(data.SessionStorage))
	}
	if len(data.BearerTokens) > 0 {
		selectedAud, _ := selectBearerToken(data.BearerTokens, flags.RequireBearerAud, flags.RequireBearerScope)
		for _, aud := range sortedAudiences(data.BearerTokens) {
			jwt := connection.DecodeJWT(data.BearerTokens[aud])
			if jwt == nil {
				continue
			}
			t := api.Text{}
			if aud == selectedAud {
				t = t.Add(icons.Check.WithStyle("text-green-500")).Append(" bearer", "font-bold")
			} else {
				t = t.Append("  bearer_"+aud, "text-muted")
			}
			t = t.Appendf(" aud=%s", jwt.Audience).
				Appendf(" scopes=%d", jwt.ScopeCount()).
				Appendf(" expires=%s", time.Until(jwt.ExpiresAt).Round(time.Second))
			fmt.Fprintln(cmd.OutOrStdout(), t.ANSI())
		}
	}

	return nil
}

func runBrowserTest(cmd *cobra.Command, args []string) error {
	ctx, stop, err := duty.Start("mission-control", duty.ClientOnly)
	if err != nil {
		return err
	}
	shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

	verbose := clicky.Flags.LevelCount

	var conn models.Connection
	if err := ctx.DB().Where("name = ? AND namespace = ? AND deleted_at IS NULL", browserTestName, browserTestNamespace).First(&conn).Error; err != nil {
		return fmt.Errorf("connection %s/%s not found: %w", browserTestNamespace, browserTestName, err)
	}

	if verbose >= 1 {
		printConnectionState(conn, verbose)
	}

	screenshotURL := browserTestScreenshotURL
	if screenshotURL == "" {
		if strings.Contains(conn.URL, "azure") || strings.Contains(conn.URL, "graph.microsoft.com") {
			screenshotURL = "https://portal.azure.com/#settings/directory"
		} else {
			screenshotURL = conn.URL
		}
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.NoSandbox,
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(cmd.Context(), opts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	if storageJSON := conn.Properties["storageState"]; storageJSON != "" {
		var state connection.PlaywrightSessionState
		if err := json.Unmarshal([]byte(storageJSON), &state); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to parse storageState: %v\n", err)
		} else if err := injectCookies(browserCtx, state.Cookies); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to inject cookies: %v\n", err)
		}
	}

	if sessionJSON := conn.Properties["sessionStorage"]; sessionJSON != "" {
		var payload struct {
			Origin string            `json:"origin"`
			Items  map[string]string `json:"items"`
		}
		if err := json.Unmarshal([]byte(sessionJSON), &payload); err == nil && payload.Origin != "" && len(payload.Items) > 0 {
			if err := injectSessionStorage(browserCtx, payload.Origin, payload.Items); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to inject sessionStorage: %v\n", err)
			}
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Navigating to %s\n", screenshotURL)

	if err := chromedp.Run(browserCtx, chromedp.Navigate(screenshotURL)); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}

	// Wait for the page to settle
	time.Sleep(5 * time.Second)

	if err := detectLoginPage(browserCtx); err != nil {
		// Still take the screenshot for debugging before failing
		var buf []byte
		if screenshotErr := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx gocontext.Context) error {
			var captureErr error
			buf, captureErr = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).WithCaptureBeyondViewport(true).Do(ctx)
			return captureErr
		})); screenshotErr == nil && len(buf) > 0 {
			_ = os.WriteFile(browserTestOutput, buf, 0644)
			fmt.Fprintf(cmd.ErrOrStderr(), "Screenshot saved to %s for debugging\n", browserTestOutput)
		}
		return err
	}

	var buf []byte
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx gocontext.Context) error {
		var err error
		buf, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).WithCaptureBeyondViewport(true).Do(ctx)
		return err
	})); err != nil {
		return fmt.Errorf("failed to take screenshot: %w", err)
	}

	if err := os.WriteFile(browserTestOutput, buf, 0644); err != nil {
		return fmt.Errorf("failed to write screenshot: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Screenshot saved to %s (%d bytes)\n", browserTestOutput, len(buf))
	return nil
}

var loginPagePatterns = []string{
	"sign in",
	"sign-in",
	"signin",
	"login",
	"log in",
	"log-in",
	"authenticate",
	"enter your password",
	"pick an account",
}

func detectLoginPage(browserCtx gocontext.Context) error {
	var title, currentURL string
	if err := chromedp.Run(browserCtx,
		chromedp.Title(&title),
		chromedp.Location(&currentURL),
	); err != nil {
		return fmt.Errorf("failed to read page state: %w", err)
	}

	lower := strings.ToLower(title)
	for _, pattern := range loginPagePatterns {
		if strings.Contains(lower, pattern) {
			return fmt.Errorf("session expired: landed on login page (title=%q url=%s)", title, currentURL)
		}
	}

	loginDomains := []string{"login.microsoftonline.com", "login.microsoft.com", "login.live.com", "accounts.google.com"}
	for _, domain := range loginDomains {
		if strings.Contains(currentURL, domain) {
			return fmt.Errorf("session expired: redirected to login page (title=%q url=%s)", title, currentURL)
		}
	}

	return nil
}

func injectCookies(browserCtx gocontext.Context, cookies connection.Cookies) error {
	if len(cookies) == 0 {
		return nil
	}
	params := make([]*network.CookieParam, 0, len(cookies))
	for _, c := range cookies {
		cp := &network.CookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			t := cdp.TimeSinceEpoch(time.Unix(int64(c.Expires), 0))
			cp.Expires = &t
		}
		switch c.SameSite {
		case "Strict":
			cp.SameSite = network.CookieSameSiteStrict
		case "Lax":
			cp.SameSite = network.CookieSameSiteLax
		case "None":
			cp.SameSite = network.CookieSameSiteNone
		}
		params = append(params, cp)
	}
	return chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx gocontext.Context) error {
		return network.SetCookies(params).Do(ctx)
	}))
}

func injectSessionStorage(browserCtx gocontext.Context, origin string, items map[string]string) error {
	if err := chromedp.Run(browserCtx, chromedp.Navigate(origin)); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)

	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return err
	}
	js := fmt.Sprintf(`
		const entries = %s;
		for (const [k, v] of Object.entries(entries)) {
			sessionStorage.setItem(k, v);
		}
	`, string(itemsJSON))
	return chromedp.Run(browserCtx, chromedp.Evaluate(js, nil))
}

func printConnectionState(conn models.Connection, verbose int) {
	fmt.Fprintf(os.Stderr, "Connection: %s/%s (type=%s)\n", conn.Namespace, conn.Name, conn.Type)

	if bearer := conn.Properties["bearer"]; bearer != "" {
		if jwt := connection.DecodeJWT(bearer); jwt != nil {
			if verbose >= 2 {
				fmt.Fprintln(os.Stderr, jwt.PrettyFull().ANSI())
			} else {
				fmt.Fprintln(os.Stderr, jwt.Pretty().ANSI())
			}
		}
	}

	if storageJSON := conn.Properties["storageState"]; storageJSON != "" {
		var state connection.PlaywrightSessionState
		if err := json.Unmarshal([]byte(storageJSON), &state); err == nil {
			if verbose >= 2 {
				fmt.Fprintln(os.Stderr, state.PrettyFull().ANSI())
			} else {
				fmt.Fprintln(os.Stderr, state.Pretty().ANSI())
			}
		}
	}
}
