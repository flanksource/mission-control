package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/gomplate/v3"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/mail"
	"github.com/flanksource/incident-commander/utils"
	"github.com/google/uuid"
	"github.com/slack-go/slack"
)

const claimNotificationRecoverySQL = `UPDATE notification_deliveries SET lease_token = ?, lease_until = NOW() + INTERVAL '5 minutes', attempts = attempts + 1
   WHERE id = (SELECT id FROM notification_deliveries WHERE sent_at IS NOT NULL AND resolved_at IS NULL
    AND not_before <= NOW() AND (lease_until IS NULL OR lease_until < NOW()) ORDER BY not_before
    FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *`

func ReconcileNotificationRecoveriesJob(ctx context.Context) *job.Job {
	return &job.Job{Name: "NotificationRecoveries", Context: ctx, Schedule: "@every 15s", RunNow: true, Singleton: false, JobHistory: true, Retention: job.RetentionFailed,
		Fn: func(runtime job.JobRuntime) error { return ReconcileNotificationRecoveries(runtime.Context) }}
}

// ReconcileNotificationRecoveries claims bounded work without keeping database transactions open over transport calls.
func ReconcileNotificationRecoveries(ctx context.Context) error {
	ctx, cancel := ctx.WithTimeout(2 * time.Minute)
	defer cancel()
	var failures []error
	for range 20 {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		token := uuid.New()
		var receipts []models.NotificationDelivery
		err := ctx.DB().Raw(claimNotificationRecoverySQL, token).Scan(&receipts).Error
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if len(receipts) == 0 {
			break
		}
		receipt := receipts[0]
		err = resolveDelivery(ctx.WithSubject(receipt.NotificationID.String()), &receipt)
		values := map[string]any{"lease_until": nil, "lease_token": nil, "error": nil}
		if err != nil {
			delay := min(time.Hour, time.Duration(1<<min(receipt.Attempts, 10))*time.Second)
			if errors.Is(err, errRecoveryDeferred) {
				delay = 15 * time.Second
				values["status"] = "waiting-for-healthy"
				values["attempts"] = 0
			} else {
				values["status"] = "recovery-error"
				failures = append(failures, fmt.Errorf("recovery %s: %w", receipt.ID, err))
			}
			// Transport errors can contain credential-bearing URLs; only safe classifications are persisted here.
			values["error"] = "recovery deferred: " + recoveryErrorClass(err)
			values["not_before"] = time.Now().Add(delay)
		} else {
			values["status"] = "resolved"
			values["resolved_at"] = time.Now()
		}
		result := ctx.DB().Model(&models.NotificationDelivery{}).Where("id = ? AND lease_token = ?", receipt.ID, token).Updates(values)
		if result.Error != nil {
			failures = append(failures, result.Error)
		} else if result.RowsAffected != 1 {
			failures = append(failures, fmt.Errorf("recovery %s lease lost", receipt.ID))
		}
	}
	return errors.Join(failures...)
}

func recoveryErrorClass(err error) string {
	if errors.Is(err, errRecoveryDeferred) {
		return "resource is not stably healthy"
	}
	return "operation or authorization failed; inspect NotificationRecoveries job errors"
}

func stableRecoveryHealth(ctx context.Context, episode models.NotificationHealthEpisode, policy v1.NotificationOnResolved) (*models.NotificationHealthState, error) {
	state, err := currentRecoveryHealth(ctx, episode.ResourceType, episode.ResourceID)
	if err != nil {
		return nil, err
	}
	delay, err := policy.Delay()
	if err != nil {
		return nil, err
	}
	if state.Health != "healthy" || state.HealthySince == nil || time.Now().Before(state.HealthySince.Add(delay)) {
		return nil, errRecoveryDeferred
	}
	return state, nil
}

func resolveDelivery(ctx context.Context, receipt *models.NotificationDelivery) error {
	ctx, cancel := ctx.WithTimeout(2 * time.Minute)
	defer cancel()
	var episode models.NotificationHealthEpisode
	if err := ctx.DB().Where("id = ?", receipt.EpisodeID).First(&episode).Error; err != nil {
		return err
	}
	var policy v1.NotificationOnResolved
	if err := json.Unmarshal(receipt.Policy, &policy); err != nil {
		return err
	}
	state, err := stableRecoveryHealth(ctx, episode, policy)
	if err != nil {
		return err
	}
	var n models.Notification
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", receipt.NotificationID).First(&n).Error; err != nil {
		return fmt.Errorf("notification unavailable: %w", err)
	}
	var destination recoveryDestination
	if err := json.Unmarshal(receipt.Destination, &destination); err != nil {
		return err
	}
	env, err := recoveryEnvironment(ctx, episode, *state)
	if err != nil {
		return err
	}
	env = env.WithNotificationRef(receipt.NotificationID.String())
	body := recoveryBody(episode, *state, time.Now())
	if resource := env.SelectableResource(); resource != nil {
		body = strings.Replace(body, episode.ResourceType+"/"+episode.ResourceID.String(), resource.GetName()+" ("+episode.ResourceType+"/"+episode.ResourceID.String()+")", 1)
	}
	if policy.Template != "" {
		templateEnv := env.AsMap(ctx)
		templateEnv["resource"] = map[string]any{"id": episode.ResourceID.String(), "type": episode.ResourceType, "health": "healthy"}
		templateEnv["recoveredAt"] = episode.HealthyAt
		templateEnv["resolvedAt"] = time.Now()
		if episode.HealthyAt != nil {
			templateEnv["outageDuration"] = episode.HealthyAt.Sub(episode.StartedAt).String()
		}
		rendered, err := ctx.RunTemplate(gomplate.Template{Template: policy.Template}, templateEnv)
		if err != nil {
			return err
		}
		body = fmt.Sprint(rendered)
	}
	var conn *models.Connection
	if receipt.ConnectionID != nil {
		conn, err = connection.Get(ctx, receipt.ConnectionID.String())
		if err != nil {
			return err
		}
	}
	// Revalidate after credentials/template work, and separately before each external operation.
	revalidate := func() error {
		latest, err := stableRecoveryHealth(ctx, episode, policy)
		if err != nil {
			return err
		}
		if latest.Generation != state.Generation {
			return errRecoveryDeferred
		}
		return nil
	}
	complete := func(column string) error {
		result := ctx.DB().Model(&models.NotificationDelivery{}).Where("id = ? AND lease_token = ? AND lease_until > NOW()", receipt.ID, receipt.LeaseToken).Update(column, time.Now())
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("delivery lease lost after external success")
		}
		return nil
	}
	switch receipt.Transport {
	case "slack":
		if conn == nil || conn.Type != models.ConnectionTypeSlack {
			return fmt.Errorf("original Slack connection unavailable or changed type")
		}
		if receipt.Channel == "" || receipt.MessageID == "" {
			return fmt.Errorf("original Slack receipt is incomplete")
		}
		if err := policy.ValidateSlack(); err != nil {
			return err
		}
		client := newSlackClient(conn.Password)
		if policy.SlackReply() && receipt.ReplyAt == nil {
			if err := revalidate(); err != nil {
				return err
			}
			if _, _, err := client.PostMessageContext(ctx, receipt.Channel, slack.MsgOptionText(body, false), slack.MsgOptionTS(receipt.MessageID)); err != nil {
				return err
			}
			if err := complete("reply_at"); err != nil {
				return err
			}
		}
		if policy.SlackReaction() != "" && receipt.ReactionAt == nil {
			if err := revalidate(); err != nil {
				return err
			}
			err := client.AddReactionContext(ctx, policy.SlackReaction(), slack.ItemRef{Channel: receipt.Channel, Timestamp: receipt.MessageID})
			if err != nil && err.Error() != "already_reacted" {
				return err
			}
			if err := complete("reaction_at"); err != nil {
				return err
			}
		}
	case "smtp":
		if receipt.ReplyAt != nil {
			return nil
		}
		var smtp v1.ConnectionSMTP
		if conn == nil {
			smtp, err = mail.GetDefaultSMTP(ctx)
		} else {
			if conn.Type != models.ConnectionTypeEmail {
				return fmt.Errorf("original SMTP connection changed type")
			}
			smtp, err = v1.SMTPConnectionFromModel(*conn)
		}
		if err != nil {
			return err
		}
		if err := revalidate(); err != nil {
			return err
		}
		subject := destination.Subject
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
		message := mail.New(destination.To, subject, utils.MarkdownToHTML(body), `text/html; charset="UTF-8"`).SetFrom(destination.FromName, destination.From).
			SetHeader("Message-ID", fmt.Sprintf("<%s.resolved@notifications.mission-control>", receipt.ID)).
			SetHeader("In-Reply-To", receipt.MessageID).SetHeader("References", receipt.MessageID)
		if err := message.SendContext(ctx, smtp); err != nil {
			return err
		}
		if err := complete("reply_at"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported recorded recovery transport")
	}
	return nil
}
