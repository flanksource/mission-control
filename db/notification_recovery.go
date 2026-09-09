package db

import (
	"encoding/json"
	"fmt"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

// ValidateRecoveryRecipients checks routing metadata; dispatch authorizes connections and resolves credentials.
func ValidateRecoveryRecipients(ctx context.Context, spec v1.NotificationSpec) error {
	if spec.OnResolved == nil || !spec.OnResolved.Enabled {
		return nil
	}
	recipients := []v1.NotificationRecipientSpec{spec.To}
	if spec.Fallback != nil {
		recipients = append(recipients, spec.Fallback.NotificationRecipientSpec)
	}
	for _, recipient := range recipients {
		configs := []api.NotificationConfig{{Connection: recipient.Connection, URL: recipient.URL, Webhook: recipient.Webhook}}
		if recipient.Team != "" {
			team, err := query.FindTeam(ctx, recipient.Team, query.GetterOptionNoCache)
			if err != nil {
				return err
			}
			if team == nil {
				return fmt.Errorf("team %q not found", recipient.Team)
			}
			var teamSpec api.TeamSpec
			if err := json.Unmarshal(team.Spec, &teamSpec); err != nil {
				return err
			}
			configs = teamSpec.Notifications
		}
		for _, config := range configs {
			if config.Webhook != nil {
				return fmt.Errorf("onResolved does not support webhook recipients")
			}
			if config.Connection != "" {
				conn, err := context.FindConnectionByURL(ctx, config.Connection)
				if err != nil {
					return err
				}
				if conn == nil {
					return dutyAPI.Errorf(dutyAPI.ENOTFOUND, "connection (%s) not found", config.Connection)
				}
				switch conn.Type {
				case models.ConnectionTypeSlack:
					if err := spec.OnResolved.ValidateSlack(); err != nil {
						return err
					}
				case models.ConnectionTypeEmail:
				default:
					return fmt.Errorf("onResolved does not support connection type %q", conn.Type)
				}
			} else if config.URL != "" && !strings.HasPrefix(config.URL, api.SystemSMTP) {
				return fmt.Errorf("onResolved requires native Slack, named SMTP or system SMTP")
			}
		}
	}
	return nil
}
