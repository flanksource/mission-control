package mail

import (
	"os"
	"strconv"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var fallbackSMTP v1.ConnectionSMTP

// SetFallbackSMTP is called during startup to set CLI flag values.
func SetFallbackSMTP(fromAddress, fromName string) {
	fallbackSMTP.FromAddress = fromAddress
	fallbackSMTP.FromName = fromName
}

// GetDefaultSMTP returns the system SMTP configuration.
// Priority: DB connection named "system" with type email, then env vars, then CLI flags.
func GetDefaultSMTP(ctx context.Context) (v1.ConnectionSMTP, error) {
	smtp, err := loadSMTPFromDB(ctx)
	if err != nil {
		return v1.ConnectionSMTP{}, err
	} else if smtp != nil {
		return *smtp, nil
	}

	return buildFallbackSMTP(), nil
}

func loadSMTPFromDB(ctx context.Context) (*v1.ConnectionSMTP, error) {
	var conn models.Connection
	err := ctx.DB().
		Where("name = ? AND type = ? AND deleted_at IS NULL", "system", models.ConnectionTypeEmail).
		Limit(1).
		Find(&conn).Error
	if err != nil {
		return nil, err
	} else if conn.ID == uuid.Nil {
		return nil, nil
	}

	hydrated, err := ctx.HydrateConnection(&conn)
	if err != nil {
		return nil, err
	}

	smtp, err := v1.SMTPConnectionFromModel(*hydrated)
	if err != nil {
		return nil, err
	}

	return &smtp, nil
}

func buildFallbackSMTP() v1.ConnectionSMTP {
	smtp := v1.ConnectionSMTP{
		Host:        os.Getenv("SMTP_HOST"),
		Port:        587,
		FromAddress: fallbackSMTP.FromAddress,
		FromName:    fallbackSMTP.FromName,
		Auth:        v1.SMTPAuthPlain,
		Encryption:  v1.EncryptionExplicitTLS,
	}

	if portStr := os.Getenv("SMTP_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			smtp.Port = port
		}
	}

	if user := os.Getenv("SMTP_USER"); user != "" {
		_ = smtp.Username.Scan(user)
	}
	if pass := os.Getenv("SMTP_PASSWORD"); pass != "" {
		_ = smtp.Password.Scan(pass)
	}

	return smtp
}
