package clientcmd

import (
	gocontext "context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/incident-commander/auth/oidcclient"
	"golang.org/x/crypto/argon2"
)

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

// discoverOIDCEndpoints resolves the provider metadata for a server. Unlike the
// grant itself, discovery is idempotent and safe to attempt against every
// candidate — it spends nothing.
func discoverOIDCEndpoints(server string) (*oidcclient.Discovery, error) {
	var lastErr error
	for _, candidate := range oidcServerCandidates(server) {
		endpoints, err := oidcclient.Discover(strings.TrimRight(candidate, "/") + "/.well-known/openid-configuration")
		if err == nil {
			return endpoints, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("OIDC discovery failed for %s: %w", server, lastErr)
}

// contextTokenEndpoint returns the token endpoint for a context, discovering
// and caching it in config.json on first use so later commands skip the two
// discovery round-trips. Failing to write that cache is not fatal — it holds no
// credential, and the refresh it guards is already in flight.
func contextTokenEndpoint(cfg *MCConfig, mcCtx *MCContext) (string, error) {
	if mcCtx.Endpoints != nil && mcCtx.Endpoints.TokenEndpoint != "" {
		return mcCtx.Endpoints.TokenEndpoint, nil
	}

	endpoints, err := discoverOIDCEndpoints(mcCtx.Server)
	if err != nil {
		return "", err
	}
	mcCtx.Endpoints = endpoints

	if stored := cfg.GetContext(mcCtx.Name); stored != nil {
		stored.Endpoints = endpoints
		if err := saveConfigLocked(cfg); err != nil {
			logger.Debugf("failed to cache OIDC endpoints for context %q: %v", mcCtx.Name, err)
		}
	}
	return endpoints.TokenEndpoint, nil
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
