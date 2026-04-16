package catalog

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/api"
)

var _ = ginkgo.Describe("ChangeMappings", func() {
	var ctx dutyContext.Context

	ginkgo.BeforeEach(func() {
		ctx = dutyContext.New()
	})

	ginkgo.It("applies category and transform independently", func() {
		mapper, err := newChangeMapper(ctx, []api.CatalogReportCategoryMapping{
			{
				Category: "backup.failed",
				Filter:   `changeType == "BackupFailed"`,
			},
			{
				Filter:    `"kind" in details && details["kind"] == "Backup/v1"`,
				Transform: `details`,
			},
		})
		Expect(err).ToNot(HaveOccurred())

		change := api.CatalogReportChange{
			ChangeType: "BackupFailed",
			Details: map[string]any{
				"kind":   "Backup/v1",
				"status": "failed",
				"target": "prod-db",
			},
		}

		Expect(mapper.Apply(&change)).To(Succeed())
		Expect(change.Category).To(Equal("backup.failed"))
		Expect(change.TypedChange).To(HaveKeyWithValue("kind", "Backup/v1"))
		Expect(change.TypedChange).To(HaveKeyWithValue("target", "prod-db"))
	})

	ginkgo.It("ignores transform results without a kind", func() {
		mapper, err := newChangeMapper(ctx, []api.CatalogReportCategoryMapping{
			{
				Filter:    `changeType == "ScalingReplicaSet"`,
				Transform: `{"from_replicas": 1, "to_replicas": 3}`,
			},
		})
		Expect(err).ToNot(HaveOccurred())

		change := api.CatalogReportChange{ChangeType: "ScalingReplicaSet"}
		Expect(mapper.Apply(&change)).To(Succeed())
		Expect(change.TypedChange).To(BeNil())
	})

	ginkgo.It("hydrates typedChange from typed details without a transform rule", func() {
		var mapper *changeMapper
		change := api.CatalogReportChange{
			ChangeType: "PipelineRunCompleted",
			Details: map[string]any{
				"kind":          "PipelineRun/v1",
				"pipeline_name": "deploy-api",
				"status":        "completed",
			},
		}

		Expect(mapper.Apply(&change)).To(Succeed())
		Expect(change.TypedChange).To(HaveKeyWithValue("kind", "PipelineRun/v1"))
		Expect(change.TypedChange).To(HaveKeyWithValue("pipeline_name", "deploy-api"))
	})

	ginkgo.It("threads decoded details onto report changes", func() {
		change := newCatalogReportChangeFromRow(queryChange("chg-1", "BackupCompleted"), "prod-db", "AWS::RDS::DBInstance", map[string]any{
			"kind":   "Backup/v1",
			"status": "completed",
		})

		Expect(change.ConfigName).To(Equal("prod-db"))
		Expect(change.ConfigType).To(Equal("AWS::RDS::DBInstance"))
		Expect(change.Details).To(HaveKeyWithValue("kind", "Backup/v1"))
		Expect(change.Details).To(HaveKeyWithValue("status", "completed"))
	})
})

func queryChange(id, changeType string) query.ConfigChangeRow {
	return query.ConfigChangeRow{
		ID:         id,
		ConfigID:   "cfg-1",
		ChangeType: changeType,
		Severity:   "info",
		Source:     "unit-test",
		Summary:    "summary",
		Count:      1,
	}
}
