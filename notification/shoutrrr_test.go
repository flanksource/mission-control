package notification

import (
	"reflect"
	"testing"
)

func Test_getPropsForService(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			if got := getPropsForService(tt.args.service, tt.args.property); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getPropsForService() = %v, want %v", got, tt.want)
			}
		})
	}
}
