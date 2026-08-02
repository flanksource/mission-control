package v1

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ParseAndValidatePlaybookSpec", func() {
	ginkgo.It("accepts schema-valid playbooks", func() {
		spec, err := ParseAndValidatePlaybookSpec([]byte(`{
			"actions": [{"name": "echo", "exec": {"script": "echo ok"}}]
		}`))

		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Actions).To(HaveLen(1))
	})

	ginkgo.It("rejects properties outside the generated schema", func() {
		_, err := ParseAndValidatePlaybookSpec([]byte(`{
			"actions": [{"name": "echo", "exec": {"script": "echo ok"}}],
			"unexpected": true
		}`))

		Expect(err).To(MatchError(ContainSubstring("Additional property unexpected is not allowed")))
	})

	ginkgo.It("applies semantic validation after schema validation", func() {
		_, err := ParseAndValidatePlaybookSpec([]byte(`{
			"actions": [
				{"name": "echo", "exec": {"script": "echo one"}},
				{"name": "echo", "exec": {"script": "echo two"}}
			]
		}`))

		Expect(err).To(MatchError(ContainSubstring("all actions should have unique names")))
	})

	reportSpec := func(report string) []byte {
		return []byte(`{"actions": [{"name": "report", "report": ` + report + `}]}`)
	}

	ginkgo.It("accepts a report action with only a view", func() {
		_, err := ParseAndValidatePlaybookSpec(reportSpec(`{"view": "my-view"}`))

		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("rejects a report action with both view and configs", func() {
		_, err := ParseAndValidatePlaybookSpec(reportSpec(`{"view": "my-view", "configs": {"name": "my-config"}}`))

		Expect(err).To(MatchError(ContainSubstring("view is mutually exclusive with configs, configsFromParams, and file")))
	})

	ginkgo.It("rejects a report action with both view and configsFromParams", func() {
		_, err := ParseAndValidatePlaybookSpec(reportSpec(`{"view": "my-view", "configsFromParams": true}`))

		Expect(err).To(MatchError(ContainSubstring("view is mutually exclusive with configs, configsFromParams, and file")))
	})

	ginkgo.It("rejects a report action with both view and file", func() {
		_, err := ParseAndValidatePlaybookSpec(reportSpec(`{"view": "my-view", "file": {"path": "report/CatalogReport.tsx"}}`))

		Expect(err).To(MatchError(ContainSubstring("view is mutually exclusive with configs, configsFromParams, and file")))
	})

	ginkgo.It("rejects a report action with both configs and configsFromParams", func() {
		_, err := ParseAndValidatePlaybookSpec(reportSpec(`{"configs": {"name": "my-config"}, "configsFromParams": true}`))

		Expect(err).To(MatchError(ContainSubstring("configs and configsFromParams are mutually exclusive")))
	})

	ginkgo.It("rejects a report file with both path and git", func() {
		_, err := ParseAndValidatePlaybookSpec(reportSpec(`{"configsFromParams": true, "file": {"path": "a.tsx", "git": {"url": "https://github.com/org/repo", "file": "b.tsx"}}}`))

		Expect(err).To(MatchError(ContainSubstring("exactly one of path or git must be set")))
	})

	ginkgo.It("rejects a report file with neither path nor git", func() {
		_, err := ParseAndValidatePlaybookSpec(reportSpec(`{"configsFromParams": true, "file": {}}`))

		Expect(err).To(MatchError(ContainSubstring("exactly one of path or git must be set")))
	})
})
