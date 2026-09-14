package notification

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/utils"
)

func sendRecoverableSMTP(ctx *Context, conn v1.ConnectionSMTP, to []string, data NotificationTemplate, headerString string) error {
	headers, err := utils.StringToStringMap(headerString)
	if err != nil {
		return fmt.Errorf("invalid SMTP headers")
	}
	for key := range headers {
		switch strings.ToLower(key) {
		case "message-id", "in-reply-to", "references", "from", "to", "subject":
			return fmt.Errorf("onResolved reserves SMTP header %q", key)
		}
	}
	for i := range to {
		to[i] = strings.TrimSpace(to[i])
	}
	destination := recoveryDestination{To: to, From: conn.FromAddress, FromName: conn.FromName, Subject: data.Title}
	receipt, err := beginDelivery(ctx, "smtp", destination)
	if err != nil {
		return err
	}
	if receipt.SentAt != nil {
		return nil
	}
	if err := json.Unmarshal(receipt.Destination, &destination); err != nil {
		return err
	}
	m := mail.New(destination.To, destination.Subject, data.Message, `text/html; charset="UTF-8"`).SetFrom(destination.FromName, destination.From)
	for key, value := range headers {
		m.SetHeader(key, value)
	}
	m.SetHeader("Message-ID", receipt.MessageID)
	for _, a := range data.Attachments {
		m.AddAttachment(a)
	}
	sendCtx, cancel := ctx.Context.WithTimeout(2 * time.Minute)
	defer cancel()
	return finishDelivery(ctx, receipt, "", "", m.SendContext(sendCtx, conn))
}
