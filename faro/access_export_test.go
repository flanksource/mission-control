package main

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("access users export", func() {
	var (
		humanID   uuid.UUID
		serviceID uuid.UUID
		groupID   uuid.UUID
		grantedAt time.Time
		email     string
	)

	ginkgo.BeforeEach(func() {
		humanID = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
		serviceID = uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
		groupID = uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
		grantedAt = time.Date(2026, 7, 27, 11, 6, 33, 0, time.UTC)
		email = "jane@example.com"
	})

	human := func(id uuid.UUID, mail *string) models.ExternalUser {
		return models.ExternalUser{
			ID: id, Name: "Jane Doe", Email: mail, UserType: "Human",
			Tenant: "tenant-x", CreatedAt: grantedAt, Aliases: []string{"jdoe"},
		}
	}

	grant := func(user uuid.UUID, configType, group string) sdk.AccessGrant {
		g := sdk.AccessGrant{GroupName: group}
		g.ConfigID = uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
		g.ConfigName = "prod-cluster"
		g.ConfigType = configType
		g.ExternalUserID = user
		g.Role = "Owner"
		g.CreatedAt = grantedAt
		return g
	}

	ginkgo.It("records a group-held grant as group:<name> and a direct one as direct", func() {
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email)},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", "platform-admins"), grant(humanID, "AWS::Account", "")},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].ConfigAccess).To(HaveLen(2))
		Expect(entries[0].ConfigAccess[0].Grant).To(Equal("group:platform-admins"))
		Expect(entries[0].ConfigAccess[1].Grant).To(Equal("direct"))
	})

	ginkgo.It("truncates timestamps to the calendar date the register records", func() {
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email)},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", "")},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(*entries[0].ConfigAccess[0].CreatedAt).To(Equal("2026-07-27"))
		Expect(*entries[0].ProvisionedAt).To(Equal("2026-07-27"))
	})

	ginkgo.It("leaves a never-signed-in grant nil rather than dating it today", func() {
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email)},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", "")},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries[0].ConfigAccess[0].LastSignedInAt).To(BeNil())
		Expect(entries[0].ConfigAccess[0].LastReviewedAt).To(BeNil())
	})

	ginkgo.It("omits the tenant key entirely when the scraper records no account", func() {
		user := human(humanID, &email)
		user.Tenant = ""
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{user},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", "")},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		// An empty string would fail the register's schema, which requires a non-empty tenant
		// when the key is present at all.
		Expect(entries[0].Tenant).To(BeEmpty())
	})

	ginkgo.It("omits users holding no config access", func() {
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email), human(serviceID, nil)},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", "")},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].ExternalUserID).To(Equal(humanID.String()))
	})

	ginkgo.It("derives identity_provider from the distinct config type prefixes, sorted", func() {
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email)},
			[]sdk.AccessGrant{
				grant(humanID, "Azure::Subscription", ""),
				grant(humanID, "AWS::Account", ""),
				grant(humanID, "Azure::EnterpriseApplication", ""),
			},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries[0].IdentityProvider).To(Equal("AWS, Azure"))
	})

	ginkgo.It("maps user types onto the register vocabulary and withholds name/email from non-persons", func() {
		service := models.ExternalUser{ID: serviceID, Name: "ci-runner", UserType: "ServiceAccount", CreatedAt: grantedAt}
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email), service},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", ""), grant(serviceID, "AWS::Account", "")},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(2))
		Expect(entries[0].IdentityType).To(Equal("person"))
		Expect(entries[0].Email).To(Equal(email))
		Expect(entries[1].IdentityType).To(Equal("workload_identity"))
		Expect(entries[1].Name).To(BeEmpty())
		Expect(entries[1].Email).To(BeEmpty())
	})

	ginkgo.DescribeTable("maps every user type the tenant actually reports",
		func(userType, expected string) {
			user := models.ExternalUser{ID: serviceID, Name: "principal", UserType: userType, CreatedAt: grantedAt}
			entries, err := projectRegisterIdentities(
				[]models.ExternalUser{user},
				[]sdk.AccessGrant{grant(serviceID, "AWS::Account", "")},
				nil,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].IdentityType).To(Equal(expected))
		},
		ginkgo.Entry("Azure/Google directory user", "User", "person"),
		ginkgo.Entry("GitHub account", "GitHub::User", "person"),
		ginkgo.Entry("local Mission Control account", "local", "person"),
		ginkgo.Entry("cloud service account", "ServiceAccount", "workload_identity"),
		ginkgo.Entry("AWS service principal", "AWSService", "workload_identity"),
	)

	ginkgo.It("emits no entry for a group principal", func() {
		// Group-held access reaches the register through each member's group:<name>
		// grant; a second entry for the group itself would double-count it.
		group := models.ExternalUser{ID: serviceID, Name: "infra-access@example.com", UserType: "Group", CreatedAt: grantedAt}
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email), group},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", ""), grant(serviceID, "AWS::Account", "")},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].ExternalUserID).To(Equal(humanID.String()))
	})

	ginkgo.It("errors rather than guessing when a user type has no register mapping", func() {
		unknown := models.ExternalUser{ID: serviceID, Name: "mystery", UserType: "Okta::Principal", CreatedAt: grantedAt}
		_, err := projectRegisterIdentities(
			[]models.ExternalUser{unknown},
			[]sdk.AccessGrant{grant(serviceID, "AWS::Account", "")},
			nil,
		)
		Expect(err).To(MatchError(ContainSubstring("unmapped user_type \"Okta::Principal\"")))
	})

	ginkgo.It("counts grants that no exported entry accounts for, keyed by their holder", func() {
		// Kubernetes-style bindings are recorded against a nil external user. A per-user
		// register cannot hold them, so the export must at least report them.
		orphan := grant(uuid.Nil, "Kubernetes::Cluster", "system:authenticated")
		orphan.User = "system:authenticated"
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email)},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", ""), orphan, orphan},
			nil,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(unattributedGrants(entries, []sdk.AccessGrant{grant(humanID, "Azure::Subscription", ""), orphan, orphan})).
			To(Equal(map[string]int{"system:authenticated": 2}))
	})

	ginkgo.It("attaches only the memberships belonging to each user", func() {
		entries, err := projectRegisterIdentities(
			[]models.ExternalUser{human(humanID, &email)},
			[]sdk.AccessGrant{grant(humanID, "Azure::Subscription", "")},
			map[uuid.UUID][]RegisterGroup{
				humanID:   {{ID: groupID.String(), Name: "platform-admins", GroupType: "Security"}},
				serviceID: {{ID: groupID.String(), Name: "ci", GroupType: "Security"}},
			},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries[0].Groups).To(HaveLen(1))
		Expect(entries[0].Groups[0].Name).To(Equal("platform-admins"))
	})
})
