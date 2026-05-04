package notification

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/gomplate/v3"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

var _ = ginkgo.Describe("CelVariable", func() {
	var (
		vars celVariables
		env  map[string]any
	)

	ginkgo.BeforeEach(func() {
		vars = celVariables{
			Permalink: "https://example.com",
			ConfigItem: &models.ConfigItem{
				Name:   lo.ToPtr("podinfo"),
				Health: lo.ToPtr(models.HealthHealthy),
				Status: lo.ToPtr("Running"),
				Type:   lo.ToPtr("Kubernetes::Pod"),
				Labels: &types.JSONStringMap{
					"app": "podinfo",
				},
				Tags: map[string]string{
					"namespace":   "default",
					"region":      "us-west-1",
					"environment": "prod",
				},
			},
		}
		env = vars.AsMap(context.New())
	})

	ginkgo.It("should expose tags as root level variables", func() {
		tests := []struct {
			expr     string
			expected string
		}{
			{expr: `region`, expected: "us-west-1"},
			{expr: `region == "us-west-1"`, expected: "true"},
			{expr: `region == "us-west-2"`, expected: "false"},
			{expr: `environment == "prod"`, expected: "true"},
			{expr: `permalink == "https://example.com"`, expected: "true"},
		}

		for _, tt := range tests {
			result, err := gomplate.RunTemplate(env, gomplate.Template{Expression: tt.expr})
			Expect(err).To(BeNil())
			Expect(result).To(Equal(tt.expected))
		}
	})

	ginkgo.It("should have no null variables", func() {
		Expect(env).To(HaveKey("component"))
		Expect(env).To(HaveKey("config"))
		Expect(env).To(HaveKey("check"))

		Expect(env["check"]).To(HaveKeyWithValue("name", ""))
		Expect(env["component"]).To(HaveKeyWithValue("name", ""))
		Expect(env["config"]).To(HaveKeyWithValue("name", "podinfo"))
	})

	ginkgo.It("should expose resource field aliases", func() {
		Expect(env).To(HaveKeyWithValue("name", "podinfo"))
		Expect(env).To(HaveKeyWithValue("health", string(models.HealthHealthy)))
		Expect(env).To(HaveKeyWithValue("status", "Running"))

		Expect(env).To(HaveKey("labels"))
		Expect(env).To(HaveKey("tags"))

		Expect(env["labels"]).To(HaveKeyWithValue("app", "podinfo"))
		Expect(env["tags"]).To(HaveKeyWithValue("namespace", "default"))
	})

	ginkgo.It("should refresh nested selected resource health and status", func() {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		Expect(err).ToNot(HaveOccurred())

		Expect(db.Exec(`CREATE TABLE config_items (id text PRIMARY KEY, health text, status text, deleted_at datetime, updated_at datetime)`).Error).To(Succeed())
		Expect(db.Exec(`CREATE TABLE components (id text PRIMARY KEY, health text, status text, deleted_at datetime, updated_at datetime)`).Error).To(Succeed())
		Expect(db.Exec(`CREATE TABLE checks (id text PRIMARY KEY, status text, deleted_at datetime, updated_at datetime)`).Error).To(Succeed())

		ctx := context.New().WithDB(db, nil)

		configID := uuid.New()
		componentID := uuid.New()
		checkID := uuid.New()
		staleHealth := models.HealthHealthy
		latestHealth := models.HealthUnhealthy

		Expect(db.Exec(`INSERT INTO config_items (id, health, status) VALUES (?, ?, ?)`, configID.String(), latestHealth, "Terminated").Error).To(Succeed())
		Expect(db.Exec(`INSERT INTO components (id, health, status) VALUES (?, ?, ?)`, componentID.String(), latestHealth, "Stopped").Error).To(Succeed())
		Expect(db.Exec(`INSERT INTO checks (id, status) VALUES (?, ?)`, checkID.String(), models.CheckStatusUnhealthy).Error).To(Succeed())

		tests := []struct {
			name         string
			vars         celVariables
			nestedKey    string
			latestHealth models.Health
			latestStatus string
		}{
			{
				name: "config",
				vars: celVariables{ConfigItem: &models.ConfigItem{
					ID:     configID,
					Name:   lo.ToPtr("podinfo"),
					Health: lo.ToPtr(staleHealth),
					Status: lo.ToPtr("Running"),
				}},
				nestedKey:    "config",
				latestHealth: latestHealth,
				latestStatus: "Terminated",
			},
			{
				name: "component",
				vars: celVariables{Component: &models.Component{
					ID:     componentID,
					Name:   "frontend",
					Health: lo.ToPtr(staleHealth),
					Status: "Running",
				}},
				nestedKey:    "component",
				latestHealth: latestHealth,
				latestStatus: "Stopped",
			},
			{
				name: "check",
				vars: celVariables{Check: &models.Check{
					ID:     checkID,
					Name:   "http-check",
					Status: models.CheckStatusHealthy,
				}},
				nestedKey:    "check",
				latestHealth: models.HealthUnhealthy,
				latestStatus: models.CheckStatusUnhealthy,
			},
		}

		for _, tt := range tests {
			celEnv := tt.vars.AsMap(ctx, celVarGetLatestHealthStatus)
			Expect(celEnv).To(HaveKeyWithValue("health", tt.latestHealth), tt.name)
			Expect(celEnv).To(HaveKeyWithValue("status", tt.latestStatus), tt.name)

			nested, ok := celEnv[tt.nestedKey].(map[string]any)
			Expect(ok).To(BeTrue(), tt.name)
			Expect(nested).To(HaveKeyWithValue("health", tt.latestHealth), tt.name)
			Expect(nested).To(HaveKeyWithValue("status", tt.latestStatus), tt.name)
		}
	})
})
