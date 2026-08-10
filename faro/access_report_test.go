package main

import (
	"github.com/goccy/go-yaml"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func reportIdentity(id, provider, email string, aliases []string, types ...string) RegisterIdentity {
	grants := make([]RegisterGrant, 0, len(types))
	for _, configType := range types {
		grants = append(grants, RegisterGrant{
			ConfigID:   "config-" + configType,
			ConfigName: "item-" + configType,
			ConfigType: configType,
			Role:       "reader",
			Grant:      "direct",
		})
	}
	return RegisterIdentity{
		ExternalUserID:   id,
		IdentityType:     "person",
		IdentityProvider: provider,
		Email:            email,
		Aliases:          aliases,
		ConfigAccess:     grants,
	}
}

var _ = ginkgo.Describe("access report", func() {
	ginkgo.It("reports a principal reachable from two contexts as one row", func() {
		person := reportIdentity("u1", "GCP", "jane@example.com", nil, "GCP::Project")
		alsoPerson := reportIdentity("u2", "MissionControl", "jane@example.com", nil, "MissionControl::Playbook")

		report := buildAccessReport([]AccessExportResult{
			{Context: "beta", ExportedAt: "2026-08-07", Entries: []RegisterIdentity{person}},
			{Context: "prod-eu", ExportedAt: "2026-08-07", Entries: []RegisterIdentity{alsoPerson}},
		}, map[string]string{}, false)

		Expect(report.Users).To(HaveLen(1))
		Expect(report.Users[0].Contexts).To(Equal([]string{"beta", "prod-eu"}))
		Expect(report.Users[0].Providers).To(Equal([]string{"GCP", "MissionControl"}))
		Expect(report.Users[0].Grants).To(HaveLen(2))
	})

	ginkgo.It("folds accounts onto the human a register records as their owner", func() {
		// A GitHub login carries no address, so nothing in the export ties it to a person.
		// The register's owner determination is the only evidence that it does, which is
		// why the fold reads it from there rather than guessing from the name.
		github := reportIdentity("gh1", "GitHub", "", []string{"github://user/1"}, "GitHub::Repository")
		gcp := reportIdentity("gcp1", "GCP", "jane@example.com", nil, "GCP::Project")
		owners := map[string]string{"github://user/1": "jane@example.com", "jane@example.com": "jane@example.com"}

		report := buildAccessReport([]AccessExportResult{
			{Context: "beta", ExportedAt: "2026-08-07", Entries: []RegisterIdentity{github, gcp}},
		}, owners, false)

		Expect(report.Users).To(HaveLen(1))
		Expect(report.Users[0].Label).To(Equal("jane@example.com"))
		Expect(report.Users[0].Grants).To(HaveLen(2))
	})

	ginkgo.It("keeps an unowned account its own row rather than guessing a person", func() {
		github := reportIdentity("gh1", "GitHub", "", []string{"github://user/1"}, "GitHub::Repository")
		gcp := reportIdentity("gcp1", "GCP", "jane@example.com", nil, "GCP::Project")

		report := buildAccessReport([]AccessExportResult{
			{Context: "beta", ExportedAt: "2026-08-07", Entries: []RegisterIdentity{github, gcp}},
		}, map[string]string{}, false)

		Expect(report.Users).To(HaveLen(2))
	})

	ginkgo.It("counts distinct items rather than grants, so two roles on one config is one item", func() {
		identity := reportIdentity("u1", "MissionControl", "jane@example.com", nil, "MissionControl::Playbook")
		identity.ConfigAccess = append(identity.ConfigAccess, RegisterGrant{
			ConfigID:   "config-MissionControl::Playbook",
			ConfigName: "item-MissionControl::Playbook",
			ConfigType: "MissionControl::Playbook",
			Role:       "playbook:approve",
			Grant:      "direct",
		})

		report := buildAccessReport([]AccessExportResult{
			{Context: "beta", ExportedAt: "2026-08-07", Entries: []RegisterIdentity{identity}},
		}, map[string]string{}, false)

		rows := report.matrixRows()
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Items).To(Equal(1))
		Expect(rows[0].Roles).To(Equal("playbook:approve, reader"))
	})

	ginkgo.It("omits workload identities when only people are asked for", func() {
		person := reportIdentity("u1", "GCP", "jane@example.com", nil, "GCP::Project")
		workload := reportIdentity("u2", "GCP", "", nil, "GCP::ServiceAccount")
		workload.IdentityType = "workload_identity"

		report := buildAccessReport([]AccessExportResult{
			{Context: "beta", ExportedAt: "2026-08-07", Entries: []RegisterIdentity{person, workload}},
		}, map[string]string{}, true)

		Expect(report.Users).To(HaveLen(1))
		Expect(report.Users[0].IdentityType).To(Equal("person"))
	})
})

var _ = ginkgo.Describe("register verify", func() {
	const twoContextRegister = `schema_version: 1
entries:
  - id: person-a
    external_user_id: beta-1
    aliases:
      - prod-1
    config_access:
      - config_id: null
        config_name: hand-authored
        config_type: Tailscale::Tailnet
        role: owner
        grant: direct
      - config_id: c1
        context: beta
        config_name: one
        config_type: GCP::Project
        role: editor
        grant: direct
      - config_id: c2
        context: prod-eu
        config_name: two
        config_type: MissionControl::Playbook
        role: playbook:run
        grant: direct
`

	summarise := func(context string) (map[string]*registerContextSummary, []*registerContextSummary) {
		var document yaml.MapSlice
		Expect(yaml.UnmarshalWithOptions([]byte(twoContextRegister), &document, yaml.UseOrderedMap())).To(Succeed())
		entries, err := registerEntries(document)
		Expect(err).ToNot(HaveOccurred())
		return summariseRegister(entries, context)
	}

	ginkgo.It("counts only the grants the named context scraped", func() {
		// The register holds the union of both contexts, while a rollup only ever
		// describes its own — comparing the whole entry would report drift on every
		// identity reachable from more than one instance.
		_, all := summarise("beta")

		Expect(all).To(HaveLen(1))
		Expect(all[0].grants).To(Equal(1))
		Expect(all[0].configs).To(Equal(1))
	})

	ginkgo.It("excludes hand-authored grants, which no rollup can report", func() {
		_, all := summarise("beta")

		Expect(all[0].handAuthored).To(Equal(1))
	})

	ginkgo.It("answers to every context's identifier, not just the last imported", func() {
		byIdentifier, _ := summarise("prod-eu")

		Expect(byIdentifier).To(HaveKey("beta-1"))
		Expect(byIdentifier).To(HaveKey("prod-1"))
		Expect(byIdentifier["prod-1"].grants).To(Equal(1))
	})

	ginkgo.It("records no summary for a context that scraped nothing", func() {
		_, all := summarise("some-other-context")

		Expect(all).To(BeEmpty())
	})
})
