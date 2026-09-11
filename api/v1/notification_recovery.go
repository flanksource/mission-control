package v1

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
)

// NotificationOnResolved updates destinations that actually received an unhealthy notification.
type NotificationOnResolved struct {
	Enabled  bool                       `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	WaitFor  string                     `json:"waitFor,omitempty" yaml:"waitFor,omitempty"`
	Template string                     `json:"template,omitempty" yaml:"template,omitempty"`
	Slack    *NotificationResolvedSlack `json:"slack,omitempty" yaml:"slack,omitempty"`
}

type NotificationResolvedSlack struct {
	Reaction string `json:"reaction,omitempty" yaml:"reaction,omitempty"`
	Reply    *bool  `json:"reply,omitempty" yaml:"reply,omitempty"`
}

func (r NotificationOnResolved) SlackReply() bool {
	return r.Slack == nil || r.Slack.Reply == nil || *r.Slack.Reply
}
func (r NotificationOnResolved) SlackReaction() string {
	if r.Slack == nil {
		return ""
	}
	return r.Slack.Reaction
}
func (r NotificationOnResolved) Delay() (time.Duration, error) {
	if r.WaitFor == "" {
		return 0, nil
	}
	d, err := duration.ParseDuration(r.WaitFor)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("onResolved.waitFor must be a nonnegative duration")
	}
	return time.Duration(d), nil
}
func (r NotificationOnResolved) ValidateSlack() error {
	if !r.SlackReply() && r.SlackReaction() == "" {
		return fmt.Errorf("onResolved.slack requires reply or reaction")
	}
	if strings.ContainsAny(r.SlackReaction(), ": \r\n\t") {
		return fmt.Errorf("onResolved.slack.reaction must be an emoji name without colons")
	}
	return nil
}
func (s NotificationSpec) ValidateOnResolved() error {
	if s.OnResolved == nil || !s.OnResolved.Enabled {
		return nil
	}
	if _, err := s.OnResolved.Delay(); err != nil {
		return err
	}
	if len(s.GroupBy) != 0 {
		return fmt.Errorf("onResolved does not support grouped notifications")
	}
	for _, event := range s.Events {
		switch event {
		case "config.healthy", "config.warning", "config.unhealthy", "config.degraded", "component.healthy", "component.warning", "component.unhealthy", "component.degraded", "check.passed", "check.failed":
		default:
			return fmt.Errorf("onResolved does not support event %q", event)
		}
	}
	for _, recipient := range []NotificationRecipientSpec{s.To, func() NotificationRecipientSpec {
		if s.Fallback != nil {
			return s.Fallback.NotificationRecipientSpec
		}
		return NotificationRecipientSpec{}
	}()} {
		if recipient.Playbook != nil || recipient.Webhook != nil {
			return fmt.Errorf("onResolved does not support playbook or webhook recipients")
		}
		if recipient.URL != "" && !strings.HasPrefix(recipient.URL, "smtp://system/") {
			return fmt.Errorf("onResolved requires native Slack, named SMTP or system SMTP")
		}
	}
	return nil
}
