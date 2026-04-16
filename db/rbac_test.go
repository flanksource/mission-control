package db

import (
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetGroupMembersForConfigs", func() {
	It("returns members of a group referenced by config_access", func() {
		rows, err := GetGroupMembersForConfigs(DefaultContext, []uuid.UUID{dummy.MissionControlNamespace.ID})
		Expect(err).ToNot(HaveOccurred())

		// The MissionControlNamespace config has access rows for both the
		// admins group (JohnDoe + Alice) and the readers group (Bob + Charlie).
		userIDsByGroup := map[uuid.UUID]map[uuid.UUID]bool{}
		groupNames := map[uuid.UUID]string{}
		for _, r := range rows {
			if userIDsByGroup[r.GroupID] == nil {
				userIDsByGroup[r.GroupID] = map[uuid.UUID]bool{}
			}
			userIDsByGroup[r.GroupID][r.UserID] = true
			groupNames[r.GroupID] = r.GroupName
		}

		Expect(groupNames).To(HaveKey(dummy.MissionControlAdminsGroup.ID))
		Expect(groupNames[dummy.MissionControlAdminsGroup.ID]).To(Equal("mission-control-admins"))
		Expect(userIDsByGroup[dummy.MissionControlAdminsGroup.ID]).To(HaveKey(dummy.JohnDoeExternalUser.ID))
		Expect(userIDsByGroup[dummy.MissionControlAdminsGroup.ID]).To(HaveKey(dummy.AliceExternalUser.ID))

		Expect(groupNames).To(HaveKey(dummy.MissionControlReadersGroup.ID))
		Expect(userIDsByGroup[dummy.MissionControlReadersGroup.ID]).To(HaveKey(dummy.BobExternalUser.ID))
		Expect(userIDsByGroup[dummy.MissionControlReadersGroup.ID]).To(HaveKey(dummy.CharlieExternalUser.ID))
	})

	It("returns no rows for configs without group-based access", func() {
		// Use a random config ID that has no config_access group rows.
		rows, err := GetGroupMembersForConfigs(DefaultContext, []uuid.UUID{uuid.New()})
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(BeEmpty())
	})

	It("returns nil when no config IDs are provided", func() {
		rows, err := GetGroupMembersForConfigs(DefaultContext, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(BeNil())
	})

	It("populates member identity fields from external_users", func() {
		rows, err := GetGroupMembersForConfigs(DefaultContext, []uuid.UUID{dummy.MissionControlNamespace.ID})
		Expect(err).ToNot(HaveOccurred())

		var johnRow *GroupMemberRow
		for i := range rows {
			if rows[i].UserID == dummy.JohnDoeExternalUser.ID {
				johnRow = &rows[i]
				break
			}
		}
		Expect(johnRow).ToNot(BeNil(), "expected john doe in admins group")
		Expect(johnRow.UserName).To(Equal("John Doe"))
		Expect(johnRow.Email).To(Equal("johndoe@flanksource.com"))
		Expect(johnRow.UserType).To(Equal("user"))
		Expect(johnRow.GroupType).To(Equal("group"))
		Expect(johnRow.MembershipAddedAt).ToNot(BeZero())
	})
})
