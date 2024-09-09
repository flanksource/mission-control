package notification

import (
	"testing"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

func Test_shouldSilence(t *testing.T) {
	type args struct {
		silences []models.NotificationSilence
		celEnv   map[string]any
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "simple",
			args: args{
				silences: []models.NotificationSilence{
					{
						ID:    uuid.New(),
						From:  time.Now().Add(-time.Hour),
						Until: time.Now().Add(time.Hour),
					},
				},
				celEnv: map[string]any{},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shouldSilence(tt.args.silences, tt.args.celEnv)
			if (err != nil) != tt.wantErr {
				t.Errorf("shouldSilence() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("shouldSilence() = %v, want %v", got, tt.want)
			}
		})
	}
}
