package catalog

import (
	"time"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/db"
)

var _ = ginkgo.Describe("ExpandGroupAccess", func() {
	var (
		aliceID    uuid.UUID
		bobID      uuid.UUID
		carolID    uuid.UUID
		adminGroup string
		viewGroup  string
		addedAt    time.Time
		deletedAt  time.Time
	)

	ginkgo.BeforeEach(func() {
		aliceID = uuid.New()
		bobID = uuid.New()
		carolID = uuid.New()
		adminGroup = "admins"
		viewGroup = "viewers"
		addedAt = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
		deletedAt = time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	})

	directRow := func(userID uuid.UUID, user, role string) db.RBACAccessRow {
		return db.RBACAccessRow{
			ConfigID:   uuid.MustParse("00000000-0000-0000-0000-000000000000"),
			ConfigName: "svc",
			ConfigType: "Kubernetes::Pod",
			UserID:     userID,
			UserName:   user,
			Email:      user + "@example.com",
			Role:       role,
			UserType:   "Kubernetes",
			CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}
	}

	groupRow := func(group, role string) db.RBACAccessRow {
		r := directRow(uuid.Nil, group, role)
		r.UserType = ""
		r.GroupName = &group
		return r
	}

	ginkgo.It("returns input unchanged when there are no group rows", func() {
		rows := []db.RBACAccessRow{directRow(aliceID, "alice", "admin")}
		out := ExpandGroupAccess(rows, nil)
		Expect(out).To(Equal(rows))
	})

	ginkgo.It("emits only the original row when the group has no active members", func() {
		rows := []db.RBACAccessRow{groupRow(adminGroup, "admin")}
		out := ExpandGroupAccess(rows, nil)
		Expect(out).To(HaveLen(1))
		Expect(out[0].GroupName).To(Equal(&adminGroup))
	})

	ginkgo.It("expands a group row into synthetic member rows while passing direct rows through", func() {
		rows := []db.RBACAccessRow{
			directRow(aliceID, "alice", "admin"),
			groupRow(adminGroup, "admin"),
		}
		members := []db.GroupMemberRow{
			{GroupName: adminGroup, UserID: bobID, UserName: "bob", Email: "bob@example.com", UserType: "Azure", MembershipAddedAt: addedAt},
			{GroupName: adminGroup, UserID: carolID, UserName: "carol", Email: "carol@example.com", UserType: "Azure", MembershipAddedAt: addedAt},
		}

		out := ExpandGroupAccess(rows, members)

		Expect(out).To(HaveLen(4))
		Expect(out[0]).To(Equal(rows[0]))
		Expect(out[1]).To(Equal(rows[1]))
		Expect(out[2].UserID).To(Equal(bobID))
		Expect(out[2].UserName).To(Equal("bob"))
		Expect(out[2].GroupName).To(Equal(&adminGroup))
		Expect(out[2].Role).To(Equal("admin"))
		Expect(out[2].UserType).To(Equal("Azure"))
		Expect(out[2].CreatedAt).To(Equal(addedAt))
		Expect(out[2].LastReviewedAt).To(BeNil())
		Expect(out[3].UserID).To(Equal(carolID))
	})

	ginkgo.It("filters out soft-deleted memberships", func() {
		rows := []db.RBACAccessRow{groupRow(adminGroup, "admin")}
		members := []db.GroupMemberRow{
			{GroupName: adminGroup, UserID: bobID, UserName: "bob", MembershipAddedAt: addedAt},
			{GroupName: adminGroup, UserID: carolID, UserName: "carol", MembershipAddedAt: addedAt, MembershipDeletedAt: &deletedAt},
		}

		out := ExpandGroupAccess(rows, members)

		Expect(out).To(HaveLen(2))
		Expect(out[0].GroupName).To(Equal(&adminGroup))
		Expect(out[1].UserID).To(Equal(bobID))
	})

	ginkgo.It("emits a member once per granting group when the same user is in multiple groups", func() {
		rows := []db.RBACAccessRow{
			groupRow(adminGroup, "admin"),
			groupRow(viewGroup, "viewer"),
		}
		members := []db.GroupMemberRow{
			{GroupName: adminGroup, UserID: bobID, UserName: "bob", MembershipAddedAt: addedAt},
			{GroupName: viewGroup, UserID: bobID, UserName: "bob", MembershipAddedAt: addedAt},
		}

		out := ExpandGroupAccess(rows, members)

		Expect(out).To(HaveLen(4))
		Expect(*out[1].GroupName).To(Equal(adminGroup))
		Expect(out[1].UserID).To(Equal(bobID))
		Expect(out[1].Role).To(Equal("admin"))
		Expect(*out[3].GroupName).To(Equal(viewGroup))
		Expect(out[3].UserID).To(Equal(bobID))
		Expect(out[3].Role).To(Equal("viewer"))
	})

	ginkgo.It("produces synthetic rows whose RoleSource resolves to group:<name>", func() {
		rows := []db.RBACAccessRow{groupRow(adminGroup, "admin")}
		members := []db.GroupMemberRow{
			{GroupName: adminGroup, UserID: bobID, UserName: "bob", MembershipAddedAt: addedAt},
		}

		out := ExpandGroupAccess(rows, members)

		Expect(out).To(HaveLen(2))
		Expect(out[1].RoleSource()).To(Equal("group:" + adminGroup))
	})

	ginkgo.It("reports hasGroupRow correctly", func() {
		Expect(hasGroupRow([]db.RBACAccessRow{directRow(aliceID, "alice", "admin")})).To(BeFalse())
		Expect(hasGroupRow([]db.RBACAccessRow{groupRow(adminGroup, "admin")})).To(BeTrue())

		empty := ""
		mixed := []db.RBACAccessRow{
			directRow(aliceID, "alice", "admin"),
			{GroupName: &empty},
		}
		Expect(hasGroupRow(mixed)).To(BeFalse())
	})
})
