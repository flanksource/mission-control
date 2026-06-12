package clientcmd

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/spf13/cobra"
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
		decoded, statusCode, err := NewAPIClientForServer(base, token).Whoami(ctx)
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

func init() {
	WhoamiCmd.Flags().BoolVar(&whoamiJSON, "json", false, "Print status as JSON")
	WhoamiCmd.Flags().BoolVar(&whoamiRefresh, "refresh", true, "Refresh stored OIDC tokens before validating")
}
