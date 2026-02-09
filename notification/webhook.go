package notification

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/models"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
)

// WebhookPayload is the structured JSON payload sent to webhook endpoints.
type WebhookPayload struct {
	Title     string `json:"title"`
	Message   string `json:"message"`
	Event     string `json:"event"`
	Permalink string `json:"permalink"`
}

func sendWebhookNotification(ctx *Context, celVars *celVariables, payload NotificationEventPayload, webhook *api.NotificationWebhookReceiver, notification *NotificationWithSpec) error {
	start := time.Now()

	hydrated, err := webhook.HTTPConnection.Hydrate(ctx, ctx.GetNamespace())
	if err != nil {
		notificationSendFailureCounter.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Inc()
		return fmt.Errorf("failed to hydrate webhook connection: %w", err)
	}
	webhook.HTTPConnection = *hydrated

	client, err := connection.CreateHTTPClient(ctx, webhook.HTTPConnection)
	if err != nil {
		notificationSendFailureCounter.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Inc()
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}

	defaultTitle, defaultBody := DefaultTitleAndBody(payload, celVars)
	templater := ctx.NewStructTemplater(celVars.AsMap(ctx.Context), "", TemplateFuncs)
	data := NotificationTemplate{
		Title:   lo.CoalesceOrEmpty(notification.Title, defaultTitle),
		Message: lo.CoalesceOrEmpty(notification.Template, defaultBody),
	}
	if err := templater.Walk(&data); err != nil {
		notificationSendFailureCounter.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Inc()
		return fmt.Errorf("error templating notification: %w", err)
	}

	permalink := celVars.Permalink

	webhookPayload := WebhookPayload{
		Title:     data.Title,
		Message:   data.Message,
		Event:     payload.EventName,
		Permalink: permalink,
	}

	method := lo.CoalesceOrEmpty(strings.ToUpper(webhook.Method), "POST")
	req := client.R(ctx.Context)
	if err := req.Body(webhookPayload); err != nil {
		notificationSendFailureCounter.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Inc()
		return fmt.Errorf("failed to encode webhook payload: %w", err)
	}

	resp, err := req.Do(method, webhook.HTTPConnection.URL)
	if err != nil {
		notificationSendFailureCounter.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Inc()
		return fmt.Errorf("failed to send webhook: %w", err)
	}

	if !resp.IsOK() {
		notificationSendFailureCounter.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Inc()
		body, _ := resp.AsString()
		return fmt.Errorf("webhook returned non-OK status %d: %s", resp.StatusCode, body)
	}

	ctx.log.Status = models.NotificationStatusSent
	ctx.WithMessage(data.Message)
	notificationSentCounter.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Inc()
	notificationSendDuration.WithLabelValues("webhook", string(RecipientTypeWebhook), ctx.notificationID.String()).Observe(time.Since(start).Seconds())

	return nil
}
