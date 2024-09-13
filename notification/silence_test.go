package notification

import (
	"testing"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

func TestSilenceSaveRequest_Validate(t *testing.T) {
	type fields struct {
		NotificationSilenceResource models.NotificationSilenceResource
		From                        string
		Until                       string
		Description                 string
		from                        time.Time
		until                       time.Time
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "empty from",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{},
				From:                        "",
				Until:                       "now+2d",
			},
			wantErr: true,
		},
		{
			name: "empty until",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{},
				From:                        "now",
				Until:                       "",
			},
			wantErr: true,
		},
		{
			name: "empty resource",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{},
				From:                        "now",
				Until:                       "now+2d",
			},
			wantErr: true,
		},
		{
			name: "valid",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{
					ConfigID: lo.ToPtr(uuid.NewString()),
				},
				From:  "now",
				Until: "now+2d",
			},
		},
		{
			name: "complete but invalid",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{
					ConfigID: lo.ToPtr(uuid.NewString()),
				},
				From:  "now",
				Until: "now-1m",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &SilenceSaveRequest{
				NotificationSilenceResource: tt.fields.NotificationSilenceResource,
				From:                        tt.fields.From,
				Until:                       tt.fields.Until,
				Description:                 tt.fields.Description,
				from:                        tt.fields.from,
				until:                       tt.fields.until,
			}
			if err := tr.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("SilenceSaveRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
