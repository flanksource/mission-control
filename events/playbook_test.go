package events

import (
	"encoding/json"
	"testing"

	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm/clause"
)

var _ = ginkgo.Describe("Should save playbook run on the correct event", ginkgo.Ordered, func() {
	var playbook models.Playbook

	ginkgo.It("should store dummy data", func() {
		dataset := dummy.GetStaticDummyData()
		err := dataset.Populate(playbookDB)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("should create a new playbook", func() {
		playbookSpec := v1.PlaybookSpec{
			Description: "write unhealthy component's name to a file",
			On: v1.PlaybookEvent{
				Component: []v1.PlaybookEventDetail{
					{
						Filter: "component.type == 'Entity'",
						Event:  "unhealthy",
						Labels: map[string]string{
							"telemetry": "enabled",
						},
					},
				},
			},
			Actions: []v1.PlaybookAction{
				{
					Name: "write component name to a file",
					Exec: &v1.ExecAction{
						Script: "printf {{.component.name}} > /tmp/component-name.txt",
					},
				},
			},
		}

		spec, err := json.Marshal(playbookSpec)
		Expect(err).NotTo(HaveOccurred())

		playbook = models.Playbook{
			Name:   "unhealthy component writer",
			Spec:   spec,
			Source: models.SourceConfigFile,
		}

		err = playbookDB.Clauses(clause.Returning{}).Create(&playbook).Error
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("update status to something else other than unhealthy", func() {
		tx := playbookDB.Model(&models.Component{}).Where("id = ?", dummy.Logistics.ID).UpdateColumn("status", types.ComponentStatusWarning)
		Expect(tx.RowsAffected).To(Equal(int64(1)))

		Expect(tx.Error).NotTo(HaveOccurred())
	})

	ginkgo.It("Expect the event consumer to NOT save a playbook run", func() {
		componentEventConsumer, err := NewComponentConsumerSync().EventConsumer()
		Expect(err).NotTo(HaveOccurred())
		componentEventConsumer.ConsumeUntilEmpty(api.NewContext(playbookDB, playbookDBPool))

		var playbooks []models.PlaybookRun
		err = playbookDB.Find(&playbooks).Error
		Expect(err).NotTo(HaveOccurred())
		Expect(len(playbooks)).To(Equal(0))
	})

	ginkgo.It("make one of the matching components unhealthy", func() {
		tx := playbookDB.Debug().Model(&models.Component{}).Where("id = ?", dummy.Logistics.ID).UpdateColumn("status", types.ComponentStatusUnhealthy)
		Expect(tx.RowsAffected).To(Equal(int64(1)))

		Expect(tx.Error).NotTo(HaveOccurred())
	})

	ginkgo.It("Expect the event consumer to save the playbook run", func() {
		componentEventConsumer, err := NewComponentConsumerSync().EventConsumer()
		Expect(err).NotTo(HaveOccurred())
		componentEventConsumer.ConsumeUntilEmpty(api.NewContext(playbookDB, playbookDBPool))

		var playbook models.PlaybookRun
		err = playbookDB.Where("component_id", dummy.Logistics.ID).First(&playbook).Error
		Expect(err).NotTo(HaveOccurred())

		Expect(playbook.Status).To(Equal(models.PlaybookRunStatusScheduled))
	})
})

func Test_matchResource(t *testing.T) {
	type args struct {
		labels        map[string]string
		eventResource EventResource
		matchFilters  []v1.PlaybookEventDetail
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "With Filter | Without Labels | Match",
			args: args{
				labels: map[string]string{
					"telemetry": "enabled",
				},
				eventResource: EventResource{
					Component: &models.Component{
						Type: "Entity",
					},
				},
				matchFilters: []v1.PlaybookEventDetail{{Filter: "component.type == 'Entity'"}},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "With Filter | Without Labels | No match",
			args: args{
				eventResource: EventResource{
					Component: &models.Component{
						Type: "Database",
					},
				},
				matchFilters: []v1.PlaybookEventDetail{{Filter: "component.type == 'Entity'"}},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "Without Filter | With Labels | Match",
			args: args{
				labels: map[string]string{
					"telemetry": "enabled",
				},
				eventResource: EventResource{},
				matchFilters: []v1.PlaybookEventDetail{
					{
						Labels: map[string]string{
							"telemetry": "enabled",
						},
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "Without Filter | With Labels | No match",
			args: args{
				labels: map[string]string{
					"telemetry": "enabled",
				},
				eventResource: EventResource{},
				matchFilters: []v1.PlaybookEventDetail{
					{
						Labels: map[string]string{
							"telemetry": "enabled",
							"env":       "production",
						},
					},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "With Filter | With Labels | match",
			args: args{
				labels: map[string]string{
					"telemetry": "enabled",
				},
				eventResource: EventResource{
					Check: &models.Check{
						Type: "http",
					},
				},
				matchFilters: []v1.PlaybookEventDetail{
					{
						Labels: map[string]string{
							"telemetry": "enabled",
						},
						Filter: "check.type == 'http'",
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "With Filter | With Labels | no match",
			args: args{
				labels: map[string]string{
					"telemetry": "enabled",
				},
				eventResource: EventResource{
					Check: &models.Check{
						Type: "http",
					},
				},
				matchFilters: []v1.PlaybookEventDetail{
					{
						Labels: map[string]string{
							"telemetry": "enabled",
						},
						Filter: "check.type == 'exec'",
					},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "With Filter | With Labels | one of the filters match",
			args: args{
				labels: map[string]string{
					"telemetry": "enabled",
				},
				eventResource: EventResource{
					Check: &models.Check{
						Type: "http",
					},
					CheckSummary: &models.CheckSummary{
						Uptime: types.Uptime{
							Failed: 12,
						},
					},
				},
				matchFilters: []v1.PlaybookEventDetail{
					{
						Labels: map[string]string{
							"telemetry": "enabled",
							"env":       "production",
						},
					},
					{Filter: "check.type == 'http' && check_summary.uptime.failed > 15"},
					{Filter: "check.type == 'http' && check_summary.uptime.failed > 10"},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "Invalid filter expression",
			args: args{
				eventResource: EventResource{
					Check: &models.Check{
						Type: "http",
					},
					CheckSummary: &models.CheckSummary{
						Uptime: types.Uptime{
							Failed: 12,
						},
					},
				},
				matchFilters: []v1.PlaybookEventDetail{
					{Filter: "summary.uptime.failed > 15"},
				},
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "Expression not returning boolean",
			args: args{
				eventResource: EventResource{
					Check: &models.Check{
						Type: "http",
					},
					CheckSummary: &models.CheckSummary{
						Uptime: types.Uptime{
							Failed: 12,
						},
					},
				},
				matchFilters: []v1.PlaybookEventDetail{
					{Filter: "check.type"},
				},
			},
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matchResource(tt.args.labels, tt.args.eventResource.AsMap(), tt.args.matchFilters)
			if (err != nil) != tt.wantErr {
				t.Errorf("matchResource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("matchResource() = %v, want %v", got, tt.want)
			}
		})
	}
}
