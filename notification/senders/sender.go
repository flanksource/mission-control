package senders

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/duty/models"

	"github.com/flanksource/incident-commander/mail"
)

type Sender interface {
	Send(ctx context.Context, conn *models.Connection, data Data) error
}

type Data struct {
	Title       string
	Message     string
	Attachments []mail.Attachment
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func ForConnection(conn *models.Connection) (Sender, error) {
	switch conn.Type {
	case models.ConnectionTypeTelegram:
		return &Telegram{}, nil
	case models.ConnectionTypeDiscord:
		return &Discord{}, nil
	case models.ConnectionTypeTeams:
		return &Teams{}, nil
	case models.ConnectionTypeMattermost:
		return &Mattermost{}, nil
	case models.ConnectionTypeNtfy:
		return &Ntfy{}, nil
	case models.ConnectionTypePushbullet:
		return &Pushbullet{}, nil
	case models.ConnectionTypePushover:
		return &Pushover{}, nil
	default:
		return nil, fmt.Errorf("unsupported notification service: %s", conn.Type)
	}
}
