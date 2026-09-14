package v1

import (
	"encoding/json"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"sigs.k8s.io/yaml"
)

var _ = ginkgo.Describe("Notification onResolved policy", func() {
	ginkgo.It("defaults to disabled and default enabled Slack replies immediately", func() {
		var spec NotificationSpec
		Expect(json.Unmarshal([]byte(`{"events":["config.unhealthy"],"to":{"email":"a@example.com"}}`), &spec)).To(Succeed())
		Expect(spec.OnResolved).To(BeNil())
		Expect(spec.ValidateOnResolved()).To(Succeed())
		spec.OnResolved = &NotificationOnResolved{Enabled: true}
		Expect(spec.ValidateOnResolved()).To(Succeed())
		Expect(spec.OnResolved.SlackReply()).To(BeTrue())
		delay, err := spec.OnResolved.Delay()
		Expect(err).NotTo(HaveOccurred())
		Expect(delay).To(BeZero())
	})
	ginkgo.It("roundtrips YAML, JSON and deep copies reaction-only recovery", func() {
		original := Notification{Spec: NotificationSpec{Events: []string{"check.failed"}, To: NotificationRecipientSpec{Connection: "connection://slack/ops"}, OnResolved: &NotificationOnResolved{Enabled: true, WaitFor: "2m", Template: "Resolved {{ .check.name }}", Slack: &NotificationResolvedSlack{Reply: lo.ToPtr(false), Reaction: "white_check_mark"}}}}
		b, err := yaml.Marshal(original)
		Expect(err).NotTo(HaveOccurred())
		var parsed Notification
		Expect(yaml.Unmarshal(b, &parsed)).To(Succeed())
		Expect(parsed.Spec).To(Equal(original.Spec))
		Expect(parsed.Spec.OnResolved.ValidateSlack()).To(Succeed())
		copy := original.DeepCopy()
		*copy.Spec.OnResolved.Slack.Reply = true
		Expect(original.Spec.OnResolved.SlackReply()).To(BeFalse())
		delay, err := parsed.Spec.OnResolved.Delay()
		Expect(err).NotTo(HaveOccurred())
		Expect(delay).To(Equal(2 * time.Minute))
	})
	for _, test := range []struct {
		name string
		spec NotificationSpec
	}{
		{"negative delay", NotificationSpec{OnResolved: &NotificationOnResolved{Enabled: true, WaitFor: "-1s"}}},
		{"invalid delay", NotificationSpec{OnResolved: &NotificationOnResolved{Enabled: true, WaitFor: "later"}}},
		{"grouped", NotificationSpec{OnResolved: &NotificationOnResolved{Enabled: true}, GroupBy: []string{"type"}}},
		{"unsupported events", NotificationSpec{OnResolved: &NotificationOnResolved{Enabled: true}, Events: []string{"incident.created"}}},
		{"generic Slack", NotificationSpec{OnResolved: &NotificationOnResolved{Enabled: true}, To: NotificationRecipientSpec{URL: "slack://secret/channel"}}},
		{"playbook", NotificationSpec{OnResolved: &NotificationOnResolved{Enabled: true}, To: NotificationRecipientSpec{Playbook: lo.ToPtr("playbook")}}},
		{"unsupported fallback", NotificationSpec{OnResolved: &NotificationOnResolved{Enabled: true}, Fallback: &NotificationFallback{NotificationRecipientSpec: NotificationRecipientSpec{URL: "https://example.com"}}}},
	} {
		ginkgo.It("rejects "+test.name+" only when enabled", func() {
			Expect(test.spec.ValidateOnResolved()).To(HaveOccurred())
			test.spec.OnResolved.Enabled = false
			Expect(test.spec.ValidateOnResolved()).To(Succeed())
		})
	}
	ginkgo.It("rejects Slack no-op and malformed reaction choices", func() {
		Expect((NotificationOnResolved{Slack: &NotificationResolvedSlack{Reply: lo.ToPtr(false)}}).ValidateSlack()).To(HaveOccurred())
		Expect((NotificationOnResolved{Slack: &NotificationResolvedSlack{Reaction: ":check:"}}).ValidateSlack()).To(HaveOccurred())
	})
})
