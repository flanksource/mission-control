package notification_test

import (
	"fmt"
	"reflect"

	"github.com/flanksource/incident-commander/notification"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Notification properties", func() {
	type args struct {
		service  string
		property map[string]string
	}

	notificationProperties := map[string]string{
		"email.from":    "no-reply@flanksource.com",
		"email.subject": "hey",
		"slack.subject": "hello",
		"slack.color":   "good",
	}

	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "for email",
			args: args{
				service:  "email",
				property: notificationProperties,
			},
			want: map[string]string{
				"from":    "no-reply@flanksource.com",
				"subject": "hey",
			},
		},
		{
			name: "for slack",
			args: args{
				service:  "slack",
				property: notificationProperties,
			},
			want: map[string]string{
				"color":   "good",
				"subject": "hello",
			},
		},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			if got := notification.GetPropsForService(tt.args.service, tt.args.property); !reflect.DeepEqual(got, tt.want) {
				ginkgo.Fail(fmt.Sprintf("GetPropsForService() = %v, want %v", tt.args, tt.want))
			}
		})
	}
})

var _ = ginkgo.Describe("Shoutrrr", func() {
	ginkgo.It("should render smtp html", func() {
		ctx := notification.NewContext(DefaultContext, uuid.Nil)
		payload := notification.NotificationMessagePayload{
			Title:       "Test Notification",
			Description: "My Test Config",
		}
		url := "smtp://username:password@host:25/?from=test@flanksource.com&to=receiver@flanksource.com"

		_, _, _, data, err := notification.PrepareShoutrrr(ctx, url, payload, nil)
		Expect(err).To(BeNil())
		Expect(data.Message).To(ContainSubstring("My Test Config"))
	})
})
