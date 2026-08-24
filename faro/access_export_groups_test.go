package main

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/sdk"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("access identity group export", func() {
	groupID := uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
	userID := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	formerUserID := uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
	configID := uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
	observedAt := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	group := sdk.ExternalGroupSummary{ExternalGroup: models.ExternalGroup{
		ID: groupID, Name: "Flanksource Jnr", GroupType: "Security", Tenant: "tenant-x",
		Aliases: []string{"flanksource-jnr"}, CreatedAt: observedAt,
	}}
	groupGrant := func() sdk.AccessGrant {
		grant := sdk.AccessGrant{GroupName: group.Name}
		grant.ConfigID = configID
		grant.ConfigName = "production"
		grant.ConfigType = "Kubernetes::Cluster"
		grant.ExternalGroupID = &groupID
		grant.Role = "View"
		grant.CreatedAt = observedAt
		return grant
	}

	ginkgo.It("emits the group holder once with direct grants and active members", func() {
		deletedAt := observedAt.Add(time.Hour)
		entries := projectRegisterGroups(
			[]sdk.ExternalGroupSummary{group},
			[]sdk.AccessGrant{groupGrant()},
			[]sdk.GroupMember{
				{GroupID: groupID, UserID: userID, UserName: "Jane Doe", Email: "jane@example.com", UserType: "Human", MembershipAddedAt: observedAt},
				{GroupID: groupID, UserID: formerUserID, UserName: "Former Member", MembershipAddedAt: observedAt, MembershipDeletedAt: &deletedAt},
			},
		)

		Expect(entries).To(HaveLen(1))
		Expect(entries[0].ID).To(Equal("external-group-" + groupID.String()))
		Expect(entries[0].IdentityType).To(Equal("group"))
		Expect(entries[0].ExternalGroupID).To(Equal(groupID.String()))
		Expect(entries[0].Name).To(Equal("Flanksource Jnr"))
		Expect(entries[0].Members).To(HaveLen(1))
		Expect(entries[0].ConfigAccess).To(HaveLen(1))
		Expect(entries[0].ConfigAccess[0].ExternalGroupID).To(Equal(groupID.String()))
		Expect(entries[0].ConfigAccess[0].Grant).To(Equal("direct"))
	})

	ginkgo.It("does not copy a group-held grant onto a user row", func() {
		user := models.ExternalUser{ID: userID, Name: "Jane Doe", UserType: "Human", CreatedAt: observedAt}
		entries, err := projectRegisterIdentities([]models.ExternalUser{user}, []sdk.AccessGrant{groupGrant()}, nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	ginkgo.It("omits groups that hold no access", func() {
		Expect(projectRegisterGroups([]sdk.ExternalGroupSummary{group}, nil, nil)).To(BeEmpty())
	})
})
