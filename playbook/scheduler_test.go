package playbook

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/api"
	pkgEvents "github.com/flanksource/incident-commander/events"
)

type TestResource struct {
	Check     *models.Check
	Canary    *models.Canary
	Config    *models.ConfigItem
	Component *models.Component
}

type TestCase struct {
	Name                  string
	Resources             TestResource
	DatabaseChange        func(context.Context, TestResource) error
	IgnoreEvents          []string
	ExpectedEvents        []string
	ExpectedEventResource func(TestResource) pkgEvents.EventResource
}

var _ = ginkgo.Describe("Playbook Scheduler EventResource Generation", ginkgo.Ordered, func() {
	var ctx context.Context
	var cleanup func()

	ginkgo.BeforeAll(func() {
		// Create an isolated database so that other tests do not interfere with this one
		var err error
		isolatedContextPtr, cleanupFunc, err := setup.NewDB(DefaultContext, "playbook_scheduler_test")
		Expect(err).NotTo(HaveOccurred())

		ctx = *isolatedContextPtr
		cleanup = cleanupFunc
	})

	ginkgo.AfterAll(func() {
		if cleanup != nil {
			cleanup()
		}
	})

	ginkgo.AfterEach(func() {
		// Clear event queue after each test to ensure isolation
		err := ctx.DB().Exec("DELETE FROM event_queue").Error
		Expect(err).NotTo(HaveOccurred())
	})

	testEvent := func(tc TestCase) {
		ginkgo.It(tc.Name, func() {
			// Create test resources
			if tc.Resources.Canary != nil {
				err := ctx.DB().Create(tc.Resources.Canary).Error
				Expect(err).NotTo(HaveOccurred())
				ginkgo.DeferCleanup(func() {
					ctx.DB().Delete(tc.Resources.Canary)
				})
			}

			if tc.Resources.Check != nil {
				tc.Resources.Check.CanaryID = tc.Resources.Canary.ID
				err := ctx.DB().Create(tc.Resources.Check).Error
				Expect(err).NotTo(HaveOccurred())
				ginkgo.DeferCleanup(func() {
					ctx.DB().Delete(tc.Resources.Check)
				})
			}

			if tc.Resources.Config != nil {
				err := ctx.DB().Create(tc.Resources.Config).Error
				Expect(err).NotTo(HaveOccurred())
				ginkgo.DeferCleanup(func() {
					ctx.DB().Delete(tc.Resources.Config)
				})
			}

			if tc.Resources.Component != nil {
				err := ctx.DB().Create(tc.Resources.Component).Error
				Expect(err).NotTo(HaveOccurred())
				ginkgo.DeferCleanup(func() {
					ctx.DB().Delete(tc.Resources.Component)
				})
			}

			// Make database change to trigger event
			err := tc.DatabaseChange(ctx, tc.Resources)
			Expect(err).NotTo(HaveOccurred())

			var events []models.Event
			query := ctx.DB().Order("created_at ASC")
			if len(tc.IgnoreEvents) > 0 {
				query = query.Where("name NOT IN ?", tc.IgnoreEvents)
			}
			Expect(query.Find(&events).Error).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(len(tc.ExpectedEvents)), "Should have events %v", tc.ExpectedEvents)

			eventNames := lo.Map(events, func(event models.Event, _ int) string {
				return event.Name
			})
			Expect(eventNames).To(ConsistOf(tc.ExpectedEvents))

			for _, event := range events {
				actualEventResource, err := pkgEvents.BuildEventResource(ctx, event)
				Expect(err).NotTo(HaveOccurred())

				expectedEventResource := tc.ExpectedEventResource(tc.Resources)
				if expectedEventResource.Check != nil {
					Expect(actualEventResource.Check).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"ID":       Equal(expectedEventResource.Check.ID),
						"Name":     Equal(expectedEventResource.Check.Name),
						"Type":     Equal(expectedEventResource.Check.Type),
						"CanaryID": Equal(expectedEventResource.Check.CanaryID),
					})))
					Expect(actualEventResource.CheckSummary).NotTo(BeNil())
				}
				if expectedEventResource.Config != nil {
					Expect(actualEventResource.Config).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"ID":          Equal(expectedEventResource.Config.ID),
						"Name":        Equal(expectedEventResource.Config.Name),
						"ConfigClass": Equal(expectedEventResource.Config.ConfigClass),
					})))
				}
				if expectedEventResource.Component != nil {
					Expect(actualEventResource.Component).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"ID":   Equal(expectedEventResource.Component.ID),
						"Name": Equal(expectedEventResource.Component.Name),
						"Type": Equal(expectedEventResource.Component.Type),
					})))
				}
				if expectedEventResource.Canary != nil {
					Expect(actualEventResource.Canary).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"ID":   Equal(expectedEventResource.Canary.ID),
						"Name": Equal(expectedEventResource.Canary.Name),
					})))
				}
			}
		})
	}

	var _ = ginkgo.Describe("Check Events", ginkgo.Ordered, func() {
		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered check.passed event",
			Resources: TestResource{
				Canary: &models.Canary{
					ID:   uuid.New(),
					Name: "test-canary-passed",
					Spec: []byte(`{"http": [{"name": "test", "url": "http://example.com"}]}`),
				},
				Check: &models.Check{
					ID:       uuid.New(),
					CanaryID: uuid.New(),
					Name:     "test-check-passed",
					Type:     "http",
					Status:   models.CheckStatusUnhealthy, // Start as unhealthy
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				res.Check.CanaryID = res.Canary.ID
				Expect(ctx.DB().Save(res.Check).Error).NotTo(HaveOccurred())
				return ctx.DB().Model(res.Check).UpdateColumn("status", models.CheckStatusHealthy).Error
			},
			ExpectedEvents: []string{api.EventCheckPassed},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Check:  res.Check,
					Canary: res.Canary,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered check.failed event",
			Resources: TestResource{
				Canary: &models.Canary{
					ID:   uuid.New(),
					Name: "test-canary-failed",
					Spec: []byte(`{"http": [{"name": "test", "url": "http://example.com"}]}`),
				},
				Check: &models.Check{
					ID:     uuid.New(),
					Name:   "test-check-failed",
					Type:   "http",
					Status: models.CheckStatusHealthy, // Start as healthy
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Check).UpdateColumn("status", models.CheckStatusUnhealthy).Error
			},
			ExpectedEvents: []string{api.EventCheckFailed},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Check:  res.Check,
					Canary: res.Canary,
				}
			},
		})
	})

	var _ = ginkgo.Describe("Component Events", func() {
		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered component.unhealthy event",
			Resources: TestResource{
				Component: &models.Component{
					ID:     uuid.New(),
					Name:   "test-component-unhealthy",
					Type:   "Entity",
					Health: lo.ToPtr(models.HealthHealthy), // Start as healthy
					Labels: map[string]string{"telemetry": "enabled"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Component).UpdateColumn("health", models.HealthUnhealthy).Error
			},
			ExpectedEvents: []string{api.EventComponentUnhealthy},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Component: res.Component,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered component.healthy event",
			Resources: TestResource{
				Component: &models.Component{
					ID:     uuid.New(),
					Name:   "test-component-healthy",
					Type:   "Entity",
					Health: lo.ToPtr(models.HealthUnhealthy), // Start as unhealthy
					Labels: map[string]string{"telemetry": "enabled"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Component).UpdateColumn("health", models.HealthHealthy).Error
			},
			ExpectedEvents: []string{api.EventComponentHealthy},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Component: res.Component,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered component.warning event",
			Resources: TestResource{
				Component: &models.Component{
					ID:     uuid.New(),
					Name:   "test-component-warning",
					Type:   "Entity",
					Health: lo.ToPtr(models.HealthHealthy), // Start as healthy
					Labels: map[string]string{"telemetry": "enabled"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Component).UpdateColumn("health", models.HealthWarning).Error
			},
			ExpectedEvents: []string{api.EventComponentWarning},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Component: res.Component,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered component.unknown event",
			Resources: TestResource{
				Component: &models.Component{
					ID:     uuid.New(),
					Name:   "test-component-unknown",
					Type:   "Entity",
					Health: lo.ToPtr(models.HealthHealthy), // Start as healthy
					Labels: map[string]string{"telemetry": "enabled"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Component).UpdateColumn("health", "").Error
			},
			ExpectedEvents: []string{api.EventComponentUnknown},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Component: res.Component,
				}
			},
		})
	})

	var _ = ginkgo.Describe("Config Health Events", func() {
		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.unhealthy event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-unhealthy"),
					ConfigClass: models.ConfigClassDeployment,
					Type:        lo.ToPtr("Kubernetes::Deployment"),
					Health:      lo.ToPtr(models.HealthHealthy), // Start as healthy
					Tags:        map[string]string{"env": "test"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Config).UpdateColumn("health", models.HealthUnhealthy).Error
			},
			IgnoreEvents:   []string{api.EventConfigChanged, api.EventConfigCreated},
			ExpectedEvents: []string{api.EventConfigUnhealthy},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.healthy event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-healthy"),
					ConfigClass: models.ConfigClassDeployment,
					Type:        lo.ToPtr("Kubernetes::Deployment"),
					Health:      lo.ToPtr(models.HealthUnhealthy), // Start as unhealthy
					Tags:        map[string]string{"env": "test"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Config).UpdateColumn("health", models.HealthHealthy).Error
			},
			IgnoreEvents:   []string{api.EventConfigChanged, api.EventConfigCreated, api.EventConfigUnhealthy},
			ExpectedEvents: []string{api.EventConfigHealthy},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.warning event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-warning"),
					ConfigClass: models.ConfigClassDeployment,
					Type:        lo.ToPtr("Kubernetes::Deployment"),
					Health:      lo.ToPtr(models.HealthHealthy), // Start as healthy
					Tags:        map[string]string{"env": "test"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Config).UpdateColumn("health", models.HealthWarning).Error
			},
			IgnoreEvents:   []string{api.EventConfigCreated, api.EventConfigChanged},
			ExpectedEvents: []string{api.EventConfigWarning},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.degraded event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-degraded"),
					ConfigClass: models.ConfigClassDeployment,
					Type:        lo.ToPtr("Kubernetes::Deployment"),
					Health:      lo.ToPtr(models.HealthUnhealthy), // Start as unhealthy
					Tags:        map[string]string{"env": "test"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				// unhealthy -> warning triggers degraded event
				return ctx.DB().Model(res.Config).UpdateColumn("health", models.HealthWarning).Error
			},
			IgnoreEvents:   []string{api.EventConfigCreated, api.EventConfigChanged, api.EventConfigUnhealthy},
			ExpectedEvents: []string{api.EventConfigDegraded},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.unknown event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-unknown"),
					ConfigClass: models.ConfigClassDeployment,
					Type:        lo.ToPtr("Kubernetes::Deployment"),
					Health:      lo.ToPtr(models.HealthHealthy), // Start as healthy
					Tags:        map[string]string{"env": "test"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Config).UpdateColumn("health", "").Error
			},
			IgnoreEvents:   []string{api.EventConfigCreated, api.EventConfigChanged},
			ExpectedEvents: []string{api.EventConfigUnknown},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})
	})

	var _ = ginkgo.Describe("Config Lifecycle Events", func() {
		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.created event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-created"),
					ConfigClass: models.ConfigClassPod,
					Type:        lo.ToPtr("Kubernetes::Pod"),
					Tags:        map[string]string{"test": "created"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return nil // do nothing
			},
			ExpectedEvents: []string{api.EventConfigCreated},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.updated event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-updated"),
					ConfigClass: models.ConfigClassDeployment,
					Type:        lo.ToPtr("Kubernetes::Deployment"),
					Tags:        map[string]string{"test": "updated"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				change := models.ConfigChange{
					ID:         uuid.New().String(),
					ConfigID:   res.Config.ID.String(),
					ChangeType: "Scaling Up",
				}
				return ctx.DB().Create(&change).Error
			},
			IgnoreEvents:   []string{api.EventConfigCreated},
			ExpectedEvents: []string{api.EventConfigChanged},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})

		testEvent(TestCase{
			Name: "should generate correct EventResource for organically triggered config.deleted event",
			Resources: TestResource{
				Config: &models.ConfigItem{
					ID:          uuid.New(),
					Name:        lo.ToPtr("test-config-deleted"),
					ConfigClass: models.ConfigClassDeployment,
					Type:        lo.ToPtr("Kubernetes::Deployment"),
					Tags:        map[string]string{"test": "deleted"},
				},
			},
			DatabaseChange: func(ctx context.Context, res TestResource) error {
				return ctx.DB().Model(res.Config).UpdateColumn("deleted_at", time.Now()).Error
			},
			IgnoreEvents:   []string{api.EventConfigCreated},
			ExpectedEvents: []string{api.EventConfigDeleted},
			ExpectedEventResource: func(res TestResource) pkgEvents.EventResource {
				return pkgEvents.EventResource{
					Config: res.Config,
				}
			},
		})
	})
})
