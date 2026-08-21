package cmd

import (
	gocontext "context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/duty"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/incident-commander/clientcmd"
	"golang.org/x/crypto/argon2"
)

const (
	maxAccessTokenTimeCost    uint32 = 10
	maxAccessTokenMemoryCost  uint32 = 256 * 1024
	maxAccessTokenParallelism uint8  = 64
)

type localWhoamiOps struct{}

func (localWhoamiOps) DefaultDBConnection() string {
	return dutyAPI.DefaultConfig.ReadEnv().ConnectionString
}

func (localWhoamiOps) ProbeDatabase(conn string) clientcmd.WhoamiDatabase {
	out := clientcmd.WhoamiDatabase{Configured: conn != ""}
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

	if err := db.QueryRowContext(ctx, "SELECT current_database(), current_user").Scan(&out.Database, &out.User); err != nil {
		out.Status = "error"
		out.Error = err.Error()
		out.Latency = time.Since(start).Round(time.Millisecond).String()
		return out
	}

	out.Status = "ok"
	out.Latency = time.Since(start).Round(time.Millisecond).String()
	return out
}

func (localWhoamiOps) InspectAccessToken(conn, token string) *clientcmd.AccessTokenStatus {
	hash, err := hashMissionControlAccessToken(token)
	if err != nil {
		return nil
	}
	db, err := duty.NewDB(conn)
	if err != nil {
		return &clientcmd.AccessTokenStatus{Status: "unknown", Error: err.Error()}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)

	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 10*time.Second)
	defer cancel()
	return queryAccessToken(ctx, db, hash)
}

func queryAccessToken(ctx gocontext.Context, db *sql.DB, hash string) *clientcmd.AccessTokenStatus {
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
			return &clientcmd.AccessTokenStatus{Status: "not_found"}
		}
		return &clientcmd.AccessTokenStatus{Status: "unknown", Error: err.Error()}
	}

	out := &clientcmd.AccessTokenStatus{
		ID:        id,
		PersonID:  personID,
		AutoRenew: autoRenew,
		Status:    "valid",
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		out.ExpiresTime = &t
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
	if timeCost == 0 || timeCost > maxAccessTokenTimeCost ||
		memoryCost > maxAccessTokenMemoryCost ||
		parallelism == 0 || parallelism > maxAccessTokenParallelism {
		return "", fmt.Errorf("invalid access token format")
	}

	hash := argon2.IDKey([]byte(fields[0]), []byte(fields[1]), timeCost, memoryCost, parallelism, 20)
	return base64.URLEncoding.EncodeToString(hash), nil
}

func parseUint32(value string) (uint32, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid access token format")
	}
	return uint32(n), nil
}

func parseUint8(value string) (uint8, error) {
	n, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid access token format")
	}
	return uint8(n), nil
}

func init() {
	clientcmd.LocalWhoami = localWhoamiOps{}
}
