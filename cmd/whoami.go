package cmd

import (
	gocontext "context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	clickyapi "github.com/flanksource/clicky/api"
	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/argon2"
)

var (
	whoamiJSON    bool
	whoamiRefresh bool
)

var WhoamiCmd = &cobra.Command{
	Use:          "whoami",
	Short:        "Show the current Mission Control context and connectivity",
	SilenceUsage: true,
	RunE:         runWhoami,
}

type whoamiReport struct {
	Context  whoamiContext  `json:"context"`
	Database whoamiDatabase `json:"database"`
	Auth     whoamiAuth     `json:"auth"`
}

type whoamiContext struct {
	Name         string   `json:"name,omitempty"`
	ConfigPath   string   `json:"config_path"`
	SelectedBy   string   `json:"selected_by,omitempty"`
	Server       string   `json:"server,omitempty"`
	DB           string   `json:"db,omitempty"`
	PropertyKeys []string `json:"property_keys,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type whoamiDatabase struct {
	Configured bool   `json:"configured"`
	Status     string `json:"status"`
	URL        string `json:"url,omitempty"`
	Database   string `json:"database,omitempty"`
	User       string `json:"user,omitempty"`
	Latency    string `json:"latency,omitempty"`
	Error      string `json:"error,omitempty"`
}

type whoamiAuth struct {
	Configured    bool               `json:"configured"`
	Status        string             `json:"status"`
	Server        string             `json:"server,omitempty"`
	Endpoint      string             `json:"endpoint,omitempty"`
	TokenSource   string             `json:"token_source,omitempty"`
	TokenExpires  string             `json:"token_expires,omitempty"`
	TokenTTL      string             `json:"token_ttl,omitempty"`
	RefreshStatus string             `json:"refresh_status,omitempty"`
	User          map[string]any     `json:"user,omitempty"`
	Roles         []string           `json:"roles,omitempty"`
	AccessToken   *accessTokenStatus `json:"access_token,omitempty"`
	Error         string             `json:"error,omitempty"`
}

type accessTokenStatus struct {
	ID          string `json:"id,omitempty"`
	PersonID    string `json:"person_id,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	AutoRenew   bool   `json:"auto_renew"`
	Renewed     bool   `json:"renewed"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	expiresTime *time.Time
}

type storedOIDCToken struct {
	Tokens *oidcclient.Tokens
	Server string
	Path   string
}

func runWhoami(cmd *cobra.Command, _ []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	mcCtx := cfg.CurrentMCContext()
	report := whoamiReport{
		Context: whoamiContext{
			ConfigPath: configPath(),
		},
	}
	if contextFlag != "" {
		report.Context.SelectedBy = "--context"
	} else if cfg.CurrentContext != "" {
		report.Context.SelectedBy = "current_context"
	}

	if mcCtx == nil {
		report.Context.Error = "no current context"
	} else {
		report.Context.Name = mcCtx.Name
		report.Context.Server = mcCtx.Server
		report.Context.DB = redactURL(mcCtx.DB)
		report.Context.PropertyKeys = sortedMapKeys(mcCtx.Properties)
	}

	dbConn := resolvedDBConnection(mcCtx)
	report.Database = probeDatabase(dbConn)
	report.Auth = probeAuth(cmd.Context(), cfg, mcCtx, dbConn, whoamiRefresh)

	if whoamiJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return whoamiStatusError(report)
	}

	printWhoami(cmd.OutOrStdout(), report)
	return whoamiStatusError(report)
}

func whoamiStatusError(report whoamiReport) error {
	if report.Database.Status == "ok" && report.Auth.Status == "ok" {
		return nil
	}
	return fmt.Errorf("whoami status failed: database=%s auth=%s", report.Database.Status, report.Auth.Status)
}

func resolvedDBConnection(mcCtx *MCContext) string {
	if mcCtx != nil && mcCtx.DB != "" {
		return mcCtx.DB
	}
	return dutyAPI.DefaultConfig.ReadEnv().ConnectionString
}

func probeDatabase(conn string) whoamiDatabase {
	out := whoamiDatabase{Configured: conn != ""}
	if conn == "" {
		out.Status = "skipped"
		out.Error = "no database configured"
		return out
	}

	out.URL = redactURL(conn)
	start := time.Now()
	db, err := duty.NewDB(conn)
	if err != nil {
		out.Status = "error"
		out.Error = err.Error()
		return out
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)

	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		out.Status = "error"
		out.Error = err.Error()
		out.Latency = time.Since(start).Round(time.Millisecond).String()
		return out
	}

	var database, user string
	if err := db.QueryRowContext(ctx, "SELECT current_database(), current_user").Scan(&database, &user); err != nil {
		out.Status = "error"
		out.Error = err.Error()
		out.Latency = time.Since(start).Round(time.Millisecond).String()
		return out
	}

	out.Status = "ok"
	out.Database = database
	out.User = user
	out.Latency = time.Since(start).Round(time.Millisecond).String()
	return out
}

func probeAuth(parent gocontext.Context, cfg *MCConfig, mcCtx *MCContext, dbConn string, refresh bool) whoamiAuth {
	out := whoamiAuth{Status: "skipped"}
	if mcCtx == nil {
		out.Error = "no current context"
		return out
	}
	if mcCtx.Server == "" {
		out.Error = "no server configured"
		return out
	}

	out.Configured = true
	out.Server = mcCtx.Server
	token := mcCtx.Token
	if token != "" {
		out.TokenSource = "context"
	}

	stored, storedErr := loadStoredOIDCTokens(mcCtx.Server)
	if storedErr != nil && !os.IsNotExist(storedErr) {
		out.RefreshStatus = "stored OIDC token unavailable: " + storedErr.Error()
	}
	if stored != nil && stored.Tokens != nil {
		oldAccessToken := stored.Tokens.AccessToken
		out.TokenExpires = formatTime(stored.Tokens.ExpiresAt)
		out.TokenTTL = formatTTL(stored.Tokens.ExpiresAt)
		if refresh && oidcTokenExpiring(stored.Tokens) && stored.Tokens.RefreshToken != "" {
			refreshed, err := refreshOIDCTokens(mcCtx.Server, stored)
			if err != nil {
				out.RefreshStatus = "failed: " + err.Error()
			} else {
				stored = refreshed
				out.RefreshStatus = "refreshed"
				out.TokenExpires = formatTime(stored.Tokens.ExpiresAt)
				out.TokenTTL = formatTTL(stored.Tokens.ExpiresAt)
			}
		} else if oidcTokenExpiring(stored.Tokens) && stored.Tokens.RefreshToken == "" {
			out.RefreshStatus = "unavailable: no refresh token"
		} else if oidcTokenExpiring(stored.Tokens) {
			out.RefreshStatus = "needed"
		} else {
			out.RefreshStatus = "not needed"
		}
		if shouldUseStoredOIDCToken(token, oldAccessToken, stored.Tokens) {
			token = stored.Tokens.AccessToken
			out.TokenSource = "stored_oidc"
			updateContextToken(cfg, mcCtx.Name, token)
		}
	}

	if token == "" {
		out.Status = "skipped"
		out.Error = "no token configured"
		return out
	}
	if out.TokenSource == "" {
		out.TokenSource = "context"
	}

	before := inspectAccessToken(dbConn, token)
	if before != nil {
		out.AccessToken = before
	}

	user, roles, endpoint, statusCode, err := callWhoami(parent, mcCtx.Server, token)
	out.Endpoint = endpoint
	if err != nil {
		out.Status = "error"
		out.Error = err.Error()
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			out.Status = "invalid"
		}
		return out
	}

	out.Status = "ok"
	out.User = user
	out.Roles = roles

	after := inspectAccessToken(dbConn, token)
	if after != nil {
		if before != nil && before.expiresTime != nil && after.expiresTime != nil && after.expiresTime.After(*before.expiresTime) {
			after.Renewed = true
		}
		out.AccessToken = after
	}
	return out
}

func callWhoami(parent gocontext.Context, server, token string) (map[string]any, []string, string, int, error) {
	if parent == nil {
		parent = gocontext.Background()
	}
	ctx, cancel := gocontext.WithTimeout(parent, 15*time.Second)
	defer cancel()

	var lastErr error
	var lastStatus int
	candidates := whoamiEndpointCandidates(server)
	for _, endpoint := range candidates {
		base := strings.TrimSuffix(endpoint, "/auth/whoami")
		decoded, statusCode, err := newAPIClientForServer(base, token).Whoami(ctx)
		lastStatus = statusCode
		if err != nil {
			if (statusCode == http.StatusNotFound || errors.Is(err, sdk.ErrHTMLResponse)) && len(candidates) > 1 {
				lastErr = err
				continue
			}
			if statusCode == 0 {
				lastErr = err
				continue
			}
			return nil, nil, endpoint, statusCode, err
		}
		return decoded.Payload.User, decoded.Payload.Roles, endpoint, statusCode, nil
	}
	if lastErr != nil {
		return nil, nil, "", lastStatus, lastErr
	}
	return nil, nil, "", lastStatus, fmt.Errorf("no whoami endpoint candidates for %s", server)
}

func whoamiEndpointCandidates(server string) []string {
	server = strings.TrimRight(server, "/")
	candidates := []string{}
	add := func(base string) {
		if base == "" {
			return
		}
		candidates = append(candidates, strings.TrimRight(base, "/")+"/auth/whoami")
	}
	add(server)
	if strings.HasSuffix(server, "/api") {
		add(strings.TrimSuffix(server, "/api"))
	} else {
		add(server + "/api")
	}
	return uniqueStrings(candidates)
}

func loadStoredOIDCTokens(server string) (*storedOIDCToken, error) {
	var firstErr error
	for _, candidate := range oidcServerCandidates(server) {
		tokens, err := loadStoredTokens(candidate)
		if err == nil {
			path, _ := tokenPath(candidate)
			return &storedOIDCToken{Tokens: tokens, Server: candidate, Path: path}, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = os.ErrNotExist
	}
	return nil, firstErr
}

func oidcServerCandidates(server string) []string {
	server = strings.TrimRight(server, "/")
	candidates := []string{server}
	if strings.HasSuffix(server, "/api") {
		candidates = append(candidates, strings.TrimSuffix(server, "/api"))
	}
	return uniqueStrings(candidates)
}

func oidcTokenExpiring(tokens *oidcclient.Tokens) bool {
	if tokens == nil {
		return false
	}
	return tokens.AccessToken == "" || (!tokens.ExpiresAt.IsZero() && time.Until(tokens.ExpiresAt) < time.Minute)
}

func shouldUseStoredOIDCToken(contextToken, previousStoredToken string, tokens *oidcclient.Tokens) bool {
	if tokens == nil || tokens.AccessToken == "" || oidcTokenExpiring(tokens) {
		return false
	}
	if contextToken == "" || contextToken == previousStoredToken {
		return true
	}
	return !isMissionControlAccessTokenFormat(contextToken)
}

func refreshOIDCTokens(server string, stored *storedOIDCToken) (*storedOIDCToken, error) {
	if stored == nil || stored.Tokens == nil {
		return nil, fmt.Errorf("no stored OIDC tokens")
	}
	if stored.Tokens.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token")
	}

	var lastErr error
	for _, candidate := range oidcServerCandidates(firstNonEmpty(stored.Server, server)) {
		endpoints, err := oidcclient.Discover(strings.TrimRight(candidate, "/") + "/.well-known/openid-configuration")
		if err != nil {
			lastErr = err
			continue
		}
		refreshed, err := oidcclient.RefreshToken(endpoints.TokenEndpoint, stored.Tokens.RefreshToken)
		if err != nil {
			lastErr = err
			continue
		}
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = stored.Tokens.RefreshToken
		}
		if refreshed.IDToken == "" {
			refreshed.IDToken = stored.Tokens.IDToken
		}
		path, err := storeTokens(candidate, refreshed)
		if err != nil {
			return nil, err
		}
		return &storedOIDCToken{Tokens: refreshed, Server: candidate, Path: path}, nil
	}
	return nil, lastErr
}

func updateContextToken(cfg *MCConfig, name, token string) {
	if cfg == nil || name == "" || token == "" {
		return
	}
	ctx := cfg.GetContext(name)
	if ctx == nil || ctx.Token == token {
		return
	}
	ctx.Token = token
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "failed to update context token: %v\n", err)
	}
}

func inspectAccessToken(conn, token string) *accessTokenStatus {
	if conn == "" || token == "" {
		return nil
	}
	hash, err := hashMissionControlAccessToken(token)
	if err != nil {
		return nil
	}
	db, err := duty.NewDB(conn)
	if err != nil {
		return &accessTokenStatus{Status: "unknown", Error: err.Error()}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)

	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 10*time.Second)
	defer cancel()
	return queryAccessToken(ctx, db, hash)
}

func queryAccessToken(ctx gocontext.Context, db *sql.DB, hash string) *accessTokenStatus {
	var (
		id        string
		personID  string
		expiresAt sql.NullTime
		autoRenew bool
	)
	err := db.QueryRowContext(ctx, `SELECT id::text, person_id::text, expires_at, auto_renew FROM access_tokens WHERE value = $1`, hash).
		Scan(&id, &personID, &expiresAt, &autoRenew)
	if err != nil {
		if err == sql.ErrNoRows {
			return &accessTokenStatus{Status: "not_found"}
		}
		return &accessTokenStatus{Status: "unknown", Error: err.Error()}
	}

	out := &accessTokenStatus{
		ID:        id,
		PersonID:  personID,
		AutoRenew: autoRenew,
		Status:    "valid",
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		out.expiresTime = &t
		out.ExpiresAt = t.Format(time.RFC3339)
		if time.Until(t) <= 0 {
			out.Status = "expired"
		}
	}
	return out
}

func hashMissionControlAccessToken(token string) (string, error) {
	fields := strings.Split(token, ".")
	if len(fields) != 5 {
		return "", fmt.Errorf("invalid access token format")
	}

	timeCost, err := parseUint32(fields[2])
	if err != nil {
		return "", err
	}
	memoryCost, err := parseUint32(fields[3])
	if err != nil {
		return "", err
	}
	parallelism, err := parseUint8(fields[4])
	if err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(fields[0]), []byte(fields[1]), timeCost, memoryCost, parallelism, 20)
	return base64.URLEncoding.EncodeToString(hash), nil
}

func isMissionControlAccessTokenFormat(token string) bool {
	fields := strings.Split(token, ".")
	if len(fields) != 5 {
		return false
	}
	if _, err := parseUint32(fields[2]); err != nil {
		return false
	}
	if _, err := parseUint32(fields[3]); err != nil {
		return false
	}
	if _, err := parseUint8(fields[4]); err != nil {
		return false
	}
	return true
}

func parseUint32(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid access token format")
	}
	return uint32(n), nil
}

func parseUint8(s string) (uint8, error) {
	n, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid access token format")
	}
	return uint8(n), nil
}

func printWhoami(w io.Writer, report whoamiReport) {
	out, err := clicky.Format(whoamiText(report), clicky.Flags.FormatOptions)
	if err != nil {
		fmt.Fprintln(w, whoamiText(report).String())
		return
	}
	fmt.Fprint(w, out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(w)
	}
}

func whoamiText(report whoamiReport) clickyapi.Text {
	t := clicky.Text("Context", "font-bold text-blue-700")
	if report.Context.Error != "" {
		t = addWhoamiLine(t, "status", clicky.Text(report.Context.Error, "text-red-600"))
	}
	t = addWhoamiLine(t, "config", clicky.Text(report.Context.ConfigPath, "font-mono text-gray-600"))
	if report.Context.Name != "" {
		t = addWhoamiLine(t, "name", clicky.Text(report.Context.Name, "font-bold"))
	}
	if report.Context.SelectedBy != "" {
		t = addWhoamiLine(t, "selected by", clicky.Text(report.Context.SelectedBy, "text-gray-600"))
	}
	if report.Context.Server != "" {
		t = addWhoamiLine(t, "server", clicky.Text(report.Context.Server, "font-mono text-gray-700"))
	}
	if report.Context.DB != "" {
		t = addWhoamiLine(t, "db", clicky.Text(report.Context.DB, "font-mono text-gray-700"))
	}
	if len(report.Context.PropertyKeys) > 0 {
		t = addWhoamiLine(t, "properties", clicky.Text(strings.Join(report.Context.PropertyKeys, ", "), "text-gray-600"))
	}

	t = t.NewLine().NewLine().Append("Database", "font-bold text-blue-700")
	t = addWhoamiLine(t, "status", statusText(report.Database.Status, report.Database.Error))
	if report.Database.URL != "" {
		t = addWhoamiLine(t, "url", clicky.Text(report.Database.URL, "font-mono text-gray-700"))
	}
	if report.Database.Database != "" {
		t = addWhoamiLine(t, "database", clicky.Text(report.Database.Database, "font-bold"))
	}
	if report.Database.User != "" {
		t = addWhoamiLine(t, "user", clicky.Text(report.Database.User, "text-gray-700"))
	}
	if report.Database.Latency != "" {
		t = addWhoamiLine(t, "latency", clicky.Text(report.Database.Latency, "text-gray-600"))
	}

	t = t.NewLine().NewLine().Append("Auth", "font-bold text-blue-700")
	t = addWhoamiLine(t, "status", statusText(report.Auth.Status, report.Auth.Error))
	if report.Auth.Server != "" {
		t = addWhoamiLine(t, "server", clicky.Text(report.Auth.Server, "font-mono text-gray-700"))
	}
	if report.Auth.Endpoint != "" {
		t = addWhoamiLine(t, "endpoint", clicky.Text(report.Auth.Endpoint, "font-mono text-gray-700"))
	}
	if report.Auth.TokenSource != "" {
		t = addWhoamiLine(t, "token source", clicky.Text(report.Auth.TokenSource, "text-gray-700"))
	}
	if report.Auth.TokenExpires != "" {
		value := clicky.Text(report.Auth.TokenExpires, "font-mono text-gray-700")
		if report.Auth.TokenTTL != "" {
			value = value.Append(" ("+report.Auth.TokenTTL+")", "text-gray-500")
		}
		t = addWhoamiLine(t, "token expires", value)
	}
	if report.Auth.RefreshStatus != "" {
		t = addWhoamiLine(t, "refresh", refreshText(report.Auth.RefreshStatus))
	}
	if report.Auth.User != nil {
		t = addWhoamiLine(t, "user", clicky.Text(formatUser(report.Auth.User), "font-bold"))
	}
	if len(report.Auth.Roles) > 0 {
		t = addWhoamiLine(t, "roles", clicky.Text(strings.Join(report.Auth.Roles, ", "), "text-purple-600"))
	}
	if report.Auth.AccessToken != nil {
		value := statusText(report.Auth.AccessToken.Status, report.Auth.AccessToken.Error)
		if report.Auth.AccessToken.AutoRenew {
			value = value.Append(", auto-renew enabled", "text-green-600")
		}
		if report.Auth.AccessToken.Renewed {
			value = value.Append(", renewed", "text-green-600 font-bold")
		}
		if report.Auth.AccessToken.ExpiresAt != "" {
			value = value.Append(", expires "+report.Auth.AccessToken.ExpiresAt, "font-mono text-gray-600")
		}
		t = addWhoamiLine(t, "access token", value)
	}
	return t
}

func addWhoamiLine(t clickyapi.Text, label string, value clickyapi.Textable) clickyapi.Text {
	return t.NewLine().
		Append("  "+label+": ", "text-gray-500").
		Append(value)
}

func statusText(status, err string) clickyapi.Text {
	style := "text-gray-600"
	switch status {
	case "ok", "valid":
		style = "text-green-600 font-bold"
	case "error", "invalid", "expired", "not_found":
		style = "text-red-600 font-bold"
	case "skipped", "unknown":
		style = "text-gray-500"
	}
	return clicky.Text(statusLine(status, err), style)
}

func refreshText(status string) clickyapi.Text {
	style := "text-gray-600"
	switch {
	case status == "refreshed", status == "not needed":
		style = "text-green-600"
	case strings.HasPrefix(status, "failed"), strings.HasPrefix(status, "unavailable"):
		style = "text-red-600"
	case status == "needed":
		style = "text-yellow-600"
	}
	return clicky.Text(status, style)
}

func formatUser(user map[string]any) string {
	parts := []string{}
	for _, key := range []string{"email", "name", "id"} {
		if v, ok := user[key]; ok && fmt.Sprint(v) != "" {
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprint(user)
	}
	return strings.Join(parts, " ")
}

func statusLine(status, err string) string {
	if err == "" {
		return status
	}
	return status + " (" + err + ")"
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatTTL(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Until(t).Round(time.Second)
	if d < 0 {
		return "expired " + (-d).String() + " ago"
	}
	return "expires in " + d.String()
}

func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		return parsed.Redacted()
	}
	return raw
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func init() {
	WhoamiCmd.Flags().BoolVar(&whoamiJSON, "json", false, "Print status as JSON")
	WhoamiCmd.Flags().BoolVar(&whoamiRefresh, "refresh", true, "Refresh stored OIDC tokens before validating")
	Root.AddCommand(WhoamiCmd)
}
