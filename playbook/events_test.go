package playbook

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/events"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"gorm.io/gorm/clause"
)

var _ = ginkgo.Describe("Playbook Events", ginkgo.Ordered, func() {

	var _ = ginkgo.Describe("Config Events", ginkgo.Ordered, func() {
		var playbook models.Playbook
		var newConfigItems []string

		ginkgo.BeforeAll(func() {
			playbookSpec := v1.PlaybookSpec{
				Description: "write newly created config item's name to a file",
				On: &v1.PlaybookTrigger{
					PlaybookTriggerEvents: v1.PlaybookTriggerEvents{
						Config: []v1.PlaybookTriggerEvent{
							{
								Filter: fmt.Sprintf("config.config_class == '%s'", models.ConfigClassPod),
								Event:  "created",
							},
						},
					},
				},
				Actions: []v1.PlaybookAction{
					{
						Name: "write config name to a file",
						Exec: &v1.ExecAction{
							Script: "printf {{.config.name}} > /tmp/config-created.txt",
						},
					},
				},
			}

			spec, err := json.Marshal(playbookSpec)
			Expect(err).NotTo(HaveOccurred())

			playbook = models.Playbook{
				Name:   "new config writer",
				Spec:   spec,
				Source: models.SourceConfigFile,
			}

			err = DefaultContext.DB().Clauses(clause.Returning{}).Create(&playbook).Error
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.AfterAll(func() {
			err := DefaultContext.DB().Delete(&models.PlaybookRun{}, "playbook_id = ?", playbook.ID).Error
			Expect(err).NotTo(HaveOccurred())

			err = DefaultContext.DB().Delete(&models.ConfigItem{}, "id IN ?", newConfigItems).Error
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.It("create a new config item that doesn't pass the playbook filter", func() {
			newConfigItem := models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("airsonic"),
				ConfigClass: models.ConfigClassDeployment,
				Type:        lo.ToPtr("Kubernetes::Deployment"),
			}

			err := DefaultContext.DB().Clauses(clause.Returning{}).Create(&newConfigItem).Error
			Expect(err).NotTo(HaveOccurred())

			newConfigItems = append(newConfigItems, newConfigItem.ID.String())
		})

		ginkgo.It("Expect the event consumer to NOT save a playbook run", func() {
			events.ConsumeAll(DefaultContext)

			var playbooks []models.PlaybookRun
			err := DefaultContext.DB().Where("playbook_id = ?", playbook.ID).Find(&playbooks).Error
			Expect(err).NotTo(HaveOccurred())
			Expect(len(playbooks)).To(Equal(0))
		})

		ginkgo.It("create a new config item that passes the playbook filter", func() {
			newConfigItem := models.ConfigItem{
				ID:          uuid.New(),
				Name:        lo.ToPtr("airsonic"),
				ConfigClass: models.ConfigClassPod,
				Type:        lo.ToPtr("Kubernetes::Pod"),
			}

			err := DefaultContext.DB().Clauses(clause.Returning{}).Create(&newConfigItem).Error
			Expect(err).NotTo(HaveOccurred())

			newConfigItems = append(newConfigItems, newConfigItem.ID.String())
		})

		ginkgo.It("Expect the event consumer to save the playbook run", func() {
			events.ConsumeAll(DefaultContext)
			Eventually(func() models.PlaybookRunStatus {
				var run models.PlaybookRun
				DefaultContext.DB().Where("config_id = ? and playbook_id = ?", newConfigItems[1], playbook.ID).First(&run)
				return run.Status
			}, "5s", "200ms").Should(BeElementOf(models.PlaybookRunStatusScheduled, models.PlaybookRunStatusRunning, models.PlaybookRunStatusCompleted))
		})
	})

	var _ = ginkgo.Describe("Component Events", ginkgo.Ordered, func() {
		var playbook models.Playbook

		ginkgo.BeforeAll(func() {
			playbookSpec := v1.PlaybookSpec{
				Description: "write unhealthy component's name to a file",
				On: &v1.PlaybookTrigger{
					PlaybookTriggerEvents: v1.PlaybookTriggerEvents{
						Component: []v1.PlaybookTriggerEvent{
							{
								Filter: "component.type == 'Entity'",
								Event:  "unhealthy",
								Labels: map[string]string{
									"telemetry": "enabled",
								},
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

			err = DefaultContext.DB().Clauses(clause.Returning{}).Create(&playbook).Error
			Expect(err).NotTo(HaveOccurred())
		})

		ginkgo.It("update status to something else other than unhealthy", func() {
			tx := DefaultContext.DB().Model(&models.Component{}).Where("id = ?", dummy.Logistics.ID).UpdateColumn("status", types.ComponentStatusWarning)
			Expect(tx.RowsAffected).To(Equal(int64(1)))

			Expect(tx.Error).NotTo(HaveOccurred())
		})

		ginkgo.It("Expect the event consumer to NOT save a playbook run", func() {
			events.ConsumeAll(DefaultContext)

			var playbooks []models.PlaybookRun
			err := DefaultContext.DB().Where("playbook_id = ?", playbook.ID).Find(&playbooks).Error
			Expect(err).NotTo(HaveOccurred())
			Expect(len(playbooks)).To(Equal(0))
		})

		ginkgo.It("make one of the matching components unhealthy", func() {
			tx := DefaultContext.DB().Debug().Model(&models.Component{}).Where("id = ?", dummy.Logistics.ID).UpdateColumn("status", types.ComponentStatusUnhealthy)
			Expect(tx.RowsAffected).To(Equal(int64(1)))

			Expect(tx.Error).NotTo(HaveOccurred())
		})

		ginkgo.It("Expect the event consumer to save the playbook run", func() {
			events.ConsumeAll(DefaultContext)

			Eventually(func() models.PlaybookRunStatus {
				var run models.PlaybookRun
				DefaultContext.DB().Where("component_id = ? and playbook_id = ?", dummy.Logistics.ID, playbook.ID).First(&run)
				return run.Status
			}, "5s", "200ms").Should(BeElementOf(models.PlaybookRunStatusScheduled, models.PlaybookRunStatusRunning, models.PlaybookRunStatusCompleted))
		})
	})
})

var _ = ginkgo.Describe("Match Resource", func() {
	ginkgo.It("should match resources", func() {
		type args struct {
			labels        map[string]string
			eventResource EventResource
			matchFilters  []v1.PlaybookTriggerEvent
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
					matchFilters: []v1.PlaybookTriggerEvent{{Filter: "component.type == 'Entity'"}},
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
					matchFilters: []v1.PlaybookTriggerEvent{{Filter: "component.type == 'Entity'"}},
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
					matchFilters: []v1.PlaybookTriggerEvent{
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
					matchFilters: []v1.PlaybookTriggerEvent{
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
					matchFilters: []v1.PlaybookTriggerEvent{
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
					matchFilters: []v1.PlaybookTriggerEvent{
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
					matchFilters: []v1.PlaybookTriggerEvent{
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
					matchFilters: []v1.PlaybookTriggerEvent{
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
					matchFilters: []v1.PlaybookTriggerEvent{
						{Filter: "check.type"},
					},
				},
				want:    false,
				wantErr: true,
			},
		}

		for _, tt := range tests {
			ginkgo.By(tt.name, func() {
				got, err := matchResource(tt.args.labels, tt.args.eventResource.AsMap(), tt.args.matchFilters)
				Expect(err == nil).To(Equal(!tt.wantErr))
				Expect(got).To(Equal(tt.want))
			})
		}
	})
})
