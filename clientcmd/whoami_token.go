package clientcmd

import (
	gocontext "context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

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

func refreshOIDCTokens(server string, tokens *oidcclient.Tokens) (*oidcclient.Tokens, error) {
	if tokens == nil {
		return nil, fmt.Errorf("no OIDC tokens")
	}
	if tokens.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token")
	}

	var lastErr error
	for _, candidate := range oidcServerCandidates(server) {
		endpoints, err := oidcclient.Discover(strings.TrimRight(candidate, "/") + "/.well-known/openid-configuration")
		if err != nil {
			lastErr = err
			continue
		}
		refreshed, err := oidcclient.RefreshToken(endpoints.TokenEndpoint, tokens.RefreshToken)
		if err != nil {
			lastErr = err
			continue
		}
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = tokens.RefreshToken
		}
		if refreshed.IDToken == "" {
			refreshed.IDToken = tokens.IDToken
		}
		return refreshed, nil
	}
	return nil, lastErr
}

func updateContextOIDCTokens(cfg *MCConfig, name string, tokens *oidcclient.Tokens) {
	if cfg == nil || name == "" || tokens == nil {
		return
	}
	ctx := cfg.GetContext(name)
	if ctx == nil {
		return
	}
	ctx.SetOIDCTokens(tokens)
	if err := SaveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "failed to update context OIDC tokens: %v\n", err)
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
